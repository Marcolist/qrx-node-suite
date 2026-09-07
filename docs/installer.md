# One-line installer

```sh
curl -sSL https://raw.githubusercontent.com/Marcolist/qrx-node-suite/main/install.sh | sudo bash
```

This installs, configures, and starts QRX Node Suite as a systemd service, from a
prebuilt release -- no Go toolchain, no Node.js/npm, no compilation, no manual
`systemd` unit editing. `install.sh` (repository root) is the supported entry point;
`installer/linux/install.sh` remains as a lower-level helper for a developer who
already has a compiled `agentd` binary (see `docs/deployment.md`).

Equivalent two-step form (identical behavior, useful if you want to read the script
before running it -- which you should, for anything piped into `sudo bash`):

```sh
curl -sSL https://raw.githubusercontent.com/Marcolist/qrx-node-suite/main/install.sh -o install.sh
sudo bash install.sh
```

## What it does

1. **Detects your system**: distribution, version, CPU architecture, Raspberry Pi
   model (if any), and confirms systemd is present -- from `/etc/os-release` and
   `uname -m`, not guesswork.
2. **Checks prerequisites**: root, a supported architecture, network reachability,
   free disk space, systemd. Installs a handful of small runtime packages if missing
   (`ca-certificates`, `curl`, `tar`, `gzip`, `jq`, `openssl`, and
   `libsqlite3-0` -- `agentd` is cgo-linked against it, see
   `docs/deployment.md#sqlite-runtime-dependency`) via `apt-get` -- never a
   compiler toolchain.
