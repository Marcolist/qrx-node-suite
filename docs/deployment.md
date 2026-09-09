# Deployment

## Linux: the one-line installer (recommended)

```sh
curl -sSL https://raw.githubusercontent.com/Marcolist/qrx-node-suite/main/install.sh | sudo bash
```

Installs a prebuilt, signed release as a systemd service -- no Go/Node.js/compiler
needed on the target machine. This is the supported path for actually *running* QRX
Node Suite; see [`docs/installer.md`](installer.md) for the full behavior, security
model, and every environment variable. The rest of this document covers building
from source instead -- for development, an unsupported architecture, or a platform
the installer doesn't cover yet (macOS/Windows).

## Linux (including Raspberry Pi 4/5, arm64): building from source

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

## Building a release

`.github/workflows/release.yml` is the actual release pipeline, triggered by
pushing a tag like `v0.1.0`: it builds `agentd` natively for linux/amd64 and
linux/arm64 (see that workflow's comments on why *natively*, on the oldest
supported glibc, rather than cross-compiling -- `agent/storage/sqlite` is cgo),
builds the dashboard (`npm ci && npm run build`), and packages each
architecture as `qrx-node-suite_<version>_linux_<arch>.tar.gz` (`agentd` +
`dashboard/` + the pinned QRX Core binaries and provenance metadata under `core/` +
`uninstall.sh` + `LICENSE` + `VERSION`, no wrapping directory --
this is what `install.sh` downloads). It generates `SHA256SUMS`, signs it with
`agent/cmd/sign-checksums` if the `RELEASE_SIGNING_PRIVATE_KEY` repo secret is
set (see [Signing keys](#signing-keys) below), and publishes everything as a
GitHub Release.

`cmd/agentd` still serves the dashboard from disk (`dashboard_dir` config,
default `./dashboard/dist`) rather than `go:embed`-ing it -- the release
tarball ships `dashboard/` alongside the binary rather than baking it in.
Switching to `//go:embed dist` later (for a single self-contained binary) is a
compatible follow-up, not required for the current release process to work.

To build a release tarball by hand (matching what CI does):

```sh
git clone --branch 0.0.7 https://github.com/phoenixkonsole/qrx.git qrx-core-source
git -C qrx-core-source checkout 4a732c1a7d2b03fb299eabde437646c99c797e2d
installer/build-qrx-core-linux.sh qrx-core-source core-dist
cd agent && go build -ldflags "-X main.suiteVersion=0.2.0 -X main.agentVersion=0.2.0 -X main.dashboardVersion=0.2.0" -o ../agentd ./cmd/agentd
cd dashboard && npm ci && npm run build && cd ..
mkdir -p pkg/dashboard pkg/core
cp agentd pkg/agentd && cp -a dashboard/dist/. pkg/dashboard/
cp -a core-dist/. pkg/core/
cp LICENSE installer/linux/uninstall.sh installer/qrx-core-sources.sh pkg/
echo 0.2.0 > pkg/VERSION
tar -czf qrx-node-suite_0.2.0_linux_amd64.tar.gz -C pkg .
```

## SQLite runtime dependency

Every deployment target needs `libsqlite3` available at **runtime**, not
just build time (already true on essentially every Linux distro and macOS;
Windows needs the DLL shipped alongside the binary). See
`agent/storage/sqlite/README.md`.

## Signing keys

There are two independent Ed25519 keys/trust boundaries in this project --
generate each once with `agent/cmd/gen-signing-key`, and never let a private
key touch anything except the machine/secret store that signs with it:

1. **OTA update manifest signing** (`agent/updates/manifest`,
   `docs/updates.md#update-manifest`): the public key is compiled into
   `install.sh` (`QRX_TRUSTED_MANIFEST_PUBLIC_KEY_B64`), which writes it into
   every deployed Agent's `updates.public_key_base64` config at install time;
   the private key signs update manifests via `agent/cmd/sign-manifest`, run
   by `.github/workflows/release.yml`'s "Sign OTA update manifests" step
   using the `MANIFEST_SIGNING_PRIVATE_KEY` repository secret. See
   `docs/development.md#signing-a-manifest-for-local-testing` and
   `docs/security.md`.
2. **Release/bootstrap-installer signing** (`docs/installer.md#release-security`):
   the public key is compiled directly into `install.sh`
   (`QRX_TRUSTED_PUBLIC_KEY_B64`) as its trust anchor; the private key signs
   each release's `SHA256SUMS` via `agent/cmd/sign-checksums`, run by
   `.github/workflows/release.yml` using the `RELEASE_SIGNING_PRIVATE_KEY`
   repository secret. Rotating this key means updating both the secret and
   `install.sh`'s embedded public key together -- old installer copies would
   otherwise reject new releases signed with a rotated key.

These are deliberately different keypairs (and different repository
secrets): a compromise of one signing flow -- e.g. whatever signs releases
-- must not, by itself, let an attacker push a malicious self-update to
every already-installed Agent, or vice versa. Rotating the manifest key
needs the same two-sided update as the release key above: a new
`MANIFEST_SIGNING_PRIVATE_KEY` secret and a matching new
`QRX_TRUSTED_MANIFEST_PUBLIC_KEY_B64` in `install.sh`, and every
already-installed Agent needs its `updates.public_key_base64` updated too
(there is no remote key-rotation mechanism -- see `docs/security.md`'s
threat model for why that's out of scope for now).
