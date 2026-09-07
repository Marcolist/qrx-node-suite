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
| `/opt/qrx-node-suite/bin/agentd` | Symlink into the OTA store's active agent release (see below) -- not a plain binary file |
| `/opt/qrx-node-suite/dashboard/` | Built dashboard static assets |
| `/opt/qrx-node-suite/bin/uninstall.sh` | Uninstaller (see below) |
| `/etc/qrx-node-suite/agent.json` | Configuration (root + `qrx-agent` readable only) |
| `/var/lib/qrx-node-suite/` | SQLite database, OTA component store |
| `/var/lib/qrx-node-suite/components/agent/` | The agent's own OTA store: `releases/<version>/agentd`, `current -> releases/<version>` |
| `/var/log/qrx-node-suite/install.log` | Installer's own log (root-owned; see [Installer log directory](#installer-log-directory)) |
| `/etc/systemd/system/qrx-agent.service` | systemd unit |
| `/usr/local/bin/qrx-node-suite` | `status` / `logs` / `uninstall` convenience wrapper |

This mirrors `installer/linux/qrx-agent.service`'s existing layout, not a new one.

`/opt/qrx-node-suite/bin/agentd` is set up once, at install time
(`install.sh`'s `bootstrap_agent_ota_store()`), as a symlink into
`/var/lib/qrx-node-suite/components/agent/current/agentd` rather than a
plain copy of the downloaded binary -- this is what lets a self-update
(`docs/updates.md#self-binary-components-agent-adapters`) actually take
effect: systemd's `ExecStart` always execs this same fixed path, and
`current` is exactly what an OTA promotion atomically repoints. Nothing
about running or managing the service changes because of this; it only
matters if you're inspecting the filesystem directly.

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

Every GitHub-sourced release tarball must pass two checks, in order: an Ed25519
signature (raw message) over the release's `SHA256SUMS`, verified against a public
key compiled into `install.sh` itself, using nothing but `openssl` + coreutils (no
extra dependency, no network fetch of the key); then the tarball's own SHA256
checksum against that now-trusted `SHA256SUMS`. **A release with no signature, or
one that fails verification, is refused outright** -- `install.sh` does not fall
back to checksum-only trust, because the checksum comes from the same release
location being verified: without the signature, "no attacker" and "an attacker who
stripped the signature" are indistinguishable. This mirrors the trust model
`agent/updates/manifest` already uses for OTA updates (`docs/security.md`): the
signing key is never the same credential as GitHub publishing access, so a
compromised release/publishing account alone cannot make `install.sh` accept a
tampered release.

`.github/workflows/release.yml` signs `SHA256SUMS` with `agent/cmd/sign-checksums`,
using a private key from the `RELEASE_SIGNING_PRIVATE_KEY` repository secret (never
committed), and **refuses to publish a release at all if that secret isn't set** --
an unsigned release would be one `install.sh` can never actually install, so
publishing it anyway would just be a broken release with a misleading green
checkmark. Set the secret (see `docs/deployment.md#signing-keys`) before tagging a
release.

The `QRX_LOCAL_TARBALL` testing/offline-install hook (see
[Environment variables](#environment-variables)) is a deliberately separate trust
boundary: it never touches GitHub at all, so there is no release signature to check
-- the operator supplying a local tarball is trusting it out of band. SHA256
verification still applies there.

## Installer log directory

`QRX_LOG_DIR` (`/var/log/qrx-node-suite` by default) is owned by `root:qrx-agent`,
mode `0750` -- **not** writable by the unprivileged `qrx-agent` service user.
Nothing in this codebase currently has `agentd` itself write into it (it logs to
stdout/stderr, captured by `journald`); the only thing that writes there is
`install.sh`'s own `install.log`, written while `install.sh` is running as root.
Keeping the directory agent-writable would let a compromised `qrx-agent` process
plant a symlink at `install.log`'s path pointing at any root-owned file; since
re-running `install.sh` is an expected, documented flow (see
[Upgrades](#upgrades) below) and its logging setup runs as root before it
re-secures the directory's ownership, that symlink would later be followed and
truncated by root -- an unprivileged-to-root arbitrary-file-truncation primitive.
`install.sh`'s `setup_logging()` additionally creates `install.log` via
`init_log_file()`, which never opens or truncates through the existing path at
all: it creates a fresh file under a private, `mktemp`-generated temp name in
the same directory (so its name is never predictable the way an earlier
`"$$-$RANDOM"`-based name was), **opens a file descriptor on it first**, and
only then atomically renames it over `install.log`'s path. `rename(2)`
replaces whatever is at the destination -- symlink, regular file, or nothing
-- without ever dereferencing it, so this is safe against a symlink already
there; opening the descriptor before the rename (not after) means there is no
window at all, race or otherwise, between "the safe file exists at that path"
and "this process has an fd bound to its actual inode".

That descriptor (`LOG_FD`) is what makes every *later* `log()` call safe too,
not just this first write: a file descriptor is bound to the underlying
inode, not the path, so `log()` writes through it directly rather than
reopening `install.log` by path each time. Two earlier, narrower versions of
this fix each closed one gap but left another: a "check for a symlink, remove
it, then truncate" version closed a pre-existing symlink but had a race window
between the check and the truncate; the atomic-rename version that replaced it
closed that race for the *first* write, but every later `log()` call
throughout the rest of the install still reopened `install.log` by path,
so a symlink planted at any point *after* that first write -- QRX_LOG_DIR can
stay agent-writable until `install_release()` re-secures it, much later in
`main()` -- would have every subsequent append follow it (reproduced: see
`installer/test/test-detection.sh`'s regression tests, which reproducibly
broke each earlier version before this fix and now run clean). This means
even a system that was *first* installed by an older, vulnerable `install.sh`
-- and so still has an agent-owned log directory left over from that earlier
run, with `qrx-agent` still actively running for the entire duration of a
later re-run -- is safe the moment it's re-run with a patched `install.sh`.

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

### QRX Core removal safety

`--remove-qrx-core`'s marker file lives at
`/etc/qrx-node-suite/qrx-core-installed-by-this-installer` -- `QRX_CONFIG_DIR`,
owned `root:qrx-agent` mode `0750`, so only root can write it. Its content becomes
an `rm -rf` target run as root, so this is deliberate: the marker never lives under
`/var/lib/qrx-node-suite` (`QRX_DATA_DIR`), which the unprivileged `qrx-agent`
service user can write to -- a compromised agent process must never be able to
plant or rewrite this file to point `uninstall.sh` at an arbitrary directory.
`uninstall.sh` additionally never trusts the marker's content outright: it resolves
the path (following any symlinks) and refuses to remove anything that doesn't fall
under `/opt/qrx` (`QRX_CORE_INSTALL_ROOT`), the documented QRX Core install root
(see `installer/qrx-core-sources.sh`), printing a refusal instead of silently doing
nothing. This is the fix for an external audit's F05 finding; see
`installer/test/test-uninstall.sh`'s `resolve_qrx_core_target` tests for the
regression coverage.

`--purge-data --remove-qrx-core` together run QRX Core removal **before**
purging data, specifically because the marker now lives inside `QRX_CONFIG_DIR`:
purging that directory first would delete the marker before it's ever read,
silently skipping Core removal even though it was explicitly requested (the R04
finding, an external re-review of the F05 fix -- see
`installer/test/test-uninstall.sh`'s combined-flags regression test).

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
