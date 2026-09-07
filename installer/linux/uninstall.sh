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

# The marker lives under QRX_CONFIG_DIR (root:qrx-agent, mode 0750 -- root
# writes it, the qrx-agent service can only read it), never QRX_DATA_DIR
# (which IS writable by the unprivileged qrx-agent service user): its
# content becomes an `rm -rf` target run as root below, so a compromised
# qrx-agent process must never be able to plant or rewrite it. Fix for an
# external security audit's F05 finding -- see
# docs/installer.md#qrx-core-removal-safety.
QRX_CORE_MARKER="${QRX_CONFIG_DIR}/qrx-core-installed-by-this-installer"
# The QRX Core install root installer/qrx-core-sources.sh documents as the
# convention for a real qrx_core_official_source() implementation to use
# (mirrors docs/deployment.md's /opt/qrx/versions/<version>,
# /opt/qrx/current layout). Even with the marker itself now root-only-
# writable, this is a second, independent line of defense: the marker's
# content is never trusted as an `rm -rf` target unless it resolves (after
# following any symlinks) to this root or somewhere underneath it --
# never "/", "/etc", a symlink escape, or anything else.
QRX_CORE_INSTALL_ROOT="${QRX_CORE_INSTALL_ROOT:-/opt/qrx}"

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

parse_args() {
  for arg in "$@"; do
    case "$arg" in
      --purge-data) PURGE_DATA=1 ;;
      --remove-qrx-core) REMOVE_QRX_CORE=1 ;;
      --yes|-y) ASSUME_YES=1 ;;
      --help|-h) usage; exit 0 ;;
      *) echo "unknown option: $arg" >&2; usage >&2; exit 2 ;;
    esac
  done
}

require_root() {
  if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
    echo "uninstall.sh must be run as root (it stops a systemd service and removes system files)." >&2
    exit 1
  fi
}

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

# resolve_qrx_core_target reads marker_file (if it exists) and echoes the
# real, symlink-resolved directory it names, ONLY if that directory both
# exists and resolves to install_root or somewhere underneath it -- the F05
# safety check, pulled out into its own function so it can be unit-tested
# (installer/test/) independent of actually running an uninstall. Prints
# nothing and returns non-zero for every other case (no marker, marker
# names a directory that no longer exists, or a path outside install_root);
# callers distinguish those cases via the exit status alone, not by parsing
# what (if anything) was echoed.
resolve_qrx_core_target() {
  local marker_file="$1" install_root="$2"
  [[ -f "$marker_file" ]] || return 1
  local dir real
  dir="$(cat "$marker_file" 2>/dev/null || true)"
  [[ -n "$dir" && -d "$dir" ]] || return 1
  real="$(cd "$dir" 2>/dev/null && pwd -P || true)"
  [[ -n "$real" ]] || return 1
  case "$real" in
    "$install_root" | "$install_root"/*)
      echo "$real"
      return 0
      ;;
    *)
      return 2 # exists, but outside install_root -- distinct from "no marker"
      ;;
  esac
}

remove_software() {
  # --- 1. Software: always removed ---
  # A direct file check, not `systemctl list-unit-files | grep -q ...`:
  # under `set -o pipefail` (this script has it), grep -q's early exit on
  # the first match SIGPIPEs the still-writing systemctl process, and
  # pipefail then reports the *pipeline* as failed on its non-zero signal
  # exit even though grep itself matched -- silently making this `if` read
  # as false (and thus never actually removing the unit) when the service
  # is present. Caught by installer-ci.yml's Docker smoke test.
  if [[ -f /etc/systemd/system/qrx-agent.service ]]; then
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
}

purge_data() {
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
}

remove_qrx_core() {
  # --- 3. QRX Core: opt-in, and only if THIS installer put it there ---
  [[ "$REMOVE_QRX_CORE" == "1" ]] || return 0

  local target status=0
  target="$(resolve_qrx_core_target "$QRX_CORE_MARKER" "$QRX_CORE_INSTALL_ROOT")" || status=$?

  case "$status" in
    0)
      if confirm "Remove the QRX Core installation that install.sh set up (${target})? (blockchain data and wallet are NEVER removed by this script, regardless)"; then
        rm -rf "$target"
        echo "  - removed ${target}"
      else
        echo "  - kept QRX Core (confirmation declined)"
      fi
      ;;
    2)
      echo "  - --remove-qrx-core given, but the recorded path is outside ${QRX_CORE_INSTALL_ROOT} -- refusing to remove it as a safety measure. Remove it yourself if this is really a QRX Core installation." >&2
      ;;
    *)
      if [[ -f "$QRX_CORE_MARKER" ]]; then
        echo "  - --remove-qrx-core given, but the recorded QRX Core directory no longer exists (nothing to remove)"
      else
        echo "  - --remove-qrx-core given, but no record shows this installer installed QRX Core (nothing to remove here; a pre-existing QRX Core is never touched by this script)"
      fi
      ;;
  esac
}

main() {
  parse_args "$@"
  require_root

  echo "QRX Node Suite Uninstaller"
  echo ""

  remove_software
  purge_data
  remove_qrx_core

  echo ""
  echo "QRX Core's blockchain data and wallet files, if any, were never touched by this script."
  echo "Done."
}

# Only auto-run when actually executed, never when sourced -- see
# installer/test/test-uninstall.sh, which sources this file to unit-test
# resolve_qrx_core_target() directly without running a real uninstall (no
# root, no systemctl, no filesystem changes outside a tempdir).
if ! (return 0 2>/dev/null); then
  main "$@"
fi
