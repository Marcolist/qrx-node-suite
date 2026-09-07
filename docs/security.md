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
| **Malicious dashboard asset** | Dashboard artifacts go through the same checksum+signature pipeline as every other component; `components.Dashboard.Extract` additionally rejects any tar entry that would escape the extraction directory (zip-slip/path traversal) and refuses non-regular-file/non-directory entries (symlinks, devices) outright. |
| **Rollback abuse** (an attacker forcing repeated rollback to reintroduce a known-vulnerable version) | Rollback targets are only ever versions this Agent itself previously verified and activated (`store.Store`'s `previous` pointer) -- rollback never fetches or trusts anything new from the network. `Manager.Rollback` and `QRXCoreUpdateManager`'s binary rollback are both audit-logged (`ADMIN_ROLLBACK_*` / `ADMIN_SWITCH_QRX_CORE`), so repeated rollback activity is visible in `audit_log`, not silent. |

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
