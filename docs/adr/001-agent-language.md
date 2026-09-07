# ADR 001: QRX Agent implementation language

## Status

Accepted

## Context

The QRX Agent is the one long-running process this project ships to every target: Linux
servers, Raspberry Pi 4/5 (ARM64), Windows, and macOS. It needs to: run as a native OS
service (systemd/launchd/Windows Service), serve an HTTP + SSE API, poll a local process
(`qrxd`) and shell out to `qrx-cli`, collect system metrics, persist history to SQLite, and
be trivial to install on a Pi with a small memory footprint. Candidates considered: Go,
Rust, Node.js/TypeScript.

## Decision

Use **Go** for the Agent (`agent/`), compiled to a single static binary per platform.

Reasons, weighed against the criteria in the spec:

- **Cross compilation**: `GOOS`/`GOARCH` cross-compiles to linux/arm64 (Pi 4/5),
  linux/amd64, windows/amd64, and darwin/{amd64,arm64} from one toolchain, no target
  sysroot needed for the pure-Go parts.
- **Single binary deployment**: matches the goal of "one Agent binary, one config, one
  SQLite DB" with the dashboard embedded via `go:embed`.
- **HTTP/SSE ecosystem**: `net/http` in the standard library is sufficient for a
  single-directional monitoring API; no framework dependency needed.
- **Concurrency model**: goroutines/channels fit the polling + event-bus + SSE fan-out
  shape described in sections 13-14 of the spec well, with less ceremony than async Rust.
- **Service integration**: straightforward to generate systemd units, launchd plists, and
  register a Windows service without a heavy dependency.
- **Developer productivity**: faster to write and review correctly than Rust for a
  monitoring/orchestration layer that is I/O-bound, not compute-bound; Rust's ownership
  rigor buys the least here since there's no unsafe memory-critical hot path.
- **Long-term maintainability**: Go's standard library stability and simple module model
  keep the dependency surface small and auditable, which matters for a project whose
  update pipeline is itself a trust boundary (see `docs/security.md`).

Rust was the strongest alternative (comparable footprint, arguably safer for anything
touching raw bytes) but was not chosen because the Agent's workload is dominated by
process orchestration, HTTP serving, and JSON shuffling rather than performance-critical
or memory-unsafe-prone code, where Go's productivity advantage outweighs Rust's safety
guarantees.

### Zero external dependencies

A stronger-than-usual constraint was applied on top of "prefer Go": **the Agent module
takes no external Go dependencies** (`agent/go.mod` has an empty `require` block). This
was a pragmatic-then-principled choice:

- The development environment this repository was bootstrapped in has no network access
  to the Go module proxy (`proxy.golang.org` returns 403 under the sandbox's egress
  policy), the same restriction npm and crates.io hit. Any external dependency added here
  could not be fetched, vetted, or built.
- It also directly serves engineering principle #24 ("prefer simple, auditable
  architecture") and the OTA threat model in `docs/security.md`: a compromised transitive
  dependency is one of the most common real-world supply-chain attacks, and an update
  pipeline that touches validator infrastructure is a bad place to carry that risk.
- SQLite access, which normally pulls in `mattn/go-sqlite3` or `modernc.org/sqlite`, is
  instead a small hand-written `database/sql/driver.Driver` implementation in
  `agent/storage/sqlite` that cgo-links against the system `libsqlite3` (present on all
  target Linux distros and installable via the platform installers). See
  `agent/storage/sqlite/README.md` for what it does and does not implement.

This is a deliberate constraint, not a permanent one. If a dependency earns its place
(vetted, small, actively maintained) it can be added — but it should be a conscious
decision recorded in a follow-up ADR, not an incidental `go get`.

## Consequences

- Contributors must not assume common ecosystem libraries (chi/gin/echo, zap/zerolog,
  testify, etc.) are available. The standard library (`net/http`, `log/slog`,
  `encoding/json`, `crypto/ed25519`, `database/sql`) covers everything currently needed.
- The bundled SQLite driver is intentionally minimal (no extensions, no custom
  collations) — see its README for the exact surface it exposes.
- Cross-compiling the sqlite driver requires cgo, which means a C toolchain and the target
  platform's libsqlite3 headers/libs for anything other than the host build. This is
  documented in `docs/deployment.md`.
- The dashboard remains a separate npm/Vite project (see ADR-002 once written) because
  frontend tooling has no equivalent zero-dependency story; its dependency surface is
  scoped to build-time only and the shipped artifact is static assets.
