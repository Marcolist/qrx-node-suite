# Version management and OTA updates

QRX Node Suite tracks every component's version independently and updates
each one on its own, verified, staged, rollback-capable path. Nothing here
ever couples a Dashboard release to an Agent release, an adapter release to
a Node Suite release, or any of the above to a QRX Core release. QRX Core in
particular gets its own, deliberately more conservative manager -- see
[QRX Core updates](#qrx-core-updates) below.

## Component version model

`agent/version.ComponentVersionModel` is the structured record; nothing in
it is derived from another field:

```json
{
  "suite_version": "0.1.0",
  "agent_version": "0.1.0",
  "dashboard_version": "0.1.0",
  "adapter": { "name": "qrx007", "version": "1.0.0", "qrx_compatibility": ["0.0.7"] },
  "qrx_core_version": "0.0.7",
  "api_version": "v1",
  "telemetry_protocol_version": 1,
  "config_schema_version": 1
}
```

`GET /api/v1/version` returns this (as `agent/models.VersionInfo`, the
API-facing projection).

## Update channels

`stable` (default) / `beta` / `nightly` / `development`, per component,
via `agent/updates.Policy.SetChannel`. QRX Core is the one exception: its
unconfigured default is `manual`, not `stable` -- see
[QRX Core updates](#qrx-core-updates).

## Update manifest

`agent/updates/manifest.Manifest`:

```json
{
  "manifest_version": 1,
  "channel": "stable",
  "suite_version": "0.2.0",
  "released_at": "2026-03-01T00:00:00Z",
  "components": {
    "agent": { "version": "0.2.0", "url": "...", "sha256": "...", "signature": "..." },
    "dashboard": { "version": "0.2.0", "url": "...", "sha256": "...", "signature": "..." },
    "adapter_qrx007": { "version": "1.1.0", "url": "...", "sha256": "...", "signature": "..." }
  },
  "manifest_signature": "..."
}
```

`manifest_signature` is an addition beyond the minimal example most specs
show: an Ed25519 signature over the whole manifest (`manifest.CanonicalPayload`),
not just each component individually. Without it, an attacker who controls
the update server could splice a component entry from an old, still
validly-signed manifest into a new one, or swap which components ship
together, without forging any single per-component signature. See
[OTA security](#ota-security).

Every component entry additionally carries its own `signature`: an Ed25519
signature over the raw SHA256 digest bytes. `manifest.VerifyManifestSignature`
must pass before ANY field is trusted; `manifest.VerifyArtifact` (checksum +
per-artifact signature) must pass before a downloaded file is staged.

## Update source abstraction

`agent/updates/sources.Source`, implemented by:

- `GitHubReleaseSource` -- `stable` is the repo's latest release; other
  channels resolve to the newest release tagged `<channel>-...`.
- `StaticManifestSource` -- a fixed URL template (`.../manifests/{channel}.json`).
- `LocalFileSource` -- offline installs (`docs/updates.md#offline-installation`).
- `DevelopmentSource` -- fixed in-memory manifests/artifacts, for tests only.

## Atomic component storage

`agent/updates/store.Store` (one per component, rooted at
`store.New(baseDir, component)`):

```
<baseDir>/<component>/
    releases/<version>/...   permanent, content-addressed by version
    current   -> releases/<version>   (symlink; text-file fallback, e.g. Windows without symlink privilege)
    previous  -> releases/<version>
    staged    -> releases/<version>   (present only mid-install)
```

`Stage` -> `Promote` (atomically: old `current` becomes `previous`, staged
becomes `current`, `staged` pointer cleared) -> `RollbackToPrevious` (swaps
`current`/`previous`, so rollback is itself reversible) -> `Prune(retain)`
(deletes old releases beyond `current`+`previous`+`retain` extra, default
`retain=2`). Every pointer write is a temp-file-then-`os.Rename`, atomic on
every target platform -- see `agent/updates/store/atomic.go`.

## Safe update process

`agent/updates.Manager.Install` runs, per component:

```
CHECK -> DOWNLOAD -> VERIFY -> STAGE -> BACKUP (implicit: Promote keeps
"previous") -> STOP (if required) -> INSTALL (Promote) -> START ->
HEALTH CHECK -> COMMIT, or ROLLBACK on any failure from STOP onward.
```

Before any of that: the maintenance update lock (`ErrUpdatesLocked`), the
scheduled update window for non-manual installs (`ErrOutsideWindow`), a
version pin (`ErrPinned`), a downgrade check (`ErrDowngradeBlocked` unless
explicitly allowed), and a compatibility check (`ErrIncompatible` /
breaking-update protection) are all evaluated first -- an update that fails
any of these is never downloaded at all.

### Self-binary components (agent, adapters)

`agent`, and every `adapter_<name>` component, are **self-binary**: the
artifact is a rebuilt Agent binary (adapters compile into the Agent binary
in this codebase -- see `agent/adapters/README.md` and
[Adapter hot reload](#adapter-hot-reload) below). A process cannot
meaningfully health-check code it isn't executing yet, so
`components.SelfBinary` components skip synchronous
STOP/START/HEALTH CHECK/COMMIT: `Install` stages and atomically promotes,
then returns `PendingRestart: true`. `cmd/agentd` is responsible for
exiting so the process supervisor (systemd `Restart=always`, see
`docs/deployment.md`) restarts the process onto the new `current` binary;
the restarted process calls `Manager.ResumeSelfUpdate`, which runs the real
health check now that the new code is actually running, then commits or
rolls back the store pointers.

**Known limitation**: if the new binary crashes before `ResumeSelfUpdate`
ever runs (rather than starting and failing its health check), there is no
crash-loop watchdog yet -- a supervisor configured with `Restart=always`
will keep restarting the crashing binary. `ResumeSelfUpdate` only protects
against "starts, but unhealthy," not "never starts at all." A future
improvement is a boot-attempt counter that forces an automatic rollback
after N consecutive failed starts within a window.

### Dashboard and compatibility profiles

These do **not** require a process restart:

- **Dashboard** (`components.Dashboard`): a `.tar.gz` of static assets,
  extracted (with zip-slip protection) into `releases/<version>/`. The
  Agent serves whichever directory `Store.CurrentDir()` currently resolves
  to on each request, so activation is instant. Health check: the newly
  activated directory has an `index.html`.
- **Compatibility profile** (`components.CompatibilityProfile`): a small
  JSON document, hot-reloaded via a `Reload` callback into whatever
  in-memory holder the rest of the Agent reads from -- no restart, no
  service interruption.

## Adapter hot reload

Per explicit product decision: **no dynamic plugin loading** (no `.so`/
`.dll` adapters). Adapters are Go packages compiled into the Agent binary
and self-register (`agent/adapters/README.md`). Changing which adapter code
is running therefore always means restarting the Agent process -- never QRX
Core. This is accepted as the simpler, more robust choice over cross-platform
plugin loading (especially on Windows), at the cost of a short Agent
restart on an adapter update.

An adapter update's health check (`components.Adapter.HealthCheck`) does
**two independent checks** after restart, and either failing blocks the
update even if the other passes:

1. `active.Health(ctx)` -- a real probe against the live QRX Core, not just
   "the process started."
2. A fresh `compatibility.Matrix` lookup for (adapter name, adapter
   version, detected QRX Core version). A mock/stub adapter that always
   reports healthy would otherwise sail through despite being
   `UNSUPPORTED` for the running Core version.

## Rollback

Every component supports `Manager.Rollback(ctx, component, actor)`
independent of any in-progress install -- swap `current`/`previous`, restart
if required, health-check the rollback target itself (if *that* fails too,
it swaps back and returns a loud error: both versions are then suspect).
`Prune`'s `retain` count (default 2) is what bounds how far back rollback
can reach.

## Update history and audit

Every attempt -- `started`, `succeeded`, `failed`, `rolled_back`, `blocked`
-- is a row in SQLite's `update_history` table
(`agent/storage.UpdateHistoryStore`), append-only. Every administrative
action (`ADMIN_UPDATE_AGENT`, `ADMIN_SWITCH_ADAPTER`, `ADMIN_SWITCH_QRX_CORE`,
`ADMIN_ROLLBACK_DASHBOARD`, ...) is a row in `audit_log`
(`agent/storage.AuditLogStore`), with the authenticated actor.

## Dry-run planning

`Manager.Plan(ctx, componentNames)` runs `Check` for several components
without downloading or installing anything -- the basis for
`POST /api/v1/updates/plan` (see `docs/qrx-node-suite` API section):

```
Agent      0.2.0 -> 0.3.0      OK
Dashboard  0.3.2 -> 0.4.1      OK
Adapter    1.0.0 -> 1.1.0      OK
QRX Core   0.0.7                unchanged

Compatibility: OK
Restart required: Agent only
Rollback available: yes
```

Each `CheckResult` reports current/latest, whether an update is available,
pinned state, compatibility, block reason (if blocked), whether activating
it requires a restart, and whether a rollback target already exists --
everything the dashboard's Settings -> Updates page's three-tier
(Current / Available / Installed) display needs, without side effects.

## QRX Core updates

QRX Core is never routed through `Manager` -- it has its own
`agent/updates.QRXCoreUpdateManager`, which differs in every way that
matters for something this critical:

- **Default policy is `manual`**, not `stable` like everything else
  (`Policy.ChannelOrDefault(ctx, "qrx_core", ChannelManual)` -- note this is
  NOT the same default `Policy.Channel` gives every other component).
- **Validator nodes hard-disable automatic updates**, full stop --
  `IsValidatorNode: true` makes `AutomaticEnabled` always return `false`,
  with no policy setting that overrides it. "Automatic upgrades must never
  force a validator onto an untested QRX Core version" is enforced in code,
  not just documented.
- **The QRX data directory is never referenced or touched.** Only the
  versioned binary tree under `QRXCoreUpdateManager.BaseDir` is managed;
  blockchain data lives entirely outside this manager's scope.
- **Validation after activation** uses `QRXCoreHealthProbe` (process
  running, network connected, block height actually progressing -- sampled
  twice with a delay, not just "process didn't crash" -- peer count,
  validator state where available), not a generic `Controller.HealthCheck`.
  Any failed dimension triggers an automatic binary rollback
  (`rollbackBinary`), recorded and audited.

### QRX Core version selection

Multiple locally installed versions are supported (same `store.Store`
layout as everything else, rooted at a QRX-specific `BaseDir`, e.g.
`/opt/qrx/versions/0.0.6`, `/opt/qrx/versions/0.0.7`,
`/opt/qrx/current -> 0.0.7`). `SwitchVersion` activates an already-installed
version directly (no download) -- the "Switch Version" dashboard action;
`Update` fetches, verifies, and installs a new one -- the "Check for
Updates" -> "Update" flow.

### Core version switching safety

Neither `SwitchVersion` nor `Update` will proceed without an explicit,
per-dimension compatibility judgement from the target version's
`version.CompatibilityProfile.CoreVersionSwitchSafety`:

| Dimension | Checked |
|---|---|
| Blockchain data | `BlockchainData` |
| Configuration | `Configuration` |
| Wallet | `Wallet` |
| Adapter | `Adapter` |
| Network | `Network` |

Each is a **tri-state** (`compatible` / `incompatible` / unset-meaning-unknown)
-- deliberately not a bool, so "nobody has verified this" can never
collapse into "yes, safe." `CheckSwitchSafety`:

- Any dimension explicitly `incompatible` -> **blocked, always**, no
  override possible (`ErrSwitchSafetyIncompatible`). An "expert override"
  is for proceeding past uncertainty, never past a known-bad combination.
- Any dimension merely unknown (unset) -> blocked unless
  `SwitchOptions.ExpertOverride` is explicitly set
  (`ErrSwitchSafetyUnknown`).

Both shipped profiles (`agent/data/compatibility-profiles/*.json`)
currently leave every dimension unset -- they are unverified (see
`docs/qrx-0.0.7-interface.md`), so both are blocked from automatic use
until a human fills them in with real judgements.

## Version pinning

`Policy.SetPin(ctx, component, version)` pins a component to an exact
version; `Install` refuses any manifest offering a different version for a
pinned component (`ErrPinned`). This is how a validator stays on a tested
QRX Core / adapter combination indefinitely regardless of what's published
upstream.

## Update lock and scheduled windows

`Policy.SetLocked(ctx, true)` blocks every automatic AND manual install
(manual can bypass with `InstallOptions.Force: true`, always audited) --
the "disable all automatic Node Suite component updates during an important
network event" control. `Policy.SetWindow` restricts non-manual installs to
a weekly time window (`ScheduledWindow.InWindow`); manual, operator-triggered
installs are exempt from the window but not the lock. QRX Core is not
subject to the window at all -- it's manual by default regardless.

## Breaking update protection

`manifest.CheckCompatibility` blocks an update whose `ReleaseNotes` declares
a `required_qrx_version`, `required_adapter_version`, or
`min_suite_version` the running system doesn't currently satisfy
(`agent/version.Satisfies` range syntax). For adapter components, an
additional matrix lookup (`CheckAdapterCompatibility`) blocks the update if
the compatibility matrix doesn't list the target combination as at least
`EXPERIMENTAL`.

## Configuration and database migrations

Node Suite configuration is versioned (`config_schema_version`) with its own
migration flow (backup -> validate old -> migrate -> validate new ->
activate -> rollback on failure) -- see `agent/config`. The SQLite schema is
independently versioned via `agent/storage.Migrate`, append-only numbered
files under `agent/storage/migrations/`, tracked in a `schema_migrations`
table -- see `agent/storage/migrate.go` and its "multi-statement footgun"
note in `agent/storage/sqlite/README.md`.
