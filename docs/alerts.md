# Alert engine

`agent/alerts.Engine` evaluates a set of `Rule`s against a `Snapshot` of
current Agent state (health, system metrics, node status, pending updates)
on each polling tick, and manages the create/resolve lifecycle so a rule
that keeps firing across many ticks produces exactly one open alert, not
one per tick.

## Rules

A `Rule` is a pure function `Snapshot -> (firing bool, message string)`.
`DefaultRules()` ships:

| Rule ID | Severity | Fires when |
|---|---|---|
| `node_offline` | critical | Guardian state is `OFFLINE`. |
| `node_unhealthy` | warning | Guardian state is `UNHEALTHY`. |
| `disk_low` | warning | Free disk space < 10%. |
| `cpu_sustained_high` | warning | CPU usage > 90%. |
| `throttled` | warning | Host reports active throttling (Raspberry Pi undervoltage/thermal). |

Add more via `Engine.AddRule` -- e.g. update-available notifications, wired
from `agent/updates.CheckResult` through `Snapshot.UpdateAvailable`.

## Persistence and events

`Engine` depends on two small interfaces, not concrete packages, to avoid
an import cycle and keep it trivially testable:

- `Store` (implemented by `agent/storage.AlertStore` against the `alerts`
  table): `Create`, `OpenByRule`, `ResolveByRule`.
- `Publisher` (implemented by `agent/events.Bus`): `Publish`, used to emit
  `alert.created` / `alert.resolved` onto the SSE stream.

## Delivery

Alert creation is separate from delivery. `cmd/agentd` subscribes to
`alert.created` events and, if Telegram is configured
(`docs/telegram.md`), forwards them via `agent/telegram.Client.SendMessage`
using `telegram.FormatAlert`. Alerts always land in `GET /api/v1/alerts`
and the dashboard's Alerts page regardless of whether Telegram is
configured -- Telegram is an additional notification channel, never the
only record.
