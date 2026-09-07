# macOS installer (scaffolding)

**Status: not wired to `agent/platform` yet.** `agent/platform.New()` on
`darwin` returns `Unsupported` (see `agent/platform/unsupported.go`) --
Guardian's automatic restart and `POST /api/v1/services/qrx/restart` will
fail loudly rather than silently no-op, until a launchd-backed
`ServiceManager` is implemented (analogous to `agent/platform/systemd_linux.go`,
using `launchctl load`/`unload`/`kickstart`).

The Agent itself builds and runs fine on macOS
(`GOOS=darwin GOARCH=arm64 go build ./cmd/agentd`); only qrxd service
management via the Agent is affected.

## Manual install (until a real installer script exists)

```sh
sudo mkdir -p /usr/local/opt/qrx-node-suite/bin /usr/local/etc/qrx-node-suite /usr/local/var/log/qrx-node-suite
sudo cp agentd /usr/local/opt/qrx-node-suite/bin/agentd
sudo cp com.qrxnodesuite.agentd.plist /Library/LaunchDaemons/
sudo launchctl load /Library/LaunchDaemons/com.qrxnodesuite.agentd.plist
```

Write `/usr/local/etc/qrx-node-suite/agent.json` per `docs/configuration.md`
before loading the daemon, or it'll start in Mock mode with `config.Default()`.
