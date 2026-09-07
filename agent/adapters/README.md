# QRX Adapter layer

This package (`adapters`) defines the `Adapter` interface and the
`AdapterRegistry`. Concrete adapters are subpackages that self-register:

```
agent/adapters/
  adapter.go     Adapter interface, Register(), Cap* capability keys
  registry.go    AdapterRegistry: discover/select/activate/rollback/validate/disable
  mock/          simulated QRX Core, for development and CI
  qrx007/        wraps qrx-cli against the QRX 0.0.7 command surface
  legacy006/     wraps qrx-cli against the QRX 0.0.6 command surface (PARTIAL support)
  future/        a minimal example showing how to add a new adapter
```

## The boundary rule

`agent/qrx` (the process/RPC transport) is imported **only** by packages under
`agent/adapters/`. Nothing else in this module — not `agent/api`, not
`agent/guardian`, not `agent/monitoring` — may import `agent/qrx` or shell out
to `qrx-cli`/`qrxd` directly. If a new subsystem needs QRX data, it goes
through an `Adapter`, normalized into `agent/models` types. This is what makes
the adapter replaceable (docs/architecture.md, principle 5) and what lets
`mock` stand in for a real node with zero code changes elsewhere.

## Adding an adapter

1. Create `agent/adapters/<name>/`.
2. Implement the `adapters.Adapter` interface, normalizing every field into
   `agent/models` types. Anything QRX Core doesn't return must become
   `models.Unavailable`/`models.Unsupported` — never a zero value or an
   invented default.
3. Register it in an `init()`:
   ```go
   func init() {
       adapters.Register("myname", func() adapters.Adapter { return &MyAdapter{} })
   }
   ```
4. Blank-import the package from `agent/cmd/agentd/main.go`:
   `_ "qrx-node-suite/agent/adapters/myname"`.
5. Add a row to `agent/data/compatibility-matrix.json` for every QRX Core
   version/adapter-version combination you've actually validated. An adapter
   with no matrix row is `UNKNOWN` and will never be auto-selected (see
   `docs/qrx-compatibility.md`).

There is no dynamic plugin loading (`.so`/`.dll`) — "installed adapters" means
"adapter packages the running binary was built with." This keeps the Agent a
single, auditable static binary (ADR-001) at the cost of needing a rebuild to
add an adapter. OTA adapter updates (`docs/updates.md#adapter-ota-updates`)
therefore ship a full updated Agent binary containing the new adapter code,
staged/verified/rolled-back like any other component.
