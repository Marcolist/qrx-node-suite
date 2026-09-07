#!/usr/bin/env bash
# Unit tests for install.sh's pure functions: sources the script (which,
# per the guard at its own end, never runs main() or touches the
# filesystem when sourced) and calls individual functions directly. No
# Docker/root/network required; run with:
#   bash installer/test/test-detection.sh
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck disable=SC1091
source "${REPO_ROOT}/install.sh"

# install.sh's own `set -Eeuo pipefail` is now active in this shell too
# (sourcing runs in the current shell, not a subshell) -- this test runner
# deliberately invokes functions that fail and checks their exit codes
# itself (assert_status), so errexit must be off or the first such failure
# would kill the whole test run before its exit code could be inspected.
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

assert_contains() {
  local desc="$1" haystack="$2" needle="$3"
  if [[ "$haystack" == *"$needle"* ]]; then
    echo "  ok - ${desc}"
    PASS=$((PASS + 1))
  else
    echo "  FAIL - ${desc}: expected output to contain [${needle}]"
    FAIL=$((FAIL + 1))
  fi
}

# assert_status DESC EXPECTED_EXIT -- command...
# Runs the command in its own forked subshell ( ... ), NOT as a plain
# function call in the current shell -- several functions under test
# (detect_arch, detect_os, ...) call die(), which does a bare `exit`. Called
# directly, that `exit` would terminate this whole test script rather than
# just "failing" the one command; forking a subshell here means the exit
# only ends that subshell, and `$?` right after still captures its status
# correctly in this (unaffected) shell.
assert_status() {
  local desc="$1" expected="$2"
  shift 2
  local actual
  ( "$@" ) >/tmp/qrx-test-out 2>&1
  actual=$?
  if [[ "$actual" == "$expected" ]]; then
    echo "  ok - ${desc}"
    PASS=$((PASS + 1))
  else
    echo "  FAIL - ${desc}: expected exit ${expected}, got ${actual} ($(cat /tmp/qrx-test-out))"
    FAIL=$((FAIL + 1))
  fi
}

echo "== detect_arch =="
# uname is invoked indirectly, from install.sh's detect_arch() (`uname -m`)
# -- shellcheck can't trace calls across the `source` boundary into
# install.sh, hence the disable below.
MOCK_UNAME_OUTPUT=""
# shellcheck disable=SC2329
uname() { echo "$MOCK_UNAME_OUTPUT"; }

MOCK_UNAME_OUTPUT="x86_64"
detect_arch
assert_eq "x86_64 -> amd64" "amd64" "$GOARCH"

MOCK_UNAME_OUTPUT="aarch64"
detect_arch
assert_eq "aarch64 -> arm64" "arm64" "$GOARCH"

MOCK_UNAME_OUTPUT="armv7l"
assert_status "armv7l is rejected" 1 detect_arch

MOCK_UNAME_OUTPUT="riscv64"
assert_status "unknown arch is rejected" 1 detect_arch
unset -f uname
unset MOCK_UNAME_OUTPUT

echo "== detect_os =="
FIXTURES="${REPO_ROOT}/installer/test/fixtures"

# QRX_OS_RELEASE_FILE is read by install.sh's detect_os() (sourced above) --
# not visible to static analysis across the `source` boundary.
# shellcheck disable=SC2034
QRX_OS_RELEASE_FILE="${FIXTURES}/ubuntu-24.04.os-release"
detect_os
assert_eq "ubuntu 24.04 ID" "ubuntu" "$DISTRO"
assert_eq "ubuntu 24.04 VERSION_ID" "24.04" "$DISTRO_VERSION"

# shellcheck disable=SC2034
QRX_OS_RELEASE_FILE="${FIXTURES}/debian-12.os-release"
detect_os
assert_eq "debian 12 ID" "debian" "$DISTRO"

# shellcheck disable=SC2034
QRX_OS_RELEASE_FILE="${FIXTURES}/unknown.os-release"
unknown_out="$(detect_os 2>&1)"
unknown_status=$?
assert_eq "unrecognized distro does not die" "0" "$unknown_status"
assert_contains "unrecognized distro warns" "$unknown_out" "not one of the officially tested targets"

# shellcheck disable=SC2034
QRX_OS_RELEASE_FILE="/nonexistent/os-release"
assert_status "missing os-release dies" 1 detect_os
unset QRX_OS_RELEASE_FILE

echo "== hex_to_bin =="
hex_out="$(mktemp)"
hex_to_bin "0a00ff" "$hex_out"
hex_actual="$(od -An -tx1 "$hex_out" | tr -d ' \n')"
assert_eq "hex_to_bin preserves 0x00 byte" "0a00ff" "$hex_actual"
rm -f "$hex_out"

echo "== verify_signature (real keypair round-trip) =="
SIG_FIXTURES="${REPO_ROOT}/installer/test/fixtures/sig"
if [[ -d "$SIG_FIXTURES" ]]; then
  WORKDIR="$(mktemp -d)"
  # LOG_FILE and QRX_TRUSTED_PUBLIC_KEY_B64 are read by install.sh's
  # verify_signature() (sourced above); shellcheck can't see that across
  # the `source` boundary.
  # shellcheck disable=SC2034
  LOG_FILE="/tmp/qrx-test.log"
  # shellcheck disable=SC2034
  QRX_TRUSTED_PUBLIC_KEY_B64="$(cat "${SIG_FIXTURES}/pub.b64")"
  assert_status "valid signature verifies" 0 verify_signature "${SIG_FIXTURES}/data.txt" "${SIG_FIXTURES}/data.txt.sig"
  assert_status "tampered data fails verification" 1 verify_signature "${SIG_FIXTURES}/tampered.txt" "${SIG_FIXTURES}/data.txt.sig"
  rm -rf "$WORKDIR"
else
  echo "  (skipped: ${SIG_FIXTURES} not present -- run installer/test/gen-fixtures.sh)"
fi

echo ""
echo "${PASS} passed, ${FAIL} failed"
[[ "$FAIL" -eq 0 ]]
