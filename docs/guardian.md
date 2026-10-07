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
| `DEGRADED` | A node-status or adapter-health probe failed, no first successful poll yet, or no successful poll in `DegradedAfter` (default 30s). |
| `UNHEALTHY` | No fully successful liveness poll in `UnhealthyAfter` (default 2m). |
| `OFFLINE` | The `qrxd` process isn't running, or no fully successful liveness poll in `OfflineAfter` (default 5m). |

A fully successful liveness poll requires **both** online node status and a
successful adapter health probe in the same polling cycle. Partial success
does not refresh `Signals.LastSuccessfulPoll`, so a permanently failing
health probe cannot be masked by successful status queries.

A transient timeout reports `DEGRADED` without requesting a restart while
the last fully successful poll is still within `UnhealthyAfter`. If the
next polling cycle succeeds, the state returns to `HEALTHY` without
consuming the restart budget. If failures persist, the elapsed-time thresholds
above still trigger recovery of an unresponsive running process.

A running process with no successful sample receives an initial grace period
starting at Guardian's first evaluation; missing data is not treated as
already five minutes old. A confirmed stopped process is still immediately
`OFFLINE`, including when it is stopped on the very first evaluation.

Every threshold can be tuned by callers through `guardian.Config`.
`cmd/agentd` currently uses `DefaultConfig()`; these fields are not wired to
the agent's JSON configuration.

## Automatic recovery

On any transition **into** `UNHEALTHY` or `OFFLINE`, Guardian requests a
restart -- deliberately regardless of whether the process is technically
still running: a hung, unresponsive-but-alive `qrxd` is exactly the case an
automatic restart exists for, not just a crashed one.
An initially stopped process also requests recovery; repeated evaluations
of the same state do not issue additional requests.

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
`cmd/agentd` to publish `node.health_changed` with the old and new state.
Only successful supervisor restart calls publish `service.restarted` with
the service name and recovery reason. A transition to `DEGRADED` or back
to `HEALTHY`, a budget-blocked attempt, or a failed restart does not claim
that the service restarted. Requested, failed and completed recovery actions
are logged separately. Only actual state changes publish health events, so
a steadily healthy node doesn't spam the event stream.
