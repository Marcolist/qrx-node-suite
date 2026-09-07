# Security

## Scope

This document covers the OTA/update trust boundary in depth (per
docs/updates.md#ota-security's requirement) and the hard guarantees listed
in [`SECURITY.md`](../SECURITY.md). It does not cover QRX Core's own
security model, which is out of scope for this project entirely (see
docs/architecture.md's boundary rules).

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
| **Downgrade and replay protection both silently disabled on a freshly bootstrapped install** (a system that has never completed one real OTA update has an empty `store.Store.Current()`, and `manifest.CheckNotDowngrade`/`CheckNotReplayed` both explicitly treat an empty current-version/last-seen as "nothing recorded yet" and let anything through -- so an attacker, or a compromised/buggy update source, offering an older, already-patched-away-from version, or replaying a stale-but-validly-signed manifest, would have it accepted on that very first check, with neither protection able to object) | `cmd/agentd`'s `buildControllers` now calls the new `store.Store.BootstrapCurrent` for "agent" and any active adapter at every startup -- a no-op once a real OTA update has ever promoted something, but on a fresh install it registers the actually-running binary's version as the baseline before any update check can happen, restoring both protections from the very first check instead of leaving a permanent gap until the first successful update. Confirmed by direct reproduction (a test proving the downgrade is silently accepted without this fix, and rejected with it) before fixing. As of the fix for R05, an external security re-review of the F07 fix -- this fix covers the bootstrap-registration piece of R05 specifically; see the "Known limitation" note below it covers a separate, larger piece of the same finding that is not yet fixed. |

## Installer threat model

`install.sh` runs as root by necessity (it creates a system user and a systemd
unit), which makes anywhere it writes as root, through a path an unprivileged
process might influence, a privilege-escalation surface distinct from the OTA
threat model above.

| Threat | Mitigation |
|---|---|
| **Installer log symlink attack** (a compromised, unprivileged `qrx-agent` process plants a symlink at `install.log`'s path so a later root-run `install.sh` truncates an arbitrary root-owned file instead) | `QRX_LOG_DIR` is root-owned (`root:qrx-agent`, mode `0750`), not agent-writable, so `qrx-agent` can no longer place anything there at all; `setup_logging()` additionally creates `install.log` via `init_log_file()`, an atomic write-elsewhere-then-`rename(2)`-over-the-destination that never opens or truncates through the existing path at all -- safe against both a symlink already present and one raced into place mid-install by a still-running compromised process, covering a system upgraded from an older, vulnerable installer that still has an agent-owned log directory left over from its first install. As of the fix for an external audit's F04 finding, hardened against a remaining TOCTOU race by R02 (external re-review: the first fix's "check for a symlink, then truncate" had a window between the two that a concurrent attacker could win, reproduced in `installer/test/test-detection.sh`'s regression test before being closed). See `docs/installer.md#installer-log-directory`. |
| **Uninstaller delete-target hijack** (a compromised, unprivileged `qrx-agent` process points `uninstall.sh --remove-qrx-core`'s marker file at an arbitrary directory, which is then `rm -rf`'d as root) | The marker (`qrx-core-installed-by-this-installer`) lives under `QRX_CONFIG_DIR` (`root:qrx-agent`, mode `0750`), never the agent-writable `QRX_DATA_DIR`, so `qrx-agent` cannot plant or rewrite it at all. As a second, independent line of defense, `uninstall.sh` resolves the marker's content (following any symlinks) and refuses to remove anything that doesn't fall under the documented QRX Core install root (`/opt/qrx`) -- never "/", "/etc", or a symlink escape. As of the fix for an external audit's F05 finding; see `docs/installer.md#qrx-core-removal-safety`. |
| **`--remove-qrx-core` silently doing nothing when combined with `--purge-data`** (moving the F05 marker under `QRX_CONFIG_DIR` meant `--purge-data`'s `rm -rf` of that whole directory could delete the marker before `--remove-qrx-core` ever read it -- an operator explicitly asking for both, e.g. when decommissioning a machine, would end up with QRX Core silently left behind) | `main()` now runs `remove_qrx_core` before `purge_data`, so the marker is always read while it still exists. As of the fix for R04, an external re-review of the F05 fix; see `docs/installer.md#qrx-core-removal-safety`. |

## Known limitation: a promoted self-update binary has no path to actually run

**This is the largest piece of the R05 finding and is NOT fixed.** Investigating
R05 (an external security re-review of the F07 fix) surfaced something more
severe than the finding's own description: as wired today, a self-binary OTA
update (`agent`, or any `adapter_*`) has **no mechanism at all** for a promoted
binary to ever actually run.

- `components.Agent.Extract` writes the new binary into the OTA store's own
  layout (`<QRX_DATA_DIR>/components/agent/releases/<version>/agentd`).
  `store.Store.Promote` only flips symlinks *within that same layout*
  (`current -> releases/<version>`) -- it never touches
  `${QRX_PREFIX}/bin/agentd`, the completely separate, fixed path
  `install.sh` writes once at install time and the shipped systemd unit's
  `ExecStart` always execs.
- `Manager.Install`'s self-binary path returns `InstallResult.PendingRestart:
  true`, documented as "the caller must arrange a process restart" -- but
  nothing in `agent/api` or `cmd/agentd` ever reads `PendingRestart` or exits
  the process because of it. `docs/updates.md`'s "Crash-loop guard" section
  already described `cmd/agentd` as "responsible for exiting so the process
  supervisor restarts it"; that description was aspirational, not something
  the code actually did.
- Even if the process did exit, systemd would simply re-exec the exact same,
  unchanged `${QRX_PREFIX}/bin/agentd` file -- the newly promoted binary
  sitting in the OTA store is never the one that runs.

In short: a self-binary "successful" OTA update today verifies, downloads,
stages, and flips an internal pointer correctly, but has zero observable
effect on what code the Agent actually executes, ever -- there is currently no
tested path from "Promote succeeded" to "the new binary is running."

**Why this isn't fixed here:** the correct fix is architectural, not a
one-line patch -- most plausibly, making `${QRX_PREFIX}/bin/agentd` itself a
symlink into the OTA store's `current` release (set up once by `install.sh` at
install time, pointing into the already-agent-writable `QRX_DATA_DIR`, so
`Promote` transitively repoints it with no `ProtectSystem=strict`/
`ReadWritePaths` sandboxing changes needed), combined with `cmd/agentd`
actually exiting on `PendingRestart: true` so systemd's restart re-execs
through that symlink onto the new binary. That combination needs to be proven
with a real `A -> B -> A` cycle -- install version A, self-update to B,
confirm the restarted process is actually running B, force B to fail its
health check or be non-executable, confirm systemd/BootGuard together land
back on A -- against a real systemd instance across an actual process restart.
This sandbox has no working Docker daemon and is not itself booted under
systemd (confirmed: `dockerd`/`docker` are installed but there is no
`/var/run/docker.sock` and no daemon to start one with, and `systemctl`
reports "System has not been booted with systemd as init system"), so that
verification cannot be done here. Shipping a change to how the Agent's own
running binary gets selected, unverified against real process-restart
behavior, risks leaving every future install unable to start at all with no
way to have caught it first -- worse than the current, honestly-documented
gap. This needs a session with real systemd/Docker test capability (this
project's own `installer-ci.yml` Docker smoke tests are the right place to
extend once a fix is implemented) rather than a blind attempt here.

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

## External audit: confirmed findings not yet fixed

An external security audit of this codebase (16 findings, F01-F16) has been
worked through in priority order: F01-F05, F07, F08, and F11 are fixed above,
each with a regression test that fails against the pre-fix code. The
remaining findings were independently verified against the current source
(never taken on faith from the report) and are confirmed real, but are
deferred rather than fixed in the same pass -- each for a reason noted below,
generally because the correct fix requires a design decision (TLS? a Host
allowlist? a privilege-escalation mechanism for Core service control?) this
document should not make unilaterally, or because the finding's blast radius
is broad enough to deserve its own dedicated review rather than a patch
appended to an already-large change.

- **F09 -- LAN exposure.** Confirmed: `agent/api` only wraps mutating
  endpoints in `RequireAdmin` (`server.go`); read endpoints
  (`/api/v1/status`, `/api/v1/system`, `/api/v1/updates`, ...) are
  unauthenticated by design, there is no `Host`/`Origin` allowlist anywhere
  in the HTTP stack, and the admin bearer token travels in cleartext (no TLS
  termination exists in this codebase at all). `QRX_DASHBOARD_BIND=lan` is
  opt-in and off by default (`docs/installer.md#dashboard-access`), which
  bounds but doesn't eliminate the exposure once an operator does turn it
  on. Deferred: closing this properly means deciding on a security posture
  (TLS, a Host/Origin allowlist for LAN bind, or authenticating read
  endpoints too) rather than a narrow bug fix.
- **F10 -- resource limits.** Confirmed: no request body size limits, no
  per-request timeouts beyond `http.Server`'s zero-value (no) defaults, no
  cap on concurrent SSE connections, and `Policy.Locked` (`agent/updates`)
  is a persisted boolean, not a mutex -- it prevents a *second admin
  request* from starting a concurrent install, but doesn't serialize
  concurrent goroutines within this process. Deferred as a general
  hardening pass rather than one fix.
- **F12 -- Core service control lacks least privilege.** Confirmed:
  `agent/platform.New()` returns a `Systemd` service manager with
  `UseSudo: false` by default, so `qrxd.service` start/stop/restart calls
  have no privilege-escalation path at all as currently wired:
  `installer/linux/qrx-agent-sudoers` exists in the repo but neither
  `install.sh` nor `cmd/agentd/main.go` reference it or `UseSudo`
  anywhere -- it is a dead, never-installed policy file, so Core service
  control genuinely cannot work against a real `qrxd.service` owned by a
  different user today; `QRXCoreUpdateManager`'s `StartCore` callback in `cmd/agentd/main.go`
  ignores the `dir` argument it's given; no `Probe` is configured, so
  Core health checks report "no health probe configured" rather than a
  real liveness signal; `QRXCoreUpdateManager.Update` does not cross-check
  the caller-supplied profile's target version the way `SwitchVersion`
  does. Deferred: this is the real "Core/adapter binding" work the audit's
  closing instructions name as the next priority tier, and is substantial
  enough (a least-privilege helper design, a real health probe, wiring
  `dir` through) to warrant its own pass.
- **F13 -- Mock-mode fallback is silent.** Confirmed: an explicitly-named
  but nonexistent `-config` path falls back to Mock-mode defaults with no
  error; `GET /api/v1/version` reports an assumed `qrx_core_version` even
  with no real node connected; `/health` is pure liveness, not readiness.
  Deferred: fixing the silent-fallback case cleanly means deciding whether
  a bad `-config` path should be a hard startup failure (a behavior
  change worth flagging to operators, not a drive-by patch).
- **F14 -- re-running `install.sh` doesn't match its own documentation.**
  Confirmed: `docs/installer.md#upgrades` says re-running `install.sh`
  "does not re-install or touch your existing configuration/database", but
  the actual code only *warns* when it detects an existing install
  (`main()`'s `if [[ -f /etc/systemd/system/qrx-agent.service ]]` check)
  and then proceeds through `install_release`/`install_systemd_service`
  regardless, overwriting the binary, dashboard, and systemd unit outside
  the OTA system's staged/health-checked/rollback-safe path. Deferred:
  the fix is either making the installer actually stop (matching the
  docs) or correcting the docs to match the code -- a product decision,
  not fixed here to avoid changing installer behavior without that
  decision.
- **F15 -- npm advisories.** `dashboard/package.json` exists but no
  lockfile is currently committed, so a reachability assessment (does this
  project's actual production usage hit the vulnerable code path in each
  of the 4 advisories the audit named -- a Vite Windows path check, a Vite
  sourcemap traversal, an esbuild dev-server issue, and a React Router
  redirect/SSR-hydration issue) needs a committed lockfile to audit
  against reproducibly. Deferred pending that.

Every fix above was verified against the actual current source before
being made (never assumed from the audit report's prose), and every
deferred finding above was independently reproduced or confirmed by
reading the relevant code, not merely restated from the report.

## Reporting

See [`SECURITY.md`](../SECURITY.md) for how to report a vulnerability.
