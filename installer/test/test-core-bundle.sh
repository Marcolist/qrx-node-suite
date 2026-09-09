#!/usr/bin/env bash
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck disable=SC1091
source "${REPO_ROOT}/installer/qrx-core-sources.sh"

PASS=0
FAIL=0
assert_status() {
  local description="$1" expected="$2"
  shift 2
  local actual=0
  ( "$@" ) >/dev/null 2>&1 || actual=$?
  if [[ "$actual" == "$expected" ]]; then
    echo "  ok - ${description}"
    PASS=$((PASS + 1))
  else
    echo "  FAIL - ${description}: expected ${expected}, got ${actual}"
    FAIL=$((FAIL + 1))
  fi
}

fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/bin"
for binary in qrx qrxd qrx-cli qrxdb_verify qrxdb_salvage qrxdb_compact qrxdb_snapshot; do
  printf '#!/bin/sh\nexit 0\n' >"$fixture/bin/$binary"
  chmod 0755 "$fixture/bin/$binary"
done
printf '0.0.7\n' >"$fixture/VERSION"
printf '4a732c1a7d2b03fb299eabde437646c99c797e2d\n' >"$fixture/SOURCE_COMMIT"
printf '3.6.4\n' >"$fixture/OPENSSL_VERSION"
printf '9bffaa1ad1e07b354c21bd3324ec02fa15579f45a7d0494b3e74bc449b7333ef\n' >"$fixture/OPENSSL_SOURCE_SHA256"
printf '80b600d5a8aba915a262a9a658539c3d64ce660fb85e341eb3ea773ad618aa4f\n' >"$fixture/SUITE_PATCH_SHA256"
printf 'fixture license\n' >"$fixture/LICENSE.qrx-core"

echo "== bundled Core provenance =="
assert_status "exact bundle metadata is accepted" 0 qrx_core_validate_bundle "$fixture"
printf '0.0.8\n' >"$fixture/VERSION"
assert_status "wrong Core version is rejected" 1 qrx_core_validate_bundle "$fixture"
printf '0.0.7\n' >"$fixture/VERSION"
printf 'untrusted\n' >"$fixture/SOURCE_COMMIT"
assert_status "wrong Core commit is rejected" 1 qrx_core_validate_bundle "$fixture"
printf '4a732c1a7d2b03fb299eabde437646c99c797e2d\n' >"$fixture/SOURCE_COMMIT"
printf 'wrong\n' >"$fixture/OPENSSL_SOURCE_SHA256"
assert_status "wrong OpenSSL source hash is rejected" 1 qrx_core_validate_bundle "$fixture"
printf '9bffaa1ad1e07b354c21bd3324ec02fa15579f45a7d0494b3e74bc449b7333ef\n' >"$fixture/OPENSSL_SOURCE_SHA256"
printf 'wrong\n' >"$fixture/SUITE_PATCH_SHA256"
assert_status "wrong suite patch hash is rejected" 1 qrx_core_validate_bundle "$fixture"
printf '80b600d5a8aba915a262a9a658539c3d64ce660fb85e341eb3ea773ad618aa4f\n' >"$fixture/SUITE_PATCH_SHA256"
chmod 0644 "$fixture/bin/qrxd"
assert_status "non-executable daemon is rejected" 1 qrx_core_validate_bundle "$fixture"

echo "${PASS} passed, ${FAIL} failed"
[[ "$FAIL" == 0 ]]
