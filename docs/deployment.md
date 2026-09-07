# Deployment

## Linux (including Raspberry Pi 4/5, arm64)

```sh
cd agent
GOOS=linux GOARCH=arm64 go build -o agentd-linux-arm64 ./cmd/agentd   # Pi 4/5
GOOS=linux GOARCH=amd64 go build -o agentd-linux-amd64 ./cmd/agentd   # server/VPS
```

Cross-compiling `agent/storage/sqlite` (cgo) to a different architecture
than the build host needs a matching cross C toolchain and that target's
`libsqlite3` headers/libs -- see `agent/storage/sqlite/README.md`. Building
natively on the target (or in a matching container) sidesteps this
entirely, which is the simpler path for a Pi.

Install as a systemd service: see `installer/linux/` for the unit file and
install script. `agent/platform`'s `Systemd` type manages the `qrxd`
service (not the Agent's own) at runtime for Guardian's automatic recovery
and the `POST /api/v1/services/qrx/restart` endpoint.

## Raspberry Pi notes

See `docs/raspberry-pi.md`.

## macOS / Windows

Service management (`agent/platform`) has no macOS (launchd) or Windows
(Service Control Manager) implementation yet -- `platform.New()` on those
platforms returns `Unsupported`, which fails loudly (not silently) on
every call. The Agent itself builds and runs fine
(`GOOS=darwin`/`GOOS=windows go build`); only Guardian's automatic restart
and the service-restart API endpoint are affected until those land. See
`installer/macos/` and `installer/windows/` for what's scaffolded so far.

On Windows specifically, `agent/updates/store`'s atomic pointers fall back
from a real symlink to a plain text file when the process lacks
`SeCreateSymbolicLinkPrivilege` (not running elevated / Developer Mode not
enabled) -- see `agent/updates/store/atomic_windows.go`. Both forms are
read transparently either way.

## Building a release (with the dashboard embedded)

This repository's `cmd/agentd` currently serves the dashboard from disk
(`dashboard_dir` config, default `./dashboard/dist`) rather than
`go:embed`-ing it, because the dashboard needs `npm run build` before
anything exists to embed, and this repository's own build environment had
no npm registry access to verify that step. A release build process should:

1. `cd dashboard && npm ci && npm run build`
2. Replace `cmd/agentd/dashboard.go`'s disk-serving handler with a
   `//go:embed dist` of the dashboard's build output (or keep disk-serving
   for a "thin" release that still needs `dashboard/dist/` shipped
   alongside the binary -- both are valid; embedding gets you the "one
   binary, one config, one SQLite DB" deployment story described in
   `docs/architecture.md`).
3. `cd agent && go build -o agentd ./cmd/agentd`

## SQLite runtime dependency

Every deployment target needs `libsqlite3` available at **runtime**, not
just build time (already true on essentially every Linux distro and macOS;
Windows needs the DLL shipped alongside the binary). See
`agent/storage/sqlite/README.md`.

## Signing keys

Generate a release signing key once (`agent/cmd/gen-signing-key`), keep the
private key off every deployed Agent entirely, and put the public key's
base64 into every Agent's `updates.public_key_base64` config. See
`docs/development.md#signing-a-manifest-for-local-testing` and
`docs/security.md`.
