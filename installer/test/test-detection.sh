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

echo "== generate_admin_token =="
generate_admin_token
assert_eq "admin token has a stable 40-character length" "40" "${#ADMIN_TOKEN}"
if [[ "$ADMIN_TOKEN" =~ ^[A-Za-z0-9_-]{40}$ ]]; then
  echo "  ok - admin token uses the base64url alphabet"
  PASS=$((PASS + 1))
else
  echo "  FAIL - admin token contains characters outside the base64url alphabet"
  FAIL=$((FAIL + 1))
fi

echo "== semantic release ordering =="
assert_status "a final release supersedes its prerelease" 0 semver_is_greater 0.2.0 0.2.0-test4
assert_status "a prerelease does not supersede its final release" 1 semver_is_greater 0.2.0-test4 0.2.0
assert_status "a patch release supersedes the prior final" 0 semver_is_greater 0.2.1 0.2.0
assert_status "newer prerelease identifiers advance" 0 semver_is_greater 0.2.0-rc.2 0.2.0-rc.1
assert_status "build metadata does not change precedence" 1 semver_is_greater 0.2.0+build.2 0.2.0+build.1

echo "== validate_release_archive =="
archive_root="$(mktemp -d)"
mkdir -p "$archive_root/safe/dashboard"
printf 'binary\n' >"$archive_root/safe/agentd"
printf 'html\n' >"$archive_root/safe/dashboard/index.html"
tar -czf "$archive_root/safe.tar.gz" -C "$archive_root/safe" .
assert_status "regular files and directories are accepted" 0 validate_release_archive "$archive_root/safe.tar.gz"
ln -s /etc/passwd "$archive_root/safe/escape"
tar -czf "$archive_root/link.tar.gz" -C "$archive_root/safe" .
assert_status "symbolic links are rejected" 1 validate_release_archive "$archive_root/link.tar.gz"
tar -czf "$archive_root/traversal.tar.gz" --transform='s|^\./|../|' -C "$archive_root/safe" . 2>/dev/null
assert_status "parent-directory traversal is rejected" 1 validate_release_archive "$archive_root/traversal.tar.gz"
rm -rf "$archive_root"

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

echo "== log() writes survive a symlink planted AFTER setup_logging (R02 re-review) =="
{
  # Regression test for the R02 re-review's finding: the original R02 fix
  # (init_log_file's atomic rename) only protected the very FIRST write.
  # Every later log() call still reopened $LOG_FILE by path
  # (`>>"$LOG_FILE"`), so a symlink planted at that path at any point
  # during the rest of the install -- QRX_LOG_DIR can stay agent-writable
  # until install_release() re-secures it, much later in main() -- would
  # have every subsequent append follow it. This proves setup_logging()
  # now holds a file descriptor (LOG_FD) bound to the real log file's
  # inode, so a symlink planted after setup_logging() has already run
  # affects neither the victim it points at nor where later log() calls'
  # content actually goes.
  test_root="$(mktemp -d)"
  # shellcheck disable=SC2034
  QRX_LOG_DIR="${test_root}/log"
  mkdir -p "$QRX_LOG_DIR"

  setup_logging
  real_log_file="$LOG_FILE"

  victim="${test_root}/victim-owned-by-someone-else"
  echo "do not touch me" >"$victim"
  # Attacker (a still-running compromised agent, in the real scenario)
  # replaces the log path with a symlink AFTER initialization.
  ln -sf "$victim" "$real_log_file"

  log "this line must land in the real log file, not the victim"

  assert_eq "victim is still untouched by the later log() call" "do not touch me" "$(cat "$victim")"
  # The path now resolves to the attacker's symlink, so the real log
  # content is only reachable through the still-open fd -- read it that
  # way (Linux-specific /proc/self/fd, fine for this Linux-only
  # installer) to prove the line was actually written somewhere real, not
  # just silently dropped (which would also make the victim-untouched
  # assertion above trivially pass for the wrong reason).
  real_content="$(cat "/proc/self/fd/${LOG_FD}" 2>/dev/null)"
  assert_contains "the log line actually landed in the real file (via the fd)" "$real_content" "this line must land in the real log file"
  if [[ -L "$real_log_file" ]]; then
    echo "  ok - the path is still the attacker's symlink (log() correctly ignored it and wrote via LOG_FD instead)"
    PASS=$((PASS + 1))
  else
    echo "  FAIL - expected the symlink at LOG_FILE's path to be left alone (log() should write via the held fd, never touch the path)"
    FAIL=$((FAIL + 1))
  fi

  rm -rf "$test_root"
}

echo "== init_log_file does not permanently redirect stderr =="
{
  test_root="$(mktemp -d)"
  init_log_file "${test_root}/install.log"
  stderr_probe="$({ echo "stderr remains visible" >&2; } 2>&1)"
  assert_eq "stderr still reaches its caller after init_log_file" "stderr remains visible" "$stderr_probe"
  rm -rf "$test_root"
}

echo "== subprocess diagnostics use the held log descriptor =="
{
  test_root="$(mktemp -d)"
  # shellcheck disable=SC2034
  QRX_LOG_DIR="${test_root}/log"
  mkdir -p "$QRX_LOG_DIR"
  setup_logging
  real_log_file="$LOG_FILE"
  victim="${test_root}/victim"
  echo "do not touch me" >"$victim"
  ln -sf "$victim" "$real_log_file"

  # shellcheck disable=SC2329
  curl() { echo "mock curl diagnostic" >&2; return 0; }
  check_connectivity curl
  unset -f curl

  assert_eq "subprocess did not follow a replaced log path" "do not touch me" "$(cat "$victim")"
  real_content="$(cat "/proc/self/fd/${LOG_FD}" 2>/dev/null)"
  assert_contains "subprocess diagnostic landed via LOG_FD" "$real_content" "mock curl diagnostic"
  rm -rf "$test_root"
}

echo ""
echo "${PASS} passed, ${FAIL} failed"
[[ "$FAIL" -eq 0 ]]
