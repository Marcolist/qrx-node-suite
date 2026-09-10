# Security

## Scope

This document covers the OTA/update trust boundary, the privileged Linux
installer, and the way the suite packages and confines QRX Core. Consensus,
wallet cryptography and QRX protocol correctness remain Core's responsibility;
the integration findings from the pinned 0.0.7 source are tracked separately in
[`qrx-core-0.0.7-security-report.md`](qrx-core-0.0.7-security-report.md).

## OTA threat model

OTA infrastructure is this project's most critical trust boundary: it is
the one path that installs and runs new code. Every threat below is
addressed in `agent/updates/manifest` and `agent/updates`.

| Threat | Mitigation |
|---|---|
| **Malicious update server** | The manifest's own Ed25519 signature (`manifest.VerifyManifestSignature`) must verify against a key the Agent already trusts, independent of who served the bytes. A malicious/compromised server can refuse to serve updates, but cannot make the Agent trust content it didn't sign. |
| **Compromised release account** (e.g. a GitHub account with push access) | The signing key is never the same credential as the release/publishing account. `GitHubReleaseSource` fetches whatever the compromised account publishes, but `VerifyManifestSignature` still requires the separate Ed25519 signing key, which a compromised GitHub account does not grant. |
| **Modified download** (MITM, corrupted transfer, tampered CDN) | `manifest.VerifyArtifact`: SHA256 checksum AND a per-component Ed25519 signature over that checksum, both checked before the artifact is staged. |
| **Downgrade attack** | `manifest.CheckNotDowngrade`: an automatic install can never target a version older than the one running (`ErrDowngradeBlocked`); a manual downgrade requires the caller to pass `AllowDowngrade: true` explicitly -- see docs/updates.md#downgrade-protection. |
| **Replay attack** (serving a stale, still-validly-signed manifest to force an old, possibly-vulnerable version) | `manifest.CheckNotReplayed` rejects a manifest strictly older (`released_at`) than the last one this Agent already acted on for that channel. Combined with downgrade protection, a replayed manifest can neither be treated as current nor move the system backward. |
| **Fake manifest** (attacker crafts a manifest, or splices real signed component entries from different manifests together) | The manifest-level signature (`manifest_signature`, `manifest.CanonicalPayload`) covers every component's name+version+url+sha256+release_notes as one signed unit -- a splice changes the payload and breaks the signature, even though each individual component signature might still verify on its own. This is a deliberate strengthening beyond a per-component-only signature scheme, and (as of the fix for an external audit's F02 finding) also closes a gap where `url` and `release_notes` -- so a compatibility requirement (`CheckCompatibility`) or the download location itself -- could be altered by anything serving the manifest without invalidating the signature. |
| **Unsigned adapter** | `Manifest.Validate` rejects any manifest with a missing `manifest_signature` or a missing per-component `signature` outright -- an unsigned manifest or component is never even parsed into something the rest of the pipeline could act on. |
| **No public key configured** (`cfg.Updates.PublicKeyBase64` is empty or malformed -- the default in every config `install.sh` generates, until an operator sets a real one) | `VerifyManifestSignature` and `VerifyArtifact` both check the key is exactly `ed25519.PublicKeySize` bytes before ever calling `ed25519.Verify`, which otherwise panics on a nil/wrong-length key. `agent/cmd/agentd/main.go`'s `parsePublicKey` already logged a warning and continued with a `nil` key on this path, but the missing guard meant the very first real manifest fetch on such an install would crash the request instead of returning a clean, expected error (`ErrNoPublicKey`). As of the fix for an external audit's F07 finding. |
| **Malicious dashboard asset** | Dashboard artifacts go through the same checksum+signature pipeline as every other component; `components.Dashboard.Extract` additionally rejects any tar entry that would escape the extraction directory (zip-slip/path traversal) and refuses non-regular-file/non-directory entries (symlinks, devices) outright. |
| **Rollback abuse** (an attacker forcing repeated rollback to reintroduce a known-vulnerable version) | Rollback targets are only ever versions this Agent itself previously verified and activated (`store.Store`'s `previous` pointer) -- rollback never fetches or trusts anything new from the network. `Manager.Rollback` and `QRXCoreUpdateManager`'s binary rollback are both audit-logged (`ADMIN_ROLLBACK_*` / `ADMIN_SWITCH_QRX_CORE`), so repeated rollback activity is visible in `audit_log`, not silent. |
| **Anonymous component-name path traversal** (an unauthenticated caller of the intentionally-public `POST /api/v1/updates/check`/`/updates/plan` endpoints supplying a `component` value like `"../../etc"`) | `Manager.Check` validates `component` against the `Controllers` allowlist before ever building a `store.Store` for it (the same gate `Rollback` already used) -- as of the fix for an external audit's F03 finding. `store.Store` itself independently rejects any component name containing a path separator or equal to `""`/`"."`/`".."` (`ErrInvalidComponent`) as defense-in-depth, so a future caller that skips the Manager-level allowlist still can't make `Store` resolve outside its `BaseDir`. Before this fix, such a name reached `filepath.Join(BaseDir, component)` unvalidated, and a pointer file (`current`/`previous`) at the resulting attacker-chosen path could have its contents returned in the JSON response. |
| **A same-version "update" destroying the still-active release** (a manifest offers the same version already installed -- e.g. a re-check on an unchanged channel -- and the extraction step then fails for any reason, e.g. a corrupted download) | `store.Store.Stage` refuses outright when asked to stage a version equal to the component's current one (`ErrAlreadyActive`); `Manager.Install` checks this even earlier and returns a clean `ErrAlreadyInstalled` before ever reaching the store layer. Confirmed by direct reproduction before the fix: a release's directory is purely a function of its version string, so staging the active version reused the live `current` directory in place, and a failed extraction's cleanup step then deleted the still-active release. As of the fix for an external audit's F08 finding. |
| **A downgrade-then-reinstall destroying the rollback target** (the version currently recorded as `previous` is offered again -- reachable with `AllowDowngrade`, which `CheckNotDowngrade` explicitly permits -- and the extraction step then fails) | The F08 fix above only covered `current`; `store.Store.Stage` now also refuses a version equal to `previous` (`ErrAlreadyPrevious`), and `Manager.Install` again short-circuits earlier with the analogous `ErrAlreadyPrevious`, pointing the caller at `Rollback` (the correct, atomic, no-download way to reactivate that version) instead. Confirmed by direct reproduction before the fix: staging `previous` again reused its own live directory, a failed extraction deleted it, and `RollbackToPrevious` afterward still reported success while leaving `current` pointing at a directory that no longer existed. As of the fix for R03, an external re-review of the F08 fix. |
| **A "successful" OTA update with no real effect** (a compromised or buggy update source could -- or, before this fix, simply a normal dashboard update always did -- report success while the old, potentially-vulnerable code keeps serving every request) | `cmd/agentd`'s dashboard HTTP handler now resolves the OTA store's `Store.CurrentDir()` on every request (falling back to the install-time-seeded directory only when nothing has ever been promoted), so a promoted dashboard update takes effect on the very next request -- matching `components.Dashboard`'s `RequiresRestart()==false` design and `Manifest.Validate`'s guarantee that whatever got promoted was itself signature-verified. As of the fix for an external audit's F07 finding. |
| **Configured adapter connection settings silently discarded** (`cfg.Adapter.CLIPath`/`Network`/`WalletName`/`DataDir` -- exactly what connects the Agent to a real QRX Core node -- never reaching the adapter that actually activates, whenever `cfg.Adapter.Name` is empty, i.e. the default automatic-selection config `install.sh` generates) | `cmd/agentd/main.go` now pre-configures every adapter returned by `adapters.Installed()`, not just one looked up by `cfg.Adapter.Name` (which is `""` in automatic mode, so the old single-name `Configure("", ...)` call looked up an adapter literally named `""`, found none, and silently did nothing) -- whichever adapter `registry.SelectAutomatic` later picks and activates has therefore already received the operator's settings via `SetConfig`. Confirmed by direct reproduction against the real `qrx007` adapter type before fixing. As of the fix for an external audit's F11 finding. |
| **Crash loop the in-process guard can't see** (a self-update binary so broken the kernel can't even `exec()` it -- wrong architecture, a truncated/corrupted extraction, a stripped executable bit -- never gives `BootGuard` a chance to run at all, since that requires the Go runtime to already be executing) | The shipped systemd unit sets `StartLimitIntervalSec=300`/`StartLimitBurst=8` as an OS-level circuit breaker independent of `BootGuard`'s own SQLite-backed counter: once systemd itself has retried that often within the window, it stops and marks the unit `failed` rather than looping forever. Set higher than `BootGuard.MaxAttempts` so BootGuard gets the first chance to self-heal via rollback. As of the fix for an external audit's F07 finding; see `docs/updates.md`'s "Crash-loop guard" section. |
| **Downgrade and replay protection both silently disabled on a freshly bootstrapped install** (a system that has never completed one real OTA update has an empty `store.Store.Current()`, and `manifest.CheckNotDowngrade`/`CheckNotReplayed` both explicitly treat an empty current-version/last-seen as "nothing recorded yet" and let anything through -- so an attacker, or a compromised/buggy update source, offering an older, already-patched-away-from version, or replaying a stale-but-validly-signed manifest, would have it accepted on that very first check, with neither protection able to object) | `cmd/agentd`'s `buildControllers` now calls the new `store.Store.BootstrapCurrent` for "agent" and any active adapter at every startup -- a no-op once a real OTA update has ever promoted something, but on a fresh install it registers the actually-running binary's version as the baseline before any update check can happen, restoring both protections from the very first check instead of leaving a permanent gap until the first successful update. Confirmed by direct reproduction (a test proving the downgrade is silently accepted without this fix, and rejected with it) before fixing. As of the fix for R05, an external security re-review of the F07 fix -- this fix covers the bootstrap-registration piece of R05 specifically; see below for the larger piece of the same finding (a promoted self-update binary having no path to actually run), which is now also fixed. |
| **A "successful" self-binary OTA update with no path to ever actually run** (`Store.Promote` only ever flipped a symlink *inside* the OTA store; nothing made `${QRX_PREFIX}/bin/agentd` -- the fixed path systemd's `ExecStart` always execs -- ever resolve to what got promoted, and nothing ever read `InstallResult.PendingRestart` to exit the process so a restart could happen at all. A self-update could verify, download, stage, and flip its internal pointer correctly and have zero observable effect on what code the Agent actually executes, ever) | See `docs/updates.md#self-binary-components-agent-adapters`: `install.sh`'s `bootstrap_agent_ota_store()` now makes `${QRX_PREFIX}/bin/agentd` a symlink into the OTA store's `current` release instead of a plain file copy, so `Store.Promote()`'s existing atomic rename transitively repoints what `ExecStart` execs; `Deps.RequestSelfRestart` (called by the `updates/install`/`updates/rollback` handlers on `PendingRestart: true`) triggers the same graceful-shutdown path `SIGTERM` already used, so systemd's `Restart=always` actually gets a chance to restart onto it; and a rolled-back self-update (`selfUpdateRollbackRequiresRestart`, scoped to `agent` only) makes `cmd/agentd` exit again so the *next* restart lands on the reverted binary. As of the fix for R05 (external security re-review of the F07 fix); `installer-ci.yml`'s Docker smoke test drives a real install-then-rollback `A -> B -> A` cycle through the live HTTP API against real systemd to verify this, since this repository's own development sandbox has no working Docker/systemd to verify it directly. |
| **`bootstrap_agent_ota_store` registered "local" as the OTA store's current version for offline/local-tarball installs** (`install.sh`'s `QRX_LOCAL_TARBALL` path -- offline/air-gapped installs, and this project's own Docker smoke test -- sets `RELEASE_VERSION="local"`, a placeholder never meant to be a real version string. The R05 install.sh fix fed it straight into `bootstrap_agent_ota_store` as the OTA store's release identifier, so `store.Store.Current()` ended up `"local"`, which doesn't parse as a semantic version -- `manifest.CheckNotDowngrade`'s `version.Compare` falls back to a raw string comparison for anything that doesn't parse as one, and `"0.0.1"` loses to `"local"` lexicographically. Every future update on any system ever installed from a local tarball would have been rejected as a downgrade, regardless of whether it actually was one) | `install_release()` now reads the tarball's own `VERSION` file (present in every release this project builds, GitHub-sourced or local) for the OTA store's release identifier instead of `$RELEASE_VERSION`, falling back to it only if that file is somehow missing. Found by `installer-ci.yml`'s Docker smoke test's self-update round trip (the same test that verifies the two rows below) failing its very first assertion (the launcher symlink resolving to `releases/local/agentd` instead of the real version) immediately after the R05 install.sh fix landed. |
| **Every update install, ever, failed at the download step under the shipped sandbox** (`Manager.Install` and `QRXCoreUpdateManager.Update` both download an update artifact to `os.CreateTemp("", ...)`, which defaults to `/tmp` -- but the shipped systemd unit's `ProtectSystem=strict` makes the real `/tmp` read-only for the unit, and `ReadWritePaths` never included it. This affected every update path, not just the R05 self-binary case above, and had never been caught because nothing had ever exercised a real `Install`/`Update` call against the real shipped sandbox before -- every prior round of review, including this one's own initial R05 fix, tested the pieces around it (the symlink chain, the restart wiring) without ever actually running a download through the real unit) | `PrivateTmp=true` added to the systemd unit (`install.sh`'s generated one and the static `installer/linux/qrx-agent.service` reference): a systemd-managed private, genuinely writable `/tmp` for this unit, set up before it starts -- no capability needed by the unprivileged `qrx-agent` process itself, and a further hardening besides the fix (this unit's temp files are no longer visible to other processes' `/tmp` either). Found and confirmed by the same self-update round trip once the version-comparison bug above (which had been masking it, by rejecting the update before it ever reached the download step) was fixed. |

## Installer threat model

`install.sh` runs as root by necessity (it creates a system user and a systemd
unit), which makes anywhere it writes as root, through a path an unprivileged
process might influence, a privilege-escalation surface distinct from the OTA
threat model above.

| Threat | Mitigation |
|---|---|
| **Root tar extraction escapes the install workspace** | Before extracting the signed release as root, `validate_release_archive` rejects absolute and parent-traversal paths plus symlinks, hard links and special files. Extraction also disables archived ownership and permissions. |
| **Substituted Core source or cryptographic dependency** | Release CI checks out the exact QRX Core commit `4a732c1a7d2b03fb299eabde437646c99c797e2d`; the build helper refuses any other commit. It downloads OpenSSL 3.6.4 over TLS, verifies its fixed SHA-256 before extraction, links it statically, and rejects a resulting `qrxd` that dynamically resolves `libcrypto` or `libssl`. The signed outer Node Suite release covers the Core binaries, the suite patch hash and embedded provenance metadata together; the target installer verifies those exact metadata values again before installation. |
| **RPC credentials exposed in process arguments or on disk to service users** | The suite patch makes `qrxd` read `QRX_RPC_USER` and `QRX_RPC_PASSWORD` from its environment, so secrets never appear in `ExecStart` arguments. Root-owned mode-`0600` environment files are read by systemd before it changes UID. RPC binds only to `127.0.0.1`; authentication remains mandatory even there. |
| **Wallet recovery phrase copied into the system journal or installer log** | First initialization runs before the persistent service, captures output in a private temporary file, removes the recovery line from all error output, writes the phrase and seed backup to root-only mode-`0600` files, then truncates the temporary file. `ExecStartPre` refuses to start the persistent service until the wallet exists, preventing a later automatic wallet creation from printing a phrase into journald. |
| **Monitoring CLI mutates Core state** | The bundled Core patch removes `qrx_ensure_node()` from `qrx-cli`. Monitoring calls are RPC-only and no longer initialize chain configuration or wallet files. This also removes a reproducible first-boot race in which `qrx-cli` and `qrxd` concurrently wrote genesis state. |
| **CLI exits zero for authentication and RPC errors** | The bundled CLI returns nonzero for `{"ok":false}` responses. The Agent independently parses the response envelope and rejects the same error even when connected to an unpatched external 0.0.7 CLI that exits zero. |
| **Core compromise reaches the Agent or host filesystem** | Core and Agent use separate unprivileged accounts. `qrxd.service` receives a strict systemd sandbox, can write only `/var/lib/qrx`, and binds its control API to loopback. The P2P listener is public by design. |
| **Installer log symlink attack** (a compromised, unprivileged `qrx-agent` process plants a symlink at `install.log`'s path so a later root-run `install.sh` write follows it instead of the real log file) | `QRX_LOG_DIR` is root-owned (`root:qrx-agent`, mode `0750`), not agent-writable, so `qrx-agent` can no longer place anything there at all; `setup_logging()` creates `install.log` via `init_log_file()`, which opens a file descriptor (`LOG_FD`) on a private `mktemp`-generated temp file *before* that file is ever visible at the public path, then atomically `rename(2)`s it into place -- safe against both a symlink already present and one raced into place mid-install. Critically, every `log()` call for the rest of the install writes through that same held descriptor, never by reopening `install.log`'s path -- a file descriptor is bound to the underlying inode, not the path, so a symlink planted at *any later point* (not just before the first write) can no longer redirect anything. Covers a system upgraded from an older, vulnerable installer that still has an agent-owned log directory left over from its first install. As of the fix for an external audit's F04 finding, hardened by two rounds of external re-review: R02 first closed the race between checking for a symlink and truncating through it (an atomic rename replaced that check-then-act pattern), then a second re-review found the atomic rename alone only protected the *first* write -- every later `log()` call still reopened the path each time, reproducibly exploitable, closed by moving to a held file descriptor. See `docs/installer.md#installer-log-directory`. |
| **Installer writes as root inside the agent-owned OTA store** (a compromised `qrx-agent` replaces a release directory or target with a symlink before a later reinstall) | Release directories, binaries, and version pointers below `${QRX_DATA_DIR}/components/agent` are now created and replaced through `runuser` as `qrx-agent`; root only opens the already-verified source artifact for stdin. A malicious link can therefore reach only paths the already-compromised service account could write itself, not turn the installer into a privileged writer. The package `VERSION` is also validated before it becomes a path component. |
| **Admin-token configuration writable by the Agent** (compromise of the web-facing service could replace its own configured bearer token and gain durable administrative access after restart) | `agent.json` is `root:qrx-agent` mode `0640`: the service can read its token and settings but cannot modify them. A reinstall also repairs ownership and mode on an existing config. Docker CI checks both the exact metadata and a write attempt as `qrx-agent`. |
| **Uninstaller delete-target hijack** (a compromised, unprivileged `qrx-agent` process points `uninstall.sh --remove-qrx-core`'s marker file at an arbitrary directory, which is then `rm -rf`'d as root) | The marker (`qrx-core-installed-by-this-installer`) lives under `QRX_CONFIG_DIR` (`root:qrx-agent`, mode `0750`), never the agent-writable `QRX_DATA_DIR`, so `qrx-agent` cannot plant or rewrite it at all. As a second, independent line of defense, `uninstall.sh` resolves the marker's content (following any symlinks) and refuses to remove anything that doesn't fall under the documented QRX Core install root (`/opt/qrx`) -- never "/", "/etc", or a symlink escape. As of the fix for an external audit's F05 finding; see `docs/installer.md#qrx-core-removal-safety`. |
| **`--remove-qrx-core` silently doing nothing when combined with `--purge-data`** (moving the F05 marker under `QRX_CONFIG_DIR` meant `--purge-data`'s `rm -rf` of that whole directory could delete the marker before `--remove-qrx-core` ever read it -- an operator explicitly asking for both, e.g. when decommissioning a machine, would end up with QRX Core silently left behind) | `main()` now runs `remove_qrx_core` before `purge_data`, so the marker is always read while it still exists. As of the fix for R04, an external re-review of the F05 fix; see `docs/installer.md#qrx-core-removal-safety`. |
| **Documentation overstating what re-running `install.sh` does** (`docs/installer.md#upgrades` claimed a re-run "does not re-install or touch your existing configuration/database", implying a pure detect-and-stop, when the actual code only warns before continuing through `install_release`/`install_systemd_service` regardless -- reinstalling the binary and dashboard and restarting `qrx-agent.service` every time, outside the OTA system's staged/health-checked/rollback-safe path, even though configuration and the database genuinely are left alone) | Not a code bug in the sense of doing something unsafe -- every non-idempotent side effect (reinstalling the binary/dashboard, restarting the service) is itself safe, just not what the docs promised. Fixed by correcting the documentation and the script's own runtime warning message to describe what re-running actually does, rather than restricting the installer to match an overstated claim: the Agent's own OTA system already exists and is the better tool for a routine upgrade, and this script deliberately stays willing to run so it remains usable to recover a broken/incomplete first install. As of the fix for an external audit's F14 finding; see `docs/installer.md#upgrades`. |

## Hard guarantees (detail)

Expanding on [`SECURITY.md`](../SECURITY.md)'s summary:

1. **No secrets stored/logged/transmitted.** `agent/models.WalletSummary`
   only ever carries what QRX Core itself reports (address, balance) --
   never a key, seed, or passphrase. No package in this module reads a
   wallet file directly; all wallet data comes through the adapter
   boundary (`agent/qrx` -> `agent/adapters`), which talks to `qrx-cli`,
   never to key material on disk.
2. **Remote control / mobile pairing default to disabled/read-only.** See
   `docs/mobile-pairing.md`.
3. **Telemetry is opt-in.** See `docs/telemetry.md`.
4. **Unsigned/unverifiable manifests are never installed.** Covered above.
5. **No automatic downgrades.** Covered above.
6. **Administrative endpoints require auth and are audited.** See
   `agent/api`'s auth middleware (docs/development.md) and
   `agent/storage.AuditLogStore`; every `ADMIN_*` action constant is defined
   in `agent/storage/audit_log.go`.
7. **An explicitly-requested config that fails to load is a startup
   failure, never a silent fallback to Mock-mode defaults.**
   `cmd/agentd/main.go`'s `requireConfigPathExists` rejects a `-config`/
   `QRX_AGENT_CONFIG` path that doesn't exist or isn't readable before
   `config.LoadInto` (whose own contract, correctly, is "no path requested
   at all -> Default()'s values stand" for zero-config Mock-mode
   development) ever runs -- a real, intended config being unreachable is
   not the same situation as none being requested, and conflating them
   meant a broken install (a typo'd path, a permissions problem) could
   silently boot serving assumed/fake node data with no error anywhere.
   As of the fix for an external audit's F13 finding.

## External audit: confirmed findings not yet fixed

An external security audit of this codebase (16 findings, F01-F16) has been
worked through in priority order: F01-F05, F07, F08, F11, and F14 are
fixed above, each with a regression test that fails against the pre-fix
code (F14, a documentation-vs-code mismatch, is verified by the docs now
matching the actual behavior rather than a test); F13 is partially fixed
(see [Hard guarantees](#hard-guarantees-detail) item 7 for what's fixed,
and below for what's still deferred); F09, F10, and F12 are each
partially fixed, below, with the same fixed/deferred split noted inline;
F15 is fixed and verified with a committed lockfile and a live npm audit.
The
remaining findings were independently verified against the current source
(never taken on faith from the report) and are confirmed real, but are
deferred rather than fixed in the same pass -- each for a reason noted below,
generally because the correct fix requires a design decision (TLS? a Host
allowlist? a privilege-escalation mechanism for Core service control?) this
document should not make unilaterally, or because the finding's blast radius
is broad enough to deserve its own dedicated review rather than a patch
appended to an already-large change.

- **F09 -- LAN exposure (partially fixed).** Confirmed three distinct
  issues under this finding. **Fixed:** there was no `Host` allowlist
  anywhere in the HTTP stack, meaning a DNS-rebinding attacker (a public
  domain whose DNS record is switched to `127.0.0.1` or this machine's LAN
  address after a browser's initial same-origin check already passed)
  could reach every unauthenticated read endpoint, and even the
  `RequireAdmin`-gated ones with a stolen/guessed token, from a page
  hosted anywhere on the internet, regardless of `QRX_DASHBOARD_BIND`.
  `RequireAllowedHost` (`agent/api/hostcheck.go`) now wraps the entire HTTP
  handler, including both API routes and dashboard static assets,
  and rejects any request whose `Host` header doesn't name a loopback,
  private-network address (RFC 1918 / link-local / `localhost`), or a literal
  IP currently assigned to the server. This permits direct VPS-IP access while
  still rejecting attacker-controlled DNS names, and follows address changes
  without a fixed install-time allowlist. Origin/CORS wasn't
  separately needed: this server sends no CORS headers at all, so a
  browser already blocks a cross-origin page from reading any response,
  and the admin endpoints' required `Authorization` header forces a CORS
  preflight this server never answers, blocking the write itself too --
  `Host` was the one gap DNS rebinding could still exploit. The dashboard
  now verifies the bearer token before displaying a successful login,
  retains it only in per-tab `sessionStorage`, sends it only on protected
  admin requests, and every response receives a restrictive CSP plus
  anti-framing, anti-sniffing, referrer, and permissions headers. **Still
  deferred:** read endpoints (`/api/v1/status`, `/api/v1/system`,
  `/api/v1/updates`, ...) remain unauthenticated by design, and the admin
  bearer token still travels in cleartext (no TLS termination exists in
  this codebase at all) -- `QRX_DASHBOARD_BIND=lan` is opt-in and off by
  default (`docs/installer.md#dashboard-access`), which bounds but doesn't
  eliminate the exposure to anyone already on the same LAN once an
  operator does turn it on. Closing those two needs deciding on a broader
  security posture (TLS, or authenticating read endpoints too) rather than
  a narrow bug fix.
- **F10 -- resource limits (partially fixed).** Confirmed four distinct
  issues under this finding. **Fixed:** no request body size limits
  (`decodeJSON` now wraps every body in `http.MaxBytesReader`, 64 KiB --
  the only place any handler in this package reads a request body at all);
  no read-side per-request timeouts (`http.Server`'s
  `ReadHeaderTimeout`/`ReadTimeout`/`IdleTimeout` now set, bounding
  slowloris-style attacks and idle keep-alive connections; `WriteTimeout`
  deliberately left unset -- it covers the entire response including an
  intentionally long-lived stream, and would forcibly cut off
  `GET /api/v1/events` after that duration); no cap on concurrent SSE
  connections (`events.Bus.MaxSubscribers`, checked and registered
  atomically under the same lock in the new `TrySubscribe`, refuses past
  256 concurrent subscribers with a `503`/`Retry-After` rather than
  accepting an unbounded number of open connections). **Still deferred:**
  `Policy.Locked` (`agent/updates`) is a persisted boolean, not a mutex --
  it prevents a *second admin request* from starting a concurrent install,
  but doesn't serialize concurrent goroutines within this process; closing
  that needs a real in-process lock around `Manager.Install`/`Rollback`,
  which is a large enough change to the update-execution path to warrant
  its own review rather than a patch bundled with the resource-exhaustion
  fixes above.
- **F12 -- Core service control lacks least privilege (partially fixed).**
  Confirmed four distinct issues under this finding. **Fixed:**
  `agent/platform.New()` returned a `Systemd` service manager with
  `UseSudo: false` by default and nothing ever set it, so `qrxd.service`
  start/stop/restart calls had no privilege-escalation path at all as
  wired -- `installer/linux/qrx-agent-sudoers` existed in the repo but
  neither `install.sh` nor `cmd/agentd/main.go` referenced it or
  `UseSudo` anywhere, a dead, never-installed policy file, so Core
  service control genuinely could not work against a real `qrxd.service`
  owned by a different user. `install.sh`'s new `install_qrx_core_sudoers`
  now generates the real rule (the actual configured
  `QRX_SERVICE_USER`, `visudo -c`-validated before it's installed where
  sudo will read it -- a malformed sudoers file breaks sudo system-wide,
  not just this rule) and `Config.QRXCoreServiceUseSudo` (defaulting to
  `false`, so `docs/development.md`'s local `go run ./cmd/agentd` --
  without the sudoers rule or necessarily a passwordless sudo session --
  keeps calling `systemctl` directly rather than hanging on a password
  prompt) wires `agent/platform.Systemd.UseSudo` through an anonymous
  interface assertion (`cmd/agentd/main.go` has no build tag of its own,
  so it can't reference the Linux-only concrete `*Systemd` type directly
  without breaking non-Linux builds). `QRXCoreUpdateManager.Update` now
  cross-checks `opts.Profile.QRXCoreVersion` against the manifest's
  offered version the same way `SwitchVersion` already did -- without it,
  `CheckSwitchSafety` could validate a profile for an unrelated version
  while actually switching to whatever the manifest offered. The shipped
  unit deliberately leaves `NoNewPrivileges` disabled: enabling it blocks
  sudo's setuid transition before the command-exact sudoers rule can be
  evaluated and makes every Core service action fail. The Agent process
  remains unprivileged; escalation is limited to the four exact
  `systemctl ... qrxd.service` commands in that rule. **Still
  deferred:** `StartCore`'s `dir` argument is still ignored, and no
  `Probe` is configured. Both need a confirmed QRX Core deployment/CLI
  contract this project doesn't have: `docs/qrx-0.0.7-interface.md` (its
  own "What is NOT confirmed" section) documents the `qrx-cli` command
  surface itself as unverified, and this project's own QRX Core boundary
  rule (`docs/architecture.md`, this document's own Scope section) is
  that QRX Core's deployment layout is entirely outside this project's
  authority -- unlike `agentd`'s own OTA symlink-launcher fix (R05,
  above), which this project fully controls and could verify end-to-end,
  building a symlink-launcher-style `StartCore` or a real height/peer-
  sampling `Probe` against an unconfirmed interface would be exactly the
  kind of invented behavior this codebase's own principles rule out.
  Closing this needs either a confirmed QRX Core interface spec or an
  explicit product decision on how far this project verifies a system it
  doesn't own.
- **F13 -- Mock-mode fallback is silent (partially fixed).** Confirmed
  three distinct issues under this finding. **Fixed:** an explicitly-named
  but nonexistent `-config` path used to fall back to Mock-mode defaults
  with no error at all -- see [Hard guarantees](#hard-guarantees-detail)
  item 7. **Still deferred:** `GET /api/v1/version` reports an assumed
  `qrx_core_version` (`AdapterConfig.AssumedQRXCoreVersion`) even with no
  real node connected -- already logged as a warning at the point it's
  used (`agent/config/config.go`'s own doc comment), so this is
  a narrower, already-flagged case than the silent-startup gap, and
  changing what `/api/v1/version` reports is a dashboard-facing behavior
  change of its own; `/health` is pure liveness (does this process
  respond at all), not readiness (is it actually connected to a working
  QRX Core node) -- deferred because that's a genuine design decision
  (a second `/ready` endpoint? redefine `/health`'s semantics, breaking
  anything that already polls it as pure liveness?) this document
  shouldn't make unilaterally.
- **F15 -- npm advisories (fixed and verified).** A real npm registry check
  is now part of this release. `dashboard/package-lock.json` is committed,
  so CI and release builds resolve the reviewed dependency graph through
  `npm ci`. Vite was upgraded to 6.4.3, `@vitejs/plugin-react` to 4.5.1,
  and React Router DOM to 7.18.3. These versions close the Vite path
  traversal/Windows path issues, the affected esbuild dependency, and the
  React Router redirect/SSR hydration advisories reported by `npm audit`.
  A clean `npm ci`, production build, and full `npm audit` complete with
  zero known vulnerabilities. The shipped service still contains no Node
  process: it serves only the resulting static assets through Go's HTTP
  server.

Every fix above was verified against the actual current source before
being made (never assumed from the audit report's prose), and every
deferred finding above was independently reproduced or confirmed by
reading the relevant code, not merely restated from the report.

## Reporting

See [`SECURITY.md`](../SECURITY.md) for how to report a vulnerability.
