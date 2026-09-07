# Architecture

## Purpose

QRX Node Suite is an operations, monitoring, and management layer around QRX Core. It
makes running and monitoring a QRX node or validator easier for node operators, on
Linux servers, Raspberry Pi, Windows, macOS, mini PCs, and VPSes.

## The one rule everything else follows

```
QRX Core
    |
    v
QRX Adapter
    |
    v
QRX Agent
    |
    +---- Local REST API
    |
    +---- Event Stream (SSE)
    |
    +---- Web Dashboard
    |
    +---- Guardian
    |
    +---- Telegram
    |
    +---- Local SQLite
    |
    +---- Future Public Telemetry Client
    |
    +---- Future Mobile Pairing API
```

QRX Node Suite **never bypasses the QRX Adapter layer** to talk to QRX Core directly from
the Agent's other subsystems (Guardian, alerts, API handlers, dashboard). Every one of
those consumes normalized data that has already passed through an adapter. This is
enforced structurally: `agent/qrx` (the process/RPC transport) is only imported by
`agent/adapters/*`; nothing else in the module is allowed to import it. See
`agent/adapters/README.md`.

## Why this boundary exists

QRX Core changes its interface between releases (0.0.6 -> 0.0.7 added VELOCITY, for
example). If every subsystem queried QRX Core directly, a single upstream change would
require touching the whole codebase, and there would be no single place to reason about
"what does this Node Suite version actually support against this Core version." Routing
everything through `agent/adapters` means:

- Upgrading QRX Core support is: write/update one adapter, update the compatibility
  matrix, done.
- The Agent can run in Mock mode (no real QRX Core) for development and CI by swapping
  the adapter.
- Compatibility (`docs/qrx-compatibility.md`) is explicit and testable instead of
  scattered assumptions.

## Core engineering principles (enforced, not aspirational)

These are checked in code review and, where practical, in tests:

1. QRX Core is authoritative for all chain/validator state.
2. No consensus logic, no fabricated blockchain state, no independently computed wallet
   balances or validator rewards (unless QRX exposes the authoritative figure and the
   calculation is documented by QRX itself).
3. No wallet file edits, no private keys, seeds, or validator signing keys stored,
   logged, or transmitted anywhere in this codebase.
4. No local secrets exposed through the dashboard or API.
5. The QRX Adapter is a hot-swappable interface (`agent/adapters`), never hardcoded
   integration scattered through the Agent.
6. Mock mode (`agent/adapters/mock`) is never visually indistinguishable from a real
   node in the dashboard — a persistent "SIMULATION MODE" indicator is required whenever
   it's active.
7. Local operation has zero dependency on any cloud service. Public telemetry
   (`agent/telemetry`) is opt-in and off by default. Remote control defaults to disabled.
   Mobile access (`agent/pairing`) defaults to read-only.
8. Central server (future telemetry/map/mobile backends) failure never affects QRX Core
   or local monitoring.
9. Fail safely: an adapter/version mismatch blocks integration rather than silently
   running degraded; see `docs/qrx-compatibility.md`.
10. Structured logging (`agent/logging`, `log/slog`), typed data models
    (`agent/models`), versioned APIs (`/api/v1`).
11. Unknown/unsupported QRX values are represented explicitly (`models.Unavailable`,
    `models.Unsupported`) — never a zero value standing in for "we don't know."
12. Raspberry Pi ARM64 is a first-class build target, not an afterthought.
13. Errors are never silently swallowed; they are logged, and where user-facing,
    surfaced as an explicit state rather than a default.

## Components

| Component | Language/stack | Ships as |
|---|---|---|
| QRX Agent | Go, stdlib only (ADR-001) | one static binary per platform |
| QRX Dashboard | React + TypeScript + Vite | static assets, embedded in the Agent binary via `go:embed` when built |
| QRX Guardian | Go, part of the Agent process | in-process health/recovery state machine |
| Installer | shell/PowerShell + systemd/launchd unit files | per-platform install scripts |

## Data flow (steady state)

1. `agent/qrx` polls `qrxd` via `qrx-cli` (or a documented local RPC transport, once
   confirmed — see `docs/qrx-0.0.7-interface.md`) on a fixed schedule
   (`docs/configuration.md#polling`).
2. The active adapter (`agent/adapters`) normalizes raw QRX output into typed models
   (`agent/models`), explicitly marking anything QRX didn't return as unavailable.
3. Normalized state is cached centrally; API handlers and the dashboard read the cache,
   they do not each re-poll QRX Core.
4. State changes are published to `agent/events`, which fans out over SSE
   (`GET /api/v1/events`) and feeds `agent/guardian` and `agent/alerts`.
5. `agent/guardian` evaluates health and can trigger recovery actions through
   `agent/services` (restart the QRX service), never by talking to QRX Core directly.
6. `agent/storage` persists history (metrics, snapshots, alerts, update history, audit
   log) to SQLite with bounded retention (`docs/configuration.md#retention`).

## Version management and OTA updates

Covered in full in [`docs/updates.md`](updates.md). Summary: every component (Agent,
Dashboard, each Adapter, Node Suite release, installer, API, telemetry protocol,
compatibility profile, config schema) has its own independently tracked version. QRX Core
updates are handled by a separate, more conservative manager than everything else. All
updates are staged, verified (checksum + signature), health-checked, and rollback-capable.
