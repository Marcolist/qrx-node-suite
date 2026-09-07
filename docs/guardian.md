# QRX Guardian

Guardian (`agent/guardian`) is the Agent's local health and recovery state
machine. It never talks to QRX Core or a process table directly -- it only
classifies already-normalized `Signals` (the active adapter's reported
health, whether the `qrxd` process is running per `agent/platform`) and, on
a bad transition, requests a restart through `agent/services`.

## States

```
HEALTHY -> DEGRADED -> UNHEALTHY -> OFFLINE
```

| State | Meaning |
|---|---|
| `HEALTHY` | Recent successful poll, adapter reports healthy, node online. |
| `DEGRADED` | No successful poll in `DegradedAfter` (default 30s). |
| `UNHEALTHY` | No successful poll in `UnhealthyAfter` (default 2m), or the adapter itself reports unhealthy. |
| `OFFLINE` | The `qrxd` process isn't running, node isn't online, or no successful poll in `OfflineAfter` (default 5m). |

Every threshold is configurable (`guardian.Config`).

## Automatic recovery

On any transition **into** `UNHEALTHY` or `OFFLINE`, Guardian requests a
restart -- deliberately regardless of whether the process is technically
still running: a hung, unresponsive-but-alive `qrxd` is exactly the case an
automatic restart exists for, not just a crashed one.

Restarts are bounded (`docs/architecture.md` principle 26, never silently
retry forever):

- `MaxAutoRestarts` (default 3) within `RestartWindow` (default 15m) --
  once exhausted, Guardian stops attempting restarts and stays
  `UNHEALTHY`/`OFFLINE` until an operator intervenes.
- `MinRestartInterval` (default 1m) cooldown between attempts.

`Guardian.RestartAttemptsInWindow()` exposes the current count for
`GET /api/v1/status`.

## Events

Every state transition invokes an `onEvent(old, new)` callback, wired by
`cmd/agentd` to publish `models.EventServiceRestarted`-adjacent events onto
`agent/events.Bus` -- not every `Evaluate` call, only actual changes, so a
steadily healthy node doesn't spam the event stream.
