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
| **A "successful" OTA update with no real effect** (a compromised or buggy update source could -- or, before this fix, simply a normal dashboard update always did -- report success while the old, potentially-vulnerable code keeps serving every request) | `cmd/agentd`'s dashboard HTTP handler now resolves the OTA store's `Store.CurrentDir()` on every request (falling back to the install-time-seeded directory only when nothing has ever been promoted), so a promoted dashboard update takes effect on the very next request -- matching `components.Dashboard`'s `RequiresRestart()==false` design and `Manifest.Validate`'s guarantee that whatever got promoted was itself signature-verified. As of the fix for an external audit's F07 finding. |
| **Crash loop the in-process guard can't see** (a self-update binary so broken the kernel can't even `exec()` it -- wrong architecture, a truncated/corrupted extraction, a stripped executable bit -- never gives `BootGuard` a chance to run at all, since that requires the Go runtime to already be executing) | The shipped systemd unit sets `StartLimitIntervalSec=300`/`StartLimitBurst=8` as an OS-level circuit breaker independent of `BootGuard`'s own SQLite-backed counter: once systemd itself has retried that often within the window, it stops and marks the unit `failed` rather than looping forever. Set higher than `BootGuard.MaxAttempts` so BootGuard gets the first chance to self-heal via rollback. As of the fix for an external audit's F07 finding; see `docs/updates.md`'s "Crash-loop guard" section. |

## Installer threat model

`install.sh` runs as root by necessity (it creates a system user and a systemd
unit), which makes anywhere it writes as root, through a path an unprivileged
process might influence, a privilege-escalation surface distinct from the OTA
threat model above.

| Threat | Mitigation |
|---|---|
| **Installer log symlink attack** (a compromised, unprivileged `qrx-agent` process plants a symlink at `install.log`'s path so a later root-run `install.sh` truncates an arbitrary root-owned file instead) | `QRX_LOG_DIR` is root-owned (`root:qrx-agent`, mode `0750`), not agent-writable, so `qrx-agent` can no longer place anything there at all; `setup_logging()` additionally refuses to write through anything at that path that isn't a plain regular file, removing it first -- covering a system upgraded from an older, vulnerable installer that still has an agent-owned log directory left over from its first install. As of the fix for an external audit's F04 finding; see `docs/installer.md#installer-log-directory`. |
| **Uninstaller delete-target hijack** (a compromised, unprivileged `qrx-agent` process points `uninstall.sh --remove-qrx-core`'s marker file at an arbitrary directory, which is then `rm -rf`'d as root) | The marker (`qrx-core-installed-by-this-installer`) lives under `QRX_CONFIG_DIR` (`root:qrx-agent`, mode `0750`), never the agent-writable `QRX_DATA_DIR`, so `qrx-agent` cannot plant or rewrite it at all. As a second, independent line of defense, `uninstall.sh` resolves the marker's content (following any symlinks) and refuses to remove anything that doesn't fall under the documented QRX Core install root (`/opt/qrx`) -- never "/", "/etc", or a symlink escape. As of the fix for an external audit's F05 finding; see `docs/installer.md#qrx-core-removal-safety`. |

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

## Reporting

See [`SECURITY.md`](../SECURITY.md) for how to report a vulnerability.
