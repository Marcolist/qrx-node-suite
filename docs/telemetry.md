# Public telemetry (future, opt-in)

**Status: not implemented in this codebase yet.** This document records the
contract any future implementation must honor, per
`docs/architecture.md` principle 7: "Public telemetry must always be
optional."

## Hard requirements for whenever this is built

1. **Opt-in, off by default.** No config flag defaults to enabled. An
   Agent that has never had telemetry explicitly turned on sends nothing.
2. **Local operation never depends on it.** Every feature this project
   ships (monitoring, Guardian, alerts, OTA updates, the dashboard) must
   work fully with telemetry permanently disabled. It is additive, never
   load-bearing.
3. **No secrets.** Telemetry payloads must never include wallet addresses
   tied to identity beyond what's already public on-chain, private keys
   (never possible -- this project never holds any), or admin tokens.
4. **`telemetry_protocol_version`** (see `agent/version.ComponentVersionModel`)
   is already a tracked, independent version domain, ready for this to
   version its wire format the same way every other component does.
5. **A `node_suite_id`** (random UUID, generated locally, NOT a wallet or
   validator address) is the intended identity for opt-in registration --
   see `docs/architecture.md`'s "Node identity" notes in the broader
   product brief this repository was built from. Not yet generated or
   persisted anywhere in this codebase.
6. **Central server failure must never affect QRX Core or local
   monitoring** (principle 8) -- a telemetry client must fail silently
   (logged, not surfaced as a node health problem) when it can't reach its
   backend.

## What would need to exist

- An `agent/telemetry` package with a `Client` interface and a
  clearly-labeled `disabled` no-op default.
- A settings key (via `agent/updates.Policy`'s pattern, or a dedicated
  telemetry settings store) for the opt-in flag and `node_suite_id`.
- A `telemetry_state` SQLite table already exists in the schema
  (`agent/storage/migrations/0001_init.sql`) reserved for this, unused
  until a client is implemented.
