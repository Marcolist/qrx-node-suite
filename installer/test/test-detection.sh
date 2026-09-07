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

echo "== verify_release refuses an unsigned GitHub release (F01) =="
if [[ -d "$SIG_FIXTURES" ]]; then
  # Mock download(): serve fixture bytes for a couple of fake URLs instead
  # of curling, so this exercises verify_release()'s real control flow
  # (not just verify_signature() in isolation) without network access.
  # shellcheck disable=SC2329
  download() {
    local url="$1" dest="$2"
    case "$url" in
      dev://asset) cp "${SIG_FIXTURES}/data.txt" "$dest" ;;
      dev://sums) cp "${SIG_FIXTURES}/sums.txt" "$dest" ;;
      dev://sig) cp "${SIG_FIXTURES}/sums.txt.sig" "$dest" ;;
      *) return 1 ;;
    esac
  }

  WORKDIR="$(mktemp -d)"
  LOG_FILE="/tmp/qrx-test.log"
  # These are all read only by install.sh's verify_release() (sourced
  # above), invisible to shellcheck across the `source` boundary.
  # shellcheck disable=SC2034
  QRX_TRUSTED_PUBLIC_KEY_B64="$(cat "${SIG_FIXTURES}/pub.b64")"
  # shellcheck disable=SC2034
  QRX_LOCAL_TARBALL=""
  # shellcheck disable=SC2034
  ASSET_NAME="data.txt"
  # shellcheck disable=SC2034
  ASSET_URL="dev://asset"
  # shellcheck disable=SC2034
  SUMS_URL="dev://sums"
  # shellcheck disable=SC2034
  RELEASE_TAG="v-test"

  # shellcheck disable=SC2034
  SIG_URL=""
  release_out="$(verify_release 2>&1)"
  release_status=$?
  assert_eq "missing SIG_URL is refused (F01)" "1" "$release_status"
  assert_contains "refusal names the reason" "$release_out" "no SHA256SUMS.sig"
  [[ -f "${WORKDIR}/data.txt" ]] && rm -f "${WORKDIR}/data.txt" # verify_release downloads before refusing; start clean for the next case

  # shellcheck disable=SC2034
  SIG_URL="dev://sig"
  assert_status "present + valid SIG_URL is accepted" 0 verify_release

  unset -f download
  rm -rf "$WORKDIR"
else
  echo "  (skipped: ${SIG_FIXTURES} not present -- run installer/test/gen-fixtures.sh)"
fi

echo "== setup_logging refuses to follow a symlink at LOG_FILE (F04) =="
{
  # Regression test for the F04 finding (external security audit):
  # QRX_LOG_DIR was made agent-writable by install_release(), and
  # setup_logging() (which runs as root, before install_release ever
  # re-secures the directory on a re-run) did `: >"$LOG_FILE"`, which
  # follows a symlink. A compromised unprivileged agent process could
  # plant a symlink at that exact path pointing at any root-owned file,
  # turning a routine re-run of this installer into a root-privileged
  # arbitrary-file-truncation primitive. This proves setup_logging() now
  # refuses to follow such a symlink: the "secret" file it points at must
  # survive untouched, and LOG_FILE itself must end up a real regular file.
  test_root="$(mktemp -d)"
  # shellcheck disable=SC2034
  QRX_LOG_DIR="${test_root}/log"
  mkdir -p "$QRX_LOG_DIR"

  secret="${test_root}/secret-owned-by-someone-else"
  echo "do not touch me" >"$secret"
  ln -s "$secret" "${QRX_LOG_DIR}/install.log"

  setup_logging

  assert_eq "LOG_FILE points at the log dir" "${QRX_LOG_DIR}/install.log" "$LOG_FILE"
  if [[ -L "$LOG_FILE" ]]; then
    echo "  FAIL - LOG_FILE is still a symlink after setup_logging"
    FAIL=$((FAIL + 1))
  else
    echo "  ok - LOG_FILE is a real regular file after setup_logging"
    PASS=$((PASS + 1))
  fi
  assert_eq "the symlink target was never truncated" "do not touch me" "$(cat "$secret")"

  rm -rf "$test_root"
}

echo "== init_log_file survives a concurrent symlink-replant race (R02) =="
{
  # Regression test for the R02 finding (external re-review of the F04
  # fix): the previous fix was "check -L, rm, then truncate" -- a
  # check-then-act pattern with its own race window between the check and
  # the truncate, where a still-running compromised agent process could
  # replant the symlink in between. init_log_file replaced that with an
  # atomic build-elsewhere-then-rename, which by construction has no such
  # window: it never opens through the destination path at all. This
  # proves it under an actual concurrent attacker, not just a
  # symlink-already-present snapshot: a background loop continuously
  # replaces the target path with a fresh symlink to the victim file while
  # the foreground repeatedly calls init_log_file, as fast as each can
  # go, for real wall-clock time -- not a fixed iteration count racing
  # against unknown scheduling. The victim's content must survive every
  # single one of those attempts.
  test_root="$(mktemp -d)"
  victim="${test_root}/victim"
  target="${test_root}/install.log"
  echo "do not touch me either" >"$victim"

  attacker_stop="${test_root}/.stop"
  attacker() {
    while [[ ! -e "$attacker_stop" ]]; do
      ln -sf "$victim" "$target" 2>/dev/null
    done
  }
  attacker &
  attacker_pid=$!

  race_failures=0
  race_iterations=0
  end_time=$((SECONDS + 2))
  while [[ $SECONDS -lt $end_time ]]; do
    init_log_file "$target" || true
    race_iterations=$((race_iterations + 1))
    if [[ "$(cat "$victim" 2>/dev/null)" != "do not touch me either" ]]; then
      race_failures=$((race_failures + 1))
      break
    fi
  done

  touch "$attacker_stop"
  wait "$attacker_pid" 2>/dev/null

  assert_eq "victim survived ${race_iterations} racing init_log_file calls" "0" "$race_failures"
  assert_eq "victim content is untouched after the race" "do not touch me either" "$(cat "$victim")"

  rm -rf "$test_root"
}

echo ""
echo "${PASS} passed, ${FAIL} failed"
[[ "$FAIL" -eq 0 ]]
