# Changelog

All notable changes to this project are documented in this file.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this
project uses independent [semantic versioning](docs/updates.md#release-strategy) per
component (Agent, Dashboard, each Adapter) rather than one suite-wide version number.

## [Unreleased]

### Added

- Initial repository scaffold: layered architecture (`agent/`, `dashboard/`, `installer/`,
  `contracts/`, `docs/`).
- Version management and OTA update system: structured component version model,
  explicit (non-inferred) compatibility matrix and compatibility profiles, adapter
  registry with automatic/manual selection, signed and checksummed update manifests,
  atomic staged component updates with rollback, a separate conservative QRX Core update
  manager, version pinning, update channels, update lock, scheduled update windows, and
  SQLite-backed update history / audit log.
- QRX Adapter interface with a Mock adapter (simulation mode) and a QRX 0.0.7 adapter
  wrapping `qrx-cli` via a safe command runner.
- Guardian health/recovery state machine (bounded automatic restarts), real /proc-based
  Linux system monitoring (Raspberry Pi model/temperature/throttling detection), an alert
  engine with default rules and create/resolve lifecycle management, a dependency-free
  Telegram notification client, and a versioned REST API with an SSE event stream backed
  by a centralized polling loop.
- `agent/cmd/agentd`: the Agent binary itself, wiring every subsystem together --
  config loading, adapter selection, the OTA managers, self-update resume on startup,
  and graceful shutdown. Verified to build and run end-to-end in Mock mode.
- `agent/cmd/gen-signing-key` and `agent/cmd/sign-manifest`: dependency-free tools for
  producing real signed update manifests during development and release.
- Dashboard source tree (React + TypeScript + Vite) covering Overview, Node, Validator,
  VELOCITY, Activity, System, Logs, Alerts, and Settings → Updates.
- OpenAPI spec, JSON Schemas, and a genuinely-signed-and-verified example manifest under
  `contracts/`.
- Linux (systemd unit + install script + scoped sudoers rule); macOS (launchd plist) and
  Windows (Service Control Manager) installer scaffolding.
- CI (`.github/workflows/ci.yml`): Go build/vet/gofmt/test, dashboard typecheck/build.
- Boot-attempt counter with automatic rollback (`agent/updates/bootguard.go`): closes the
  self-update crash-loop gap by forcing a rollback to the previous version after too many
  consecutive failed boots of a self-binary (agent/adapter) update, before
  `Manager.ResumeSelfUpdate` ever gets a chance to run.
- One-line installer (`install.sh`, `docs/installer.md`): `curl -sSL .../install.sh | sudo
  bash` installs QRX Node Suite as a systemd service on Ubuntu 22.04/24.04, Debian 12, and
  Raspberry Pi OS (amd64/arm64) from a prebuilt, checksummed, Ed25519-signed release --
  no Go/Node.js/compiler needed on the target. Includes OS/arch detection, safe dependency
  installation, a dedicated `qrx-agent` service user, a generated random admin token, QRX
  Core auto-detection (never inventing a download source), health-checked startup, and an
  uninstaller (`qrx-node-suite uninstall`) that never touches QRX Core data without
  explicit confirmation.
- `.github/workflows/release.yml`: builds and publishes signed, checksummed release
  tarballs (linux/amd64, linux/arm64) on tag push; `agent/cmd/sign-checksums` signs
  `SHA256SUMS` for `install.sh` to verify.
- `.github/workflows/installer-ci.yml`: shellcheck, pure-function unit tests
  (`installer/test/`), and a Docker-based end-to-end smoke test (Ubuntu 22.04/24.04,
  Debian 12) that installs a freshly built local release inside a real systemd container
  and verifies the Agent actually becomes healthy.
