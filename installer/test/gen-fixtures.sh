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

# sums.txt mimics a real SHA256SUMS file (a checksum line for "data.txt"),
# signed the same way release.yml signs a real one -- used to exercise
# verify_release()'s GitHub-release code path (not just verify_signature()
# in isolation), where the signature covers the checksums file, not the
# asset it references.
data_sha256="$(sha256sum "${OUT}/data.txt" | awk '{print $1}')"
printf '%s  data.txt\n' "$data_sha256" >"${OUT}/sums.txt"
(cd "${REPO_ROOT}/agent" && go run ./cmd/sign-checksums -key "${TMP}/test.priv" -in "${OUT}/sums.txt" -out "${OUT}/sums.txt.sig")

echo "wrote ${OUT}/{pub.b64,data.txt,data.txt.sig,tampered.txt,sums.txt,sums.txt.sig}"
echo "(the test-only private key used to sign these was discarded, not committed)"
