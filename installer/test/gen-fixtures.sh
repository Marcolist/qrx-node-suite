#!/usr/bin/env bash
# Regenerates installer/test/fixtures/sig/* -- a disposable, TEST-ONLY
# Ed25519 keypair (never the real release signing key) used purely to
# exercise install.sh's verify_signature()/hex_to_bin() logic end-to-end
# against real crypto output. The test-only private key is generated fresh
# each run and discarded immediately after signing; only its public key and
# the signed fixture files are committed.
set -Eeuo pipefail

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${TEST_DIR}/../.." && pwd)"
OUT="${TEST_DIR}/fixtures/sig"
mkdir -p "$OUT"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

(cd "${REPO_ROOT}/agent" && go run ./cmd/gen-signing-key -priv "${TMP}/test.priv" -pub "${TMP}/test.pub")

cp "${TMP}/test.pub" "${OUT}/pub.b64"
printf 'this is a fixture file used only by installer/test/test-detection.sh\n' >"${OUT}/data.txt"

(cd "${REPO_ROOT}/agent" && go run ./cmd/sign-checksums -key "${TMP}/test.priv" -in "${OUT}/data.txt" -out "${OUT}/data.txt.sig")

cp "${OUT}/data.txt" "${OUT}/tampered.txt"
printf 'tampered\n' >>"${OUT}/tampered.txt"

echo "wrote ${OUT}/{pub.b64,data.txt,data.txt.sig,tampered.txt}"
echo "(the test-only private key used to sign these was discarded, not committed)"
