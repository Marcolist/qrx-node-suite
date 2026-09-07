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
echo "${PASS} passed, ${FAIL} failed"
[[ "$FAIL" -eq 0 ]]
