# QRX compatibility matrix and profiles

QRX Node Suite tracks compatibility explicitly. It never infers whether an
adapter works with a QRX Core version by comparing version numbers — see
`agent/version/compatibility.go` and its tests
(`agent/version/compatibility_test.go`, in particular
`TestMatrixLookupNeverInfersFromVersionNumbers`).

## The matrix

`agent/data/compatibility-matrix.json`, loaded via `version.LoadMatrixFile`,
is a flat list of explicit rows:

```json
{
  "qrx_core_version": "0.0.7",
  "adapter_name": "qrx007",
  "adapter_version_range": "^1.0.0",
  "dashboard_version_range": ">=0.2.0",
  "status": "SUPPORTED",
  "notes": "Primary supported combination."
}
```

`Matrix.Lookup(qrxCoreVersion, adapterName, adapterVersion)` matches a row by
**exact** `qrx_core_version` + `adapter_name` equality, then evaluates
`adapter_version_range` against the adapter's own reported version. A
combination with no matching row is `UNKNOWN` — not "probably fine because
the version numbers are close." `0.0.71` is not treated as compatible with
whatever `0.0.7` supports just because they look similar.

### Compatibility states

| State | Meaning |
|---|---|
| `SUPPORTED` | Validated, recommended for production use. |
| `PARTIAL` | Works, but with known missing functionality (see the row's `notes`). |
| `EXPERIMENTAL` | Runs, but has not been validated for validator workloads. |
| `UNSUPPORTED` | Explicitly known not to work. |
| `UNKNOWN` | No row exists. Treated the same as `UNSUPPORTED` for automatic selection — see below. |

### How the matrix drives adapter selection

`adapters.Registry.SelectAutomatic` (the startup flow) calls
`Matrix.BestAdapterFor(qrxCoreVersion)`, which only ever returns a row whose
status is `SUPPORTED`, `PARTIAL`, or `EXPERIMENTAL`, preferring in that
order. `UNSUPPORTED` and `UNKNOWN` rows — and any adapter named `"UNKNOWN"`
— are never auto-selected. If nothing usable is found, or the recommended
adapter isn't installed in this build, QRX integration is left unsupported
rather than silently falling back to an incompatible adapter
(`agent/adapters/registry.go`, `ErrNoCompatibleAdapter`).

`Registry.DisableIncompatible(qrxCoreVersion)` re-evaluates every installed
adapter and disables (for the default/automatic path) any whose status comes
back `UNSUPPORTED` or `UNKNOWN`. `PARTIAL`/`EXPERIMENTAL` adapters stay
selectable manually with a warning (dashboard's Settings → Adapter page,
"Manual override") — they are not blocked outright, since operators may have
a legitimate reason to run one.

## Compatibility profiles

`agent/data/compatibility-profiles/qrx-<version>.json`
(`version.CompatibilityProfile`) is a separate, independently versioned
document per QRX Core version, describing what that version actually offers:
known CLI commands, feature flags, the adapter it requires, known bugs,
platform notes, and — critically — `core_version_switch_safety`.

### Core version switching safety

Before letting an operator switch an installed node onto a different QRX
Core version, `docs/updates.md#core-version-switching-safety` requires
checking five dimensions: blockchain data, configuration, wallet, adapter,
and network compatibility. Each is a `version.SwitchCompat` tri-state
(`"compatible"`, `"incompatible"`, or unset/unknown) — deliberately not a
bool, so "nobody has verified this" can never collapse into "yes, safe."

`SwitchSafety.AllKnown()` must be true, and `AnyIncompatible()` must be
false, before an automatic switch is permitted; otherwise it is blocked
pending an explicit expert override (see `docs/updates.md`). Both shipped
profiles currently leave all five dimensions unset, because they are
unverified — see `docs/qrx-0.0.7-interface.md`.

## Feature capability detection

Matrix rows and compatibility profiles describe what *should* be available.
Adapters additionally do **runtime** capability detection
(`Adapter.Capabilities`), probing optional commands
(`getvalidatorstatus`, `getblockproducerinfo`, `getvelocityinfo`,
`getnoncelanes`) and recording whether each one actually answered
successfully. This is what lets QRX Node Suite stay resilient to a QRX Core
point release that adds or removes a command without a matching Node Suite
release — see `docs/architecture.md#feature-capability-detection`.

## Updating the matrix/profiles independently

Both files are plain JSON, versioned (`matrix_version`, `profile_version`),
and OTA-updateable as the `compatibility_profile` component
(`docs/updates.md#ota-update-system`) — a new compatibility judgement or a
corrected profile does not require an Agent binary release.
