# agent/storage/sqlite

A minimal `database/sql/driver.Driver` implementation over the system
`libsqlite3`, via cgo. Registers itself as `"sqlite3"`
(`sql.Open("sqlite3", path)`). See ADR-001 (`docs/adr/001-agent-language.md`)
for why this exists instead of an imported driver.

## Requirements

- `CGO_ENABLED=1` (default when a C toolchain is present).
- `libsqlite3` development headers/library at build time (`libsqlite3-dev`
  on Debian/Ubuntu, `sqlite-devel` on Fedora/RHEL, `sqlite3` via Homebrew on
  macOS which includes headers, or vendor your own and set `CGO_CFLAGS`/
  `CGO_LDFLAGS`). Resolved via `pkg-config sqlite3` (`#cgo pkg-config:
  sqlite3` in `driver.go`).
- The **runtime** target needs `libsqlite3.so`/`.dylib`/`.dll` available
  (already true on essentially every Linux distro and macOS; Windows builds
  need to ship the DLL alongside the Agent binary -- see
  `docs/deployment.md`).
- Cross-compiling to a different OS/arch than the build host requires a
  matching cross C toolchain and that target's libsqlite3, same as any other
  cgo dependency.

## What it implements

- `driver.Conn`: `Prepare`, `Close`, `Begin`/`Commit`/`Rollback`.
- `driver.Stmt`: `Exec`, `Query`, `NumInput`, `Close`.
- `driver.Rows`: `Columns`, `Next`, `Close`.
- `driver.Execer` (legacy, still honored by `database/sql`) on `conn`, used
  **only** for no-argument `Exec` calls, to correctly run multi-statement
  scripts (every migration file). See the "multi-statement" note below.
- Parameter binding for `nil`, `int64` (all Go integer types are converted
  to this by `database/sql`'s default converter), `float64`, `bool`,
  `[]byte`, `string`, `time.Time` (stored as RFC3339Nano text).
- `PRAGMA foreign_keys = ON`, `PRAGMA journal_mode = WAL`, and a 5s
  `busy_timeout`, set on every new connection.

## What it deliberately does not implement

- Custom SQL functions/collations/virtual tables.
- `driver.Connector` / `driver.Validator` / context-aware Exec/Query variants
  (`database/sql` falls back to the non-context versions and checks
  `ctx.Done()` around the call, which is sufficient for this project's
  usage).
- Extension loading.
- Any encryption (SQLCipher, etc).

If a need for any of the above shows up, prefer evaluating whether it's
worth revisiting ADR-001's zero-dependency constraint before extending this
driver significantly further.

## The multi-statement footgun (and how this driver avoids it)

`sqlite3_prepare_v2` compiles **only the first statement** in a SQL string;
the remainder is silently discarded (available via its `pzTail` out
parameter, which this driver does not use). A naive driver that always
routes `db.Exec(sql)` through `Prepare` + `Stmt.Exec` would silently run only
the first `CREATE TABLE` of a migration file and drop the rest -- a serious,
quiet correctness bug for exactly the kind of scripts `agent/storage/migrations`
contains.

This driver's `conn.Exec` implements `database/sql`'s legacy `driver.Execer`
interface: when called with **no arguments**, it routes the query through
`sqlite3_exec`, which does run every statement in the string. When called
**with** arguments, it returns `driver.ErrSkip`, so `database/sql` falls back
to the normal single-statement `Prepare`+`Stmt.Exec` path (parameter binding
only ever makes sense against one statement).

Practical rule for callers: multi-statement scripts (migrations) must be run
via `db.Exec(sqlScript)` with **no parameters**. A single parameterized
statement is unaffected. See `driver_test.go`'s
`TestMultiStatementExecRunsEveryStatement` for the regression test.

## Concurrency

`database/sql` only calls into a checked-out `driver.Conn` from one goroutine
at a time by contract, so this driver adds no additional per-connection
locking. Connections are opened with `SQLITE_OPEN_FULLMUTEX` regardless, as a
defense-in-depth measure.