3. **Finds the latest release** via the GitHub Releases API (`stable` channel by
   default; see [Environment variables](#environment-variables) for `beta`).
4. **Downloads and verifies** the release tarball: SHA256 checksum against a
   published `SHA256SUMS`, and -- when the release publishes one --
   `SHA256SUMS.sig`, an Ed25519 signature over `SHA256SUMS` checked against a
   trusted public key baked into `install.sh` itself (never fetched from the same
   place being verified). See [Release security](#release-security).
5. **Installs**: creates a dedicated unprivileged `qrx-agent` system user (no
   login shell, no password), installs the binary and dashboard assets to
   `/opt/qrx-node-suite`, and the `qrx-node-suite` convenience command to
   `/usr/local/bin`.
6. **Detects QRX Core**: checks `PATH` and common install locations for an existing
   `qrx-cli`. If found, it's reused as-is -- nothing is overwritten. If not found,
   see [QRX Core](#qrx-core) below.
7. **Configures and starts**: generates `/etc/qrx-node-suite/agent.json` with a
   safe default configuration and a freshly generated random admin token (see
   [Admin token](#admin-token)), installs and enables the `qrx-agent.service`
   systemd unit, and starts it.
8. **Health-checks**: polls `GET /health` for up to 30 seconds and only prints
   success once the Agent actually responds -- never declares success just because
   `systemctl start` didn't error.

## Directory layout

| Path | Contents |
|---|---|
| `/opt/qrx-node-suite/bin/agentd` | The Agent binary |
| `/opt/qrx-node-suite/dashboard/` | Built dashboard static assets |
| `/opt/qrx-node-suite/bin/uninstall.sh` | Uninstaller (see below) |
| `/etc/qrx-node-suite/agent.json` | Configuration (root + `qrx-agent` readable only) |
| `/var/lib/qrx-node-suite/` | SQLite database, OTA component store |
| `/var/log/qrx-node-suite/install.log` | Installer's own log |
| `/etc/systemd/system/qrx-agent.service` | systemd unit |
| `/usr/local/bin/qrx-node-suite` | `status` / `logs` / `uninstall` convenience wrapper |

This mirrors `installer/linux/qrx-agent.service`'s existing layout, not a new one.

## QRX Core

`install.sh` **never invents a QRX Core download URL**. There is currently no
official, checksummed QRX Core binary release this project can point to, so by
default it only *detects* an existing installation (checking `PATH` and
`/usr/local/bin`, `/opt/qrx/current/bin`, `/opt/qrx/bin`, `/usr/bin`) -- it never
overwrites one it finds. If nothing is found:

> QRX Node Suite installed successfully.
> QRX Core was not installed because no verified, official QRX Core binary source
> is currently configured. You can install or connect QRX Core later from the
> Dashboard.

The Agent's own adapter auto-selection (`agent/adapters.Registry.SelectAutomatic`)
then correctly reports "unsupported" / waiting rather than silently pretending to
be connected with the Mock adapter -- the config `install.sh` writes leaves
`adapter.name` empty specifically so this stays true automatically as QRX Core
compatibility evolves, rather than the installer hardcoding a guess.

`installer/qrx-core-sources.sh` is the extension point a maintainer wires up once
a real, official, checksummed QRX Core release exists (see the comments in that
file). Until then it defines nothing, and `install.sh` treats that the same as
"unavailable."

## Admin token

`install.sh` generates a random 40-character admin token and writes it into
`admin_token` in `agent.json` (mode 0640, owned by `qrx-agent`) -- this is the same
static bearer token `agent/api`'s existing admin-auth middleware has always used
(`docs/security.md`), not a new mechanism. It is printed once, at the end of
installation, and never written to the installer's own log file. Treat it like any
other credential: if you lose it, generate a new one and update `agent.json`
yourself (there is currently no in-Dashboard token rotation flow).

## Dashboard access

By default the Agent listens on `127.0.0.1:8787` -- reachable only from the machine
itself. Set `QRX_DASHBOARD_BIND=lan` to bind `0.0.0.0:8787` instead (LAN-reachable)
during install, or edit `listen_addr` in `agent.json` and `systemctl restart
qrx-agent` afterward. `install.sh` never opens this to the wider internet and never
touches your firewall -- see [Firewall](#firewall).

## Release security

Every release tarball is checked against a published `SHA256SUMS`. When the release
also publishes `SHA256SUMS.sig`, `install.sh` verifies that signature (Ed25519, raw
message) against a public key compiled into the script itself, using nothing but
`openssl` + coreutils (no extra dependency, no network fetch of the key). This
mirrors the trust model `agent/updates/manifest` already uses for OTA updates
(`docs/security.md`): the signing key is never the same credential as GitHub
publishing access, so a compromised release/publishing account alone cannot make
`install.sh` accept a tampered release.

`.github/workflows/release.yml` signs `SHA256SUMS` with
`agent/cmd/sign-checksums`, using a private key from the `RELEASE_SIGNING_PRIVATE_KEY`
repository secret (never committed). If that secret isn't set, the workflow still
publishes the release but only with `SHA256SUMS` (unsigned); `install.sh` falls
back to checksum-only verification in that case and says so.

## Upgrades

Running `install.sh` again when QRX Node Suite is already installed does not
re-install or touch your existing configuration/database -- it detects
`qrx-agent.service` and tells you to use the Agent's own OTA system instead (the
Dashboard's Settings -> Updates page, or `POST /api/v1/updates/install`; see
`docs/updates.md`), which already handles staged, checksummed, signed, rollback-safe
upgrades in depth. The bootstrap installer deliberately doesn't duplicate that
logic.

## Uninstalling

```sh
qrx-node-suite uninstall
```

or directly:

```sh
sudo /opt/qrx-node-suite/bin/uninstall.sh [--purge-data] [--remove-qrx-core] [--yes]
```

By default, only QRX Node Suite's own software is removed (binary, dashboard
assets, systemd unit, the `qrx-agent` user) -- your configuration, SQLite database,
and any QRX Core installation are left untouched. Flags:

- `--purge-data` also deletes `/etc/qrx-node-suite` and `/var/lib/qrx-node-suite`
  (prompts for confirmation unless `--yes` is also given).
- `--remove-qrx-core` removes QRX Core, but **only** if `install.sh` itself
  installed it (tracked by its own marker file) -- a QRX Core you installed
  yourself, or that predates QRX Node Suite, is never touched by this flag.
- QRX Core's blockchain data and wallet files are **never** removed by this script,
  under any flag, ever.

## Environment variables

The default command needs none of these -- they're for advanced/scripted use.

| Variable | Default | Meaning |
|---|---|---|
| `QRX_CHANNEL` | `stable` | `stable` or `beta` |
| `QRX_VERSION` | `latest` | `latest`, or an exact tag like `v0.1.0` |
| `QRX_INSTALL_QRX_CORE` | `auto` | `auto` or `skip` |
| `QRX_DASHBOARD_BIND` | `local` | `local` (127.0.0.1) or `lan` (0.0.0.0) |
| `QRX_VERBOSE` | `0` | `1` for verbose step output |
| `QRX_PREFIX` | `/opt/qrx-node-suite` | Install prefix |
| `QRX_CONFIG_DIR` | `/etc/qrx-node-suite` | Config directory |
| `QRX_DATA_DIR` | `/var/lib/qrx-node-suite` | Data directory |
| `QRX_LOG_DIR` | `/var/log/qrx-node-suite` | Log directory |
| `QRX_SERVICE_USER` | `qrx-agent` | Service account name |
| `QRX_LISTEN_PORT` | `8787` | Agent listen port |
| `QRX_REPO_OWNER` / `QRX_REPO_NAME` | `Marcolist` / `qrx-node-suite` | Where to fetch releases from -- only for forks/mirrors |
| `QRX_LOCAL_TARBALL` | (unset) | Path to a local release tarball; skips GitHub entirely. For offline/air-gapped installs and CI testing (`installer/test/`) -- see the script's own comments. Still requires (or computes, with a warning) a checksum; never skips SHA256 verification. |

Example: `curl -sSL .../install.sh | QRX_DASHBOARD_BIND=lan sudo -E bash`.

## Supported platforms

Ubuntu 22.04/24.04, Debian 12, and Raspberry Pi OS 64-bit (Pi 4/5), on x86_64 and
ARM64. Architecture is detected from `uname -m` (`x86_64`/`amd64` -> `linux_amd64`,
`aarch64`/`arm64` -> `linux_arm64`); anything else stops with a clear message
rather than attempting an incompatible install.

## Firewall

`install.sh` never modifies firewall rules. If QRX Core needs a P2P port open,
that's between you and QRX Core's own documentation -- out of this project's
boundary (`docs/architecture.md`). The Dashboard/admin port is bound to `127.0.0.1`
by default specifically so it is never publicly exposed by accident; see
[Dashboard access](#dashboard-access) to change that deliberately.

## Testing the installer

`installer/test/` unit-tests the pure bash functions (architecture/OS detection,
asset selection, checksum/signature failure handling) by sourcing `install.sh` --
sourcing it never runs `main` or touches the filesystem (see the guard at the
bottom of the script), so this is safe to run outside a container. CI additionally
runs a Docker-based smoke test (Ubuntu 22.04/24.04, Debian 12) that builds a real
tarball from the checked-out repo and installs it end-to-end via
`QRX_LOCAL_TARBALL`, verifying an actual healthy `qrx-agent.service`. See
`.github/workflows/installer-ci.yml`.
