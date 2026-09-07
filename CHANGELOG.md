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
- Guardian health/recovery state machine, Linux system monitoring, alert engine skeleton,
  Telegram notification client, versioned REST API with an SSE event stream.
- Dashboard source tree (React + TypeScript + Vite) covering Overview, Node, Validator,
  VELOCITY, Activity, System, Logs, Alerts, and Settings → Updates.
- OpenAPI spec, JSON Schemas, and example payloads under `contracts/`.
- Linux (systemd) installer; macOS (launchd) and Windows installer scaffolding.
