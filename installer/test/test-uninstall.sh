#!/usr/bin/env bash
# Unit tests for installer/linux/uninstall.sh's pure functions: sources the
# script (which, per the guard at its own end, never runs main() or touches
# the filesystem when sourced) and calls individual functions directly. No
# Docker/root/network required; run with:
#   bash installer/test/test-uninstall.sh
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck disable=SC1091
source "${REPO_ROOT}/installer/linux/uninstall.sh"

# uninstall.sh's own `set -Eeuo pipefail` is now active in this shell too --
# see installer/test/test-detection.sh for why errexit must be off for the
# rest of this test runner.
set +e

PASS=0
FAIL=0

assert_eq() {
  local desc="$1" expected="$2" actual="$3"
  if [[ "$expected" == "$actual" ]]; then
    echo "  ok - ${desc}"
    PASS=$((PASS + 1))
  else
    echo "  FAIL - ${desc}: expected [${expected}], got [${actual}]"
    FAIL=$((FAIL + 1))
  fi
}

echo "== resolve_qrx_core_target (F05) =="
# Regression tests for the F05 finding (external security audit): the
# marker file uninstall.sh's --remove-qrx-core reads used to live under
# QRX_DATA_DIR, which the unprivileged qrx-agent service user can write to
# -- so a compromised agent process could point it at an arbitrary
# directory and have uninstall.sh `rm -rf` it as root. resolve_qrx_core_target
# is the pulled-out safety check: it must never return success for a
# recorded path outside the documented QRX Core install root, regardless of
# where the marker file itself lives.
test_root="$(mktemp -d)"
install_root="${test_root}/opt/qrx"
mkdir -p "${install_root}/current"

marker="${test_root}/marker"

# No marker file at all.
rm -f "$marker"
out="$(resolve_qrx_core_target "$marker" "$install_root")"
status=$?
assert_eq "no marker file: exit status" "1" "$status"
assert_eq "no marker file: no output" "" "$out"

# Marker names a directory that doesn't exist (anymore).
echo "${install_root}/nonexistent-version" >"$marker"
out="$(resolve_qrx_core_target "$marker" "$install_root")"
status=$?
assert_eq "nonexistent target dir: exit status" "1" "$status"

# Marker names a real directory INSIDE the install root -- must succeed and
# echo the resolved path.
echo "${install_root}/current" >"$marker"
out="$(resolve_qrx_core_target "$marker" "$install_root")"
status=$?
assert_eq "in-root target: exit status" "0" "$status"
assert_eq "in-root target: echoes resolved path" "${install_root}/current" "$out"

# Marker names an absolute path OUTSIDE the install root -- the core F05
# attack: a compromised qrx-agent process planting an arbitrary rm -rf
# target. Must be refused (status 2, distinct from "no marker"/"gone"), and
# nothing printed that a careless caller might still act on.
attacker_target="${test_root}/attacker-controlled-directory"
mkdir -p "$attacker_target"
echo "$attacker_target" >"$marker"
out="$(resolve_qrx_core_target "$marker" "$install_root")"
status=$?
assert_eq "outside-root target: exit status" "2" "$status"
assert_eq "outside-root target: no output" "" "$out"
[[ -d "$attacker_target" ]] # sanity: this test never actually deletes it

# Marker names a path that only ESCAPES the install root via a symlink --
# resolve_qrx_core_target must follow it and reject based on the real
# (symlink-resolved) location, not the literal string.
escape_target="${test_root}/escape-target"
mkdir -p "$escape_target"
ln -s "$escape_target" "${install_root}/sneaky-link"
echo "${install_root}/sneaky-link" >"$marker"
out="$(resolve_qrx_core_target "$marker" "$install_root")"
status=$?
assert_eq "symlink escape: exit status" "2" "$status"

rm -rf "$test_root"

echo ""
echo "== --purge-data --remove-qrx-core --yes together (R04) =="
# Regression test for the R04 finding (external re-review of the F05 fix):
# the F05 fix moved QRX_CORE_MARKER under QRX_CONFIG_DIR (deliberately --
# root-only-writable, unlike the old QRX_DATA_DIR location), but main()
# called purge_data (which `rm -rf`s the whole QRX_CONFIG_DIR) BEFORE
# remove_qrx_core -- so passing both flags together silently deleted the
# marker before remove_qrx_core ever got to read it, and QRX Core was
# never actually removed despite being explicitly requested. This calls
# remove_qrx_core then purge_data directly, in the order main() now uses
# (not main() itself, which calls require_root() and remove_software(),
# the latter touching real, hard-coded system paths like
# /etc/systemd/system/qrx-agent.service -- neither belongs in a portable,
# no-root-required unit test), against a fully isolated tempdir.
test_root="$(mktemp -d)"
QRX_CONFIG_DIR="${test_root}/etc/qrx-node-suite"
QRX_DATA_DIR="${test_root}/var/lib/qrx-node-suite"
# QRX_LOG_DIR, PURGE_DATA, REMOVE_QRX_CORE, and ASSUME_YES below are all
# read only by functions defined in uninstall.sh (sourced above) --
# invisible to shellcheck's per-file analysis across that `source`
# boundary, same as the existing disables elsewhere in this file.
# shellcheck disable=SC2034
QRX_LOG_DIR="${test_root}/var/log/qrx-node-suite"
QRX_CORE_INSTALL_ROOT="${test_root}/opt/qrx"
QRX_CORE_MARKER="${QRX_CONFIG_DIR}/qrx-core-installed-by-this-installer"
QRX_CORE_UNIT_FILE="${test_root}/etc/systemd/system/qrxd.service"
# remove_qrx_core manages the service in production. Keep this pure-function
# test from talking to the host's real systemd instance.
systemctl() { return 0; }
# shellcheck disable=SC2034
PURGE_DATA=1
# shellcheck disable=SC2034
REMOVE_QRX_CORE=1
# shellcheck disable=SC2034
ASSUME_YES=1

core_dir="${QRX_CORE_INSTALL_ROOT}/current"
mkdir -p "$core_dir" "$QRX_CONFIG_DIR"
echo "core binary" >"${core_dir}/qrx-cli"
echo "$core_dir" >"$QRX_CORE_MARKER"

remove_qrx_core_out="$(remove_qrx_core 2>&1)"
purge_data >/dev/null 2>&1

if [[ -d "$core_dir" ]]; then
  echo "  FAIL - QRX Core directory still exists after remove_qrx_core+purge_data"
  FAIL=$((FAIL + 1))
else
  echo "  ok - QRX Core directory was actually removed"
  PASS=$((PASS + 1))
fi
if [[ -d "$QRX_CONFIG_DIR" || -d "$QRX_DATA_DIR" ]]; then
  echo "  FAIL - config/data directories still exist after purge_data"
  FAIL=$((FAIL + 1))
else
  echo "  ok - config/data directories were purged"
  PASS=$((PASS + 1))
fi
if [[ "$remove_qrx_core_out" == *"no record shows this installer installed QRX Core"* ]]; then
  echo "  FAIL - remove_qrx_core reported no marker (it must run before purge_data, not after)"
  FAIL=$((FAIL + 1))
else
  echo "  ok - remove_qrx_core found and used the marker before purge_data ran"
  PASS=$((PASS + 1))
fi

rm -rf "$test_root"
# shellcheck disable=SC2034
PURGE_DATA=0
# shellcheck disable=SC2034
REMOVE_QRX_CORE=0
# shellcheck disable=SC2034
ASSUME_YES=0

echo ""
echo "${PASS} passed, ${FAIL} failed"
[[ "$FAIL" -eq 0 ]]
