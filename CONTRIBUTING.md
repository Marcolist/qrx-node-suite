# Contributing

Thanks for helping build QRX Node Suite. A few ground rules before you dive in.

## Project boundary

QRX Node Suite is an operations/monitoring layer, not a QRX Core reimplementation. Before
sending a change, re-read the "Core Engineering Principles" in
[`docs/architecture.md`](docs/architecture.md). In particular:

- All QRX-specific integration must go through the adapter interface in `agent/adapters/`.
  Do not add QRX protocol logic anywhere else in the Agent.
- Never invent a field in a normalized model because it would be convenient for the UI.
  If QRX Core doesn't expose it, the model must represent it as unavailable, not a zero
  value or a guess.
- If you're changing `agent/adapters/qrx007`, verify field names against real QRX 0.0.7
  behavior (source, `qrx-cli` output, or RPC responses) and update
  [`docs/qrx-0.0.7-interface.md`](docs/qrx-0.0.7-interface.md) accordingly. Do not merge
  speculative field mappings without marking them as unverified.

## Development setup

See [`docs/development.md`](docs/development.md).

## Dependencies

The Agent is intentionally dependency-free (Go standard library only, including a small
hand-written cgo SQLite driver in `agent/storage/sqlite`) — see
[`docs/adr/001-agent-language.md`](docs/adr/001-agent-language.md) for why. Do not add a
`go.sum` dependency without discussing it first; the same goes for adding a runtime
dependency to the dashboard beyond React/Vite's own toolchain.

## Commit / PR expectations

- One logical change per PR.
- Run `go build ./... && go vet ./... && go test ./...` from `agent/` before submitting.
- Update the relevant doc under `docs/` in the same PR as the behavior it describes.
- Migrations (`agent/storage/migrations/`, `agent/config`) are append-only — never edit a
  migration that has shipped.

## Code of conduct

Be respectful, be precise, assume good faith. Disagreements about architecture should be
resolved by referring back to the boundary rules above and, where one exists, an ADR under
`docs/adr/`.
