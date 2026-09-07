#!/usr/bin/env bash
# Installs the QRX Agent as a systemd service on Linux (including Raspberry
# Pi OS). Run as root. See docs/deployment.md.
set -euo pipefail

PREFIX="${QRX_PREFIX:-/opt/qrx-node-suite}"
CONFIG_DIR="${QRX_CONFIG_DIR:-/etc/qrx-node-suite}"
SERVICE_USER="${QRX_SERVICE_USER:-qrx-agent}"
BINARY_SRC="${1:-}"

if [[ $EUID -ne 0 ]]; then
  echo "install.sh must be run as root (it creates a system user, installs a systemd unit, and writes to $PREFIX)" >&2
  exit 1
fi
if [[ -z "$BINARY_SRC" || ! -f "$BINARY_SRC" ]]; then
  echo "usage: install.sh /path/to/agentd" >&2
  echo "  build it first: cd agent && go build -o agentd ./cmd/agentd" >&2
  exit 1
fi

echo "==> creating service user $SERVICE_USER"
if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
fi

echo "==> installing to $PREFIX"
install -d -o "$SERVICE_USER" -g "$SERVICE_USER" "$PREFIX/bin" "$PREFIX/var"
install -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0755 "$BINARY_SRC" "$PREFIX/bin/agentd"

echo "==> writing config to $CONFIG_DIR"
install -d "$CONFIG_DIR"
if [[ ! -f "$CONFIG_DIR/agent.json" ]]; then
  cat > "$CONFIG_DIR/agent.json" <<'JSON'
{
  "listen_addr": "127.0.0.1:8787",
  "data_dir": "/opt/qrx-node-suite/var",
  "adapter": { "name": "mock" }
}
JSON
  echo "    wrote a Mock-mode default config -- edit $CONFIG_DIR/agent.json for a real deployment (see docs/configuration.md)"
else
  echo "    $CONFIG_DIR/agent.json already exists, leaving it alone"
fi
chown -R "$SERVICE_USER:$SERVICE_USER" "$CONFIG_DIR"

echo "==> installing systemd unit"
install -m 0644 "$(dirname "$0")/qrx-agent.service" /etc/systemd/system/qrx-agent.service
install -m 0440 "$(dirname "$0")/qrx-agent-sudoers" "/etc/sudoers.d/qrx-agent"
visudo -cf "/etc/sudoers.d/qrx-agent"

systemctl daemon-reload
systemctl enable qrx-agent.service

echo "==> done. Start it with: systemctl start qrx-agent"
echo "    Then check:            curl -s http://127.0.0.1:8787/health"
