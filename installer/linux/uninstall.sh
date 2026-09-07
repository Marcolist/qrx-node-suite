#!/usr/bin/env bash
# QRX Node Suite uninstaller. Installed alongside the Agent at
# /opt/qrx-node-suite/bin/uninstall.sh by install.sh; also reachable via
# `qrx-node-suite uninstall` (a thin wrapper installed to
# /usr/local/bin/qrx-node-suite -- see docs/installer.md#uninstalling).
#
# Uninstall differentiates four things, each removed only when explicitly
# asked for:
#   1. QRX Node Suite software (binary, dashboard assets, systemd unit) --
#      removed by default.
#   2. QRX Node Suite's own data (SQLite DB, config, admin token) -- kept
#      by default, removed only with --purge-data.
#   3. QRX Core itself -- kept by default, removed only with
#      --remove-qrx-core, and ONLY if this installer's own marker shows
#      install.sh installed it (never an operator's pre-existing QRX Core).
#   4. QRX Core's blockchain data / wallet -- never removed by this script
#      at all, under any flag. That is exclusively QRX Core's own tooling's
#      responsibility.
set -Eeuo pipefail

QRX_PREFIX="${QRX_PREFIX:-/opt/qrx-node-suite}"
QRX_CONFIG_DIR="${QRX_CONFIG_DIR:-/etc/qrx-node-suite}"
QRX_DATA_DIR="${QRX_DATA_DIR:-/var/lib/qrx-node-suite}"
QRX_LOG_DIR="${QRX_LOG_DIR:-/var/log/qrx-node-suite}"
QRX_SERVICE_USER="${QRX_SERVICE_USER:-qrx-agent}"

PURGE_DATA=0
REMOVE_QRX_CORE=0
ASSUME_YES=0

usage() {
  cat <<'USAGE'
usage: uninstall.sh [--purge-data] [--remove-qrx-core] [--yes] [--help]

  --purge-data       Also delete QRX Node Suite's own data: the SQLite
                      database, configuration, and admin token under
                      /etc/qrx-node-suite and /var/lib/qrx-node-suite.
                      Never removes QRX Core or its blockchain data.

  --remove-qrx-core   Also remove QRX Core, but ONLY if this installer's
                      own record shows install.sh installed it. A QRX Core
                      you installed yourself, or that predates QRX Node
                      Suite, is never touched by this flag.

  --yes               Don't prompt for confirmation (required for any
                      destructive flag above when not running interactively).

By default (no flags), only QRX Node Suite's own software is removed --
your database, configuration, and QRX Core are left exactly as they are.
USAGE
}

for arg in "$@"; do
  case "$arg" in
    --purge-data) PURGE_DATA=1 ;;
    --remove-qrx-core) REMOVE_QRX_CORE=1 ;;
    --yes|-y) ASSUME_YES=1 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "unknown option: $arg" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "uninstall.sh must be run as root (it stops a systemd service and removes system files)." >&2
  exit 1
fi

confirm() {
  local prompt="$1"
  if [[ "$ASSUME_YES" == "1" ]]; then
    return 0
  fi
  if [[ ! -t 0 ]]; then
    echo "Refusing to ${prompt} without --yes (not running interactively)." >&2
    return 1
  fi
  read -r -p "${prompt} [y/N] " reply
  [[ "$reply" =~ ^[Yy]$ ]]
}

echo "QRX Node Suite Uninstaller"
echo ""

# --- 1. Software: always removed ---
if systemctl list-unit-files 2>/dev/null | grep -q '^qrx-agent\.service'; then
  systemctl stop qrx-agent.service 2>/dev/null || true
  systemctl disable qrx-agent.service 2>/dev/null || true
  rm -f /etc/systemd/system/qrx-agent.service
  systemctl daemon-reload 2>/dev/null || true
  echo "  - removed qrx-agent.service"
fi

if [[ -d "$QRX_PREFIX" ]]; then
  rm -rf "${QRX_PREFIX:?}/bin" "${QRX_PREFIX:?}/dashboard"
  # Leave QRX_PREFIX itself if anything unexpected remains in it (never
  # rm -rf a whole prefix directory blindly).
  rmdir "$QRX_PREFIX" 2>/dev/null || true
  echo "  - removed ${QRX_PREFIX}/bin and ${QRX_PREFIX}/dashboard"
fi

rm -f /usr/local/bin/qrx-node-suite
echo "  - removed the qrx-node-suite command"

if id -u "$QRX_SERVICE_USER" >/dev/null 2>&1; then
  userdel "$QRX_SERVICE_USER" 2>/dev/null || true
  echo "  - removed the ${QRX_SERVICE_USER} system user"
fi

echo ""
echo "QRX Node Suite software removed."

# --- 2. Node Suite's own data: opt-in ---
if [[ "$PURGE_DATA" == "1" ]]; then
  if confirm "Permanently delete QRX Node Suite's database, configuration, and admin token (${QRX_CONFIG_DIR}, ${QRX_DATA_DIR}, ${QRX_LOG_DIR})?"; then
    rm -rf "$QRX_CONFIG_DIR" "$QRX_DATA_DIR" "$QRX_LOG_DIR"
    echo "  - deleted ${QRX_CONFIG_DIR}, ${QRX_DATA_DIR}, ${QRX_LOG_DIR}"
  else
    echo "  - kept ${QRX_CONFIG_DIR} and ${QRX_DATA_DIR} (confirmation declined)"
  fi
else
  echo ""
  echo "Your configuration and database were kept:"
  echo "  ${QRX_CONFIG_DIR}"
  echo "  ${QRX_DATA_DIR}"
  echo "Re-run with --purge-data to remove them, or reinstall to reuse them as-is."
fi

# --- 3. QRX Core: opt-in, and only if THIS installer put it there ---
qrx_core_marker="${QRX_DATA_DIR}/qrx-core-installed-by-this-installer"
if [[ "$REMOVE_QRX_CORE" == "1" ]]; then
  if [[ -f "$qrx_core_marker" ]]; then
    if confirm "Remove the QRX Core installation that install.sh set up? (blockchain data and wallet are NEVER removed by this script, regardless)"; then
      qrx_core_dir="$(cat "$qrx_core_marker" 2>/dev/null || true)"
      if [[ -n "$qrx_core_dir" && -d "$qrx_core_dir" ]]; then
        rm -rf "$qrx_core_dir"
        echo "  - removed ${qrx_core_dir}"
      fi
    else
      echo "  - kept QRX Core (confirmation declined)"
    fi
  else
    echo "  - --remove-qrx-core given, but no record shows this installer installed QRX Core (nothing to remove here; a pre-existing QRX Core is never touched by this script)"
  fi
fi

echo ""
echo "QRX Core's blockchain data and wallet files, if any, were never touched by this script."
echo "Done."
