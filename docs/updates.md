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
  channels resolve to the newest release tagged `<channel>-...`. Asks for a
  release asset named `manifest-linux-<GOARCH>.json`
  (`cmd/agentd/sources.go`'s `buildUpdateSource`), not a single shared
  `manifest.json`: the `agent` component's artifact is a raw binary
  (`components.Agent.Extract`), so it has to match the architecture this
  process is actually running on, and the manifest format itself
  (above) has no per-architecture field of its own -- see
  `.github/workflows/release.yml`'s "Build/Sign OTA update manifests"
  steps, which publish one manifest per architecture this project ships
  for (currently `amd64`, `arm64`).
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

`Stage(version)` refuses outright if `version` is already the component's
`current` one (`store.ErrAlreadyActive`) or its `previous` one
(`store.ErrAlreadyPrevious`), and `Manager.Install` checks for both even
earlier (`ErrAlreadyInstalled` / `ErrAlreadyPrevious`, before ever calling
`Stage`): a release's directory is purely a function of its version string
(`releases/<version>`), so staging either would reuse that live directory in
place, and a failed extraction there would then have its cleanup delete a
release a pointer still references -- the running process's own binary for
`current`, or the only rollback target for `previous`. Fix for an external
security audit's F08 finding (`current`) and R03, an external re-review of
that fix (`previous` -- reachable via `AllowDowngrade`, which
`CheckNotDowngrade` explicitly permits). A caller wanting to reactivate the
previous version should call `Rollback` instead of reinstalling it through
`Install` -- an atomic pointer swap needing no download or re-extraction.

`Store.BootstrapCurrent(version)` records `version` as `current` directly, no
staging or promotion, and only if nothing is recorded yet (a no-op once any
real update has ever promoted something -- it never overwrites real state).
`cmd/agentd`'s `buildControllers` calls it for `agent` and any active adapter
at every startup, using the running binary's own compiled-in version. Without
it, a freshly bootstrapped install has an empty
`Current()` until its first-ever OTA update succeeds -- and
`manifest.CheckNotDowngrade`/`CheckNotReplayed` both explicitly treat an empty
current-version/last-seen as "nothing to compare against yet" and let
anything through. `BootstrapCurrent` closes that gap for the version-tracking
piece of it. `install.sh` additionally places the Agent binary in this store
and makes systemd's fixed launcher path resolve through `current`, so later
promotions become executable after the requested service restart. Fix for
the R05 finding (external security re-review of the F07 fix).

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
then returns `PendingRestart: true`.

Making that promotion actually take effect needs two things working
together, both fixed by an external re-review (R05) after the original
design shipped only the store-side half:

1. **`${QRX_PREFIX}/bin/agentd` has to resolve to whatever the store
   promotes.** `Store.Promote()` only ever flips a symlink *inside* the OTA
   store (`<DataDir>/components/agent/current -> releases/<version>`) --
   it has no way to touch systemd's `ExecStart`, which is a fixed path.
   `install.sh`'s `bootstrap_agent_ota_store()` closes that gap once, at
   install time: it lays the initial binary into the OTA store's own
   layout (instead of copying it straight to `${QRX_PREFIX}/bin/agentd`)
   and makes `${QRX_PREFIX}/bin/agentd` a symlink into the store's
   `current` release. `Store.Promote()`'s existing atomic rename of
   `current` then transitively repoints what `ExecStart` execs, forever
   after, with no further installer involvement and no systemd sandbox
   changes -- only the *inner* `current` symlink, inside `QRX_DATA_DIR`
   (already in `ReadWritePaths`), ever changes again.
2. **The process has to actually exit for a restart to happen at all.**
   `Deps.RequestSelfRestart` (`agent/api`) is called by the `updates/install`
   and `updates/rollback` HTTP handlers whenever a result reports
   `PendingRestart: true`, after the response is written so a polling
   caller still sees it. `cmd/agentd` wires this to the same graceful-
   shutdown path SIGTERM already uses (cancel the top-level context ->
   `srv.Shutdown` -> `run()` returns -> process exits 0), which is what
   lets systemd's `Restart=always` start a fresh process at all. Before
   this existed, nothing ever read `PendingRestart`: a self-update could
   verify, stage, and promote a new binary correctly and never actually
   run it.

Either half missing is enough to make self-update a no-op that reports
success -- see `docs/security.md` for how the external re-review found
this. The restarted process calls `Manager.ResumeSelfUpdate`, which runs
the real health check now that the new code is actually running, then
commits or rolls back the store pointers; if it rolled back, `cmd/agentd`
exits again (`selfUpdateRollbackRequiresRestart`, scoped to `agent` only --
an `adapter_*` rollback is just a version-tracking pointer change, since
adapter code is compiled into this same running binary, so nothing
restarting would accomplish) so the *next* restart lands on the reverted
binary rather than continuing to run the one that just failed its health
check.

**Crash-loop guard**: `ResumeSelfUpdate` alone only protects against "starts,
but unhealthy" -- it can't run at all if the new binary crashes before it
gets that far, which would otherwise leave a supervisor configured with
`Restart=always` retrying the crashing binary forever. `cmd/agentd` closes
this gap with `updates.BootGuard`, which runs as the very first thing on
every process start, before adapter selection or anything else that could
itself crash:

1. `BootGuard.PendingComponents` scans for any `agent`/`adapter_*` still
   carrying a pending self-update marker (set by `Install`, normally cleared
   by `ResumeSelfUpdate`) -- i.e. components whose most recent self-update
   hasn't been confirmed healthy yet.
2. For each one, `BootGuard.CheckAndRecordAttempt` increments a persisted
   boot-attempt counter, capped at `config.UpdatesConfig.MaxBootAttempts`
   (default 3, wired into `BootGuard.MaxAttempts`). Within budget, startup
   continues normally -- if this boot is the one that
   reaches `ResumeSelfUpdate`, that clears the counter, since reaching it at
   all proves the crash loop is over regardless of the health check's own
   outcome.
3. Once the counter exceeds `MaxBootAttempts`, BootGuard forces the same
   rollback `ResumeSelfUpdate` would have performed on an unhealthy new
   version -- reverting the component store's `current`/`previous`
   pointers, recording a `rolled_back` update-history entry and an audit
   event, and clearing both the pending marker and the counter -- then
   `cmd/agentd` exits immediately so the supervisor's next restart runs the
   reverted, previously-healthy binary.

See `agent/updates/bootguard.go`.

BootGuard's own logic runs *inside* the new binary's Go runtime, so it can
never catch a restart where the kernel fails to `exec()` the binary at all
(wrong architecture, a truncated/corrupted extraction, a stripped
executable bit) -- there is no Go code running yet to count that attempt.
The shipped systemd unit (`install.sh`'s `install_systemd_service()`,
`installer/linux/qrx-agent.service`) sets `StartLimitIntervalSec=300` and
`StartLimitBurst=8` as a complementary, OS-level circuit breaker for
exactly that case: once systemd itself has retried that many times within
the window, it stops and marks the unit `failed` instead of looping
forever, without ever needing BootGuard's own SQLite-backed counter to run
at all. Set higher than `BootGuard.MaxAttempts` so BootGuard gets the first
chance to self-heal via rollback before this harder limit ever triggers.
Fix for an external security audit's F07 finding.

### Dashboard and compatibility profiles

These do **not** require a process restart:

- **Dashboard** (`components.Dashboard`): a `.tar.gz` of static assets,
  extracted (with zip-slip protection) into `releases/<version>/`. The
  Agent serves whichever directory `Store.CurrentDir()` currently resolves
  to on each request, so activation is instant. Health check: the newly
  activated directory has an `index.html`. `cmd/agentd`'s HTTP handler
  falls back to `cfg.DashboardDir` only for developer/manual installs or
  older deployments with no active dashboard store entry. Current
  `install.sh` releases seed the verified bundled dashboard and its version
  into the store immediately, so update status does not offer the installed
  build again and the store is the source of truth from first boot. Before
  the fix for an external audit's F07 finding, the
  handler was wired to the static `cfg.DashboardDir` unconditionally, so a
  dashboard OTA install could verify/stage/promote and report success with
  zero effect on what was actually served.
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
