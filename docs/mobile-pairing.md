# Mobile pairing API (future, read-only by default)

**Status: not implemented in this codebase yet.** This document records the
contract any future implementation must honor, per
`docs/architecture.md` principle 7: "Mobile access must default to
read-only."

## Hard requirements for whenever this is built

1. **Disabled by default**, same as remote control generally (principle 7).
   An operator must explicitly enable pairing before any pairing endpoint
   accepts a request.
2. **Read-only by default** even once enabled. Any write/administrative
   capability for a paired device is a separate, explicit additional grant
   -- never implied by pairing itself.
3. **Per-device revocable.** The `paired_devices` table already exists in
   the schema (`agent/storage/migrations/0001_init.sql`: `id`, `name`,
   `paired_at`, `last_seen_at`, `read_only`, `revoked`), unused until a
   pairing flow is implemented. `revoked` must be checked on every request
   from that device, not just at pairing time.
4. **No QRX secrets ever cross this boundary** -- same constraint as
   everything else in this project (docs/architecture.md principles 6-12).
5. **Central server failure must never affect QRX Core or local
   monitoring** (principle 8), same as telemetry.

## What would need to exist

- An `agent/pairing` package: pairing handshake (e.g. QR-code-based, out of
  band from any cloud service -- consistent with principle 7's "Local
  operation must not depend on any cloud service"), token issuance/rotation,
  and per-device read-only vs. read-write scoping.
- New `/api/v1/pairing/...` endpoints, admin-gated the same way
  `docs/updates.md#admin-authorization` gates OTA/version-switching actions.
- A UI in the dashboard's Settings for listing/revoking paired devices.
