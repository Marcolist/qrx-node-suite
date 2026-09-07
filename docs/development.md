# Development

## Requirements

- Go 1.24+, a C toolchain, and `libsqlite3-dev` (or equivalent) --
  `agent/storage/sqlite` cgo-links against the system SQLite (ADR-001).
- Node.js 20+ and npm registry access, only for the dashboard.

## Running the Agent

```sh
cd agent
go build -o bin/agentd ./cmd/agentd
QRX_AGENT_CONFIG="" ./bin/agentd   # Mock mode, zero config needed
```

With no config file, `config.Default()` starts the Agent in Mock mode:
adapter `mock` (a simulated QRX node, clearly marked `simulation_mode: true`
in `GET /api/v1/node`), a `development` update source (no real network
calls), no admin token (administrative endpoints return 403 — disabled, not
open), listening on `127.0.0.1:8787`.

Verify it's alive:

```sh
curl -s localhost:8787/health
curl -s localhost:8787/api/v1/status | head -c 200
curl -sN localhost:8787/api/v1/events   # live SSE stream; watch node.height_changed etc.
```

A config file (`-config path/to/config.json` or `QRX_AGENT_CONFIG=...`)
overrides any subset of `config.Config`'s fields (see `agent/config/config.go`)
-- e.g. to point at a real `qrx-cli`:

```json
{
  "adapter": { "name": "qrx007", "cli_path": "/usr/local/bin/qrx-cli", "network": "mainnet" },
  "admin_token": "a-real-secret-for-local-testing-only"
}
```

## Running the dashboard

```sh
cd dashboard
npm install
npm run dev      # dev server with API proxy to :8787
# or:
npm run build     # writes dashboard/dist -- the Agent serves this at "/"
                   # once you point its dashboard_dir config (or the
                   # default ./dashboard/dist) at it
```

## Tests

```sh
cd agent
go build ./... && go vet ./... && go test ./...
```

Every package with meaningful logic has tests; run `go test ./... -v` for
per-test output. `agent/storage/sqlite`'s tests exercise the cgo driver
directly (multi-statement exec, transactions, foreign keys). The `updates`
package's tests are the most involved -- they build a full in-memory
`Manager`/`QRXCoreUpdateManager` against `storage.Open(":memory:")` and a
`sources.DevelopmentSource`, and cover the safe-update-process end to end
including the self-binary two-phase (stage+promote now, health-check-and-commit-or-rollback
on next "restart," simulated by calling `ResumeSelfUpdate` directly).

## Adding an adapter

See `agent/adapters/README.md`.

## Adding a migration

Add a new numbered file under `agent/storage/migrations/` (e.g.
`0002_whatever.sql`) -- never edit `0001_init.sql` once it's shipped. No
`BEGIN`/`COMMIT` in the file itself; `agent/storage.Migrate` wraps each file
in its own transaction. See `agent/storage/sqlite/README.md`'s
"multi-statement footgun" note for why this matters.

## Signing a manifest for local testing

```sh
cd agent
go run ./cmd/gen-signing-key                                  # writes signing-key.priv / .pub
go run ./cmd/sign-manifest -key signing-key.priv -manifest manifest.json
```

Both are small, dependency-free wrappers around
`agent/updates/manifest.SignManifest`/`SignComponentChecksum`. Put the
public key's base64 contents into `updates.public_key_base64` in the
Agent's config; keep the private key off any running Agent entirely.

## Project conventions

- No emojis, no unexplained abbreviations in log messages.
- Every field in `agent/models` that QRX Core might not return is
  `models.Value[T]`, never a bare zero value -- see
  `agent/models/availability.go`.
- Structured logging only (`log/slog`, via `agent/logging`).
- No new external Go module dependency without discussing it first — see
  ADR-001 and CONTRIBUTING.md.
