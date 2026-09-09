#!/usr/bin/env bash
# Build the pinned QRX Core 0.0.7 source with a private static OpenSSL.
set -Eeuo pipefail

SOURCE_DIR="${1:?usage: build-qrx-core-linux.sh CORE_SOURCE_DIR OUTPUT_DIR}"
OUTPUT_DIR="${2:?usage: build-qrx-core-linux.sh CORE_SOURCE_DIR OUTPUT_DIR}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OPENSSL_VERSION="${OPENSSL_VERSION:-3.6.4}"
OPENSSL_SHA256="9bffaa1ad1e07b354c21bd3324ec02fa15579f45a7d0494b3e74bc449b7333ef"
CORE_COMMIT="4a732c1a7d2b03fb299eabde437646c99c797e2d"
PATCH_FILE="${SCRIPT_DIR}/core/qrx-0.0.7-cli-readonly.patch"
OPENSSL_PREFIX="${OPENSSL_PREFIX:-${RUNNER_TEMP:-/tmp}/qrx-openssl-${OPENSSL_VERSION}-$(uname -m)}"
BUILD_DIR="${BUILD_DIR:-${RUNNER_TEMP:-/tmp}/qrx-core-build}"
JOBS="${JOBS:-$(nproc)}"

actual_commit="$(git -C "$SOURCE_DIR" rev-parse HEAD)"
[[ "$actual_commit" == "$CORE_COMMIT" ]] || {
  echo "refusing to build unpinned QRX Core commit: ${actual_commit}" >&2
  exit 1
}

if [[ ! -f "${OPENSSL_PREFIX}/lib64/libcrypto.a" && ! -f "${OPENSSL_PREFIX}/lib/libcrypto.a" ]]; then
  archive="${RUNNER_TEMP:-/tmp}/openssl-${OPENSSL_VERSION}.tar.gz"
  source_tree="${RUNNER_TEMP:-/tmp}/openssl-${OPENSSL_VERSION}"
  curl --proto '=https' --tlsv1.2 -fsSL "https://www.openssl.org/source/openssl-${OPENSSL_VERSION}.tar.gz" -o "$archive"
  echo "${OPENSSL_SHA256}  ${archive}" | sha256sum -c -
  rm -rf "$source_tree"
  tar -xzf "$archive" -C "${RUNNER_TEMP:-/tmp}"
  (
    cd "$source_tree"
    ./Configure --prefix="$OPENSSL_PREFIX" --openssldir="$OPENSSL_PREFIX/ssl" no-shared no-module
    make -j"$JOBS"
    make install_sw install_ssldirs
  )
fi

libcrypto="${OPENSSL_PREFIX}/lib64/libcrypto.a"
[[ -f "$libcrypto" ]] || libcrypto="${OPENSSL_PREFIX}/lib/libcrypto.a"
[[ -f "$libcrypto" ]] || { echo "static libcrypto was not built" >&2; exit 1; }

patch -d "$SOURCE_DIR" -p1 --forward --batch --no-backup-if-mismatch <"$PATCH_FILE"
rm -rf "$BUILD_DIR" "$OUTPUT_DIR"
cmake -S "${SOURCE_DIR}/qrx-core" -B "$BUILD_DIR" \
  -DCMAKE_BUILD_TYPE=Release \
  -DOPENSSL_USE_STATIC_LIBS=TRUE \
  -DOPENSSL_ROOT_DIR="$OPENSSL_PREFIX" \
  -DOPENSSL_INCLUDE_DIR="$OPENSSL_PREFIX/include" \
  -DOPENSSL_CRYPTO_LIBRARY="$libcrypto"
cmake --build "$BUILD_DIR" --parallel "$JOBS"

install -d "$OUTPUT_DIR/bin"
for binary in qrx qrxd qrx-cli qrxdb_verify qrxdb_salvage qrxdb_compact qrxdb_snapshot; do
  install -m 0755 "${BUILD_DIR}/${binary}" "${OUTPUT_DIR}/bin/${binary}"
done
install -m 0644 "${SOURCE_DIR}/LICENSE" "${OUTPUT_DIR}/LICENSE.qrx-core"
printf '%s\n' '0.0.7' >"${OUTPUT_DIR}/VERSION"
printf '%s\n' "$CORE_COMMIT" >"${OUTPUT_DIR}/SOURCE_COMMIT"
printf '%s\n' "$OPENSSL_VERSION" >"${OUTPUT_DIR}/OPENSSL_VERSION"
printf '%s\n' "$OPENSSL_SHA256" >"${OUTPUT_DIR}/OPENSSL_SOURCE_SHA256"
sha256sum "$PATCH_FILE" | awk '{print $1}' >"${OUTPUT_DIR}/SUITE_PATCH_SHA256"

if ldd "${OUTPUT_DIR}/bin/qrxd" 2>/dev/null | grep -Eq 'libcrypto|libssl'; then
  echo "QRX Core unexpectedly depends on a dynamic OpenSSL library" >&2
  exit 1
fi
"${OUTPUT_DIR}/bin/qrx-cli" --help >/dev/null
