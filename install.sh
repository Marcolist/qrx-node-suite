#!/usr/bin/env bash
# QRX Node Suite -- one-line installer.
#
#   curl -sSL https://raw.githubusercontent.com/Marcolist/qrx-node-suite/main/install.sh | sudo bash
#
# Downloads a signed, checksummed release of QRX Node Suite, installs it as
# a systemd service under a dedicated unprivileged user, generates a safe
# default configuration, starts it, and verifies it's actually healthy
# before declaring success. See docs/installer.md for the full behavior,
# every environment variable below, and the security model.
#
# This script is meant to be read before you pipe it into `sudo bash` --
# there is nothing in here that isn't described in docs/installer.md.
set -Eeuo pipefail

# ============================================================
# Configuration (all overridable via environment variables --
# the default command above needs none of these)
# ============================================================
QRX_REPO_OWNER="${QRX_REPO_OWNER:-Marcolist}"
QRX_REPO_NAME="${QRX_REPO_NAME:-qrx-node-suite}"
QRX_CHANNEL="${QRX_CHANNEL:-stable}"                # stable | beta
QRX_VERSION="${QRX_VERSION:-latest}"                # "latest" or an exact tag like v0.1.0
QRX_INSTALL_QRX_CORE="${QRX_INSTALL_QRX_CORE:-auto}" # auto | skip
QRX_DASHBOARD_BIND="${QRX_DASHBOARD_BIND:-local}"   # local | lan
QRX_VERBOSE="${QRX_VERBOSE:-0}"
QRX_PREFIX="${QRX_PREFIX:-/opt/qrx-node-suite}"
QRX_CONFIG_DIR="${QRX_CONFIG_DIR:-/etc/qrx-node-suite}"
QRX_DATA_DIR="${QRX_DATA_DIR:-/var/lib/qrx-node-suite}"
QRX_LOG_DIR="${QRX_LOG_DIR:-/var/log/qrx-node-suite}"
QRX_SERVICE_USER="${QRX_SERVICE_USER:-qrx-agent}"
QRX_LISTEN_PORT="${QRX_LISTEN_PORT:-8787}"
QRX_ASSUME_YES="${QRX_ASSUME_YES:-1}"               # non-interactive by default (piped install)

# Testing / offline-install hooks -- documented in docs/installer.md, not
# part of the primary supported command. QRX_LOCAL_TARBALL lets CI (and
# air-gapped operators who already vetted a tarball out of band) install
# from a local file instead of GitHub, skipping release *discovery* but
# still requiring a SHA256SUMS next to it -- see verify_release().
QRX_LOCAL_TARBALL="${QRX_LOCAL_TARBALL:-}"

# The project's release-signing public key (agent/cmd/gen-signing-key).
# This is the trust anchor for verify_release()'s signature check -- it
# must never be fetched from the same release location it verifies (that
# would let a compromised release location trust itself). It is the same
# key family used by the Agent's own OTA manifest verification
# (docs/updates.md#update-manifest, docs/security.md).
QRX_TRUSTED_PUBLIC_KEY_B64="tIxWU9IsRUw1/TwS+mvkhBUjPWVezLQZ9S3uVm6BSJs="

INSTALLER_VERSION="1"
TOTAL_STEPS=8
STEP_NUM=0
WORKDIR=""
LOG_FILE="/tmp/qrx-node-suite-install.log"
INSTALL_START_EPOCH=$(date +%s)

# ============================================================
# Output helpers
# ============================================================
COLOR_GREEN=""; COLOR_YELLOW=""; COLOR_RED=""; COLOR_BOLD=""; COLOR_RESET=""
if [[ -t 1 ]]; then
  COLOR_GREEN=$'\033[32m'; COLOR_YELLOW=$'\033[33m'; COLOR_RED=$'\033[31m'
  COLOR_BOLD=$'\033[1m'; COLOR_RESET=$'\033[0m'
fi
SYM_OK=$'\xe2\x9c\x93'   # UTF-8 checkmark
SYM_FAIL=$'\xe2\x9c\x97' # UTF-8 cross

log() { echo "$(date -u '+%Y-%m-%dT%H:%M:%SZ') $*" >>"$LOG_FILE" 2>/dev/null || true; }

step() {
  STEP_NUM=$((STEP_NUM + 1))
  echo ""
  echo "${COLOR_BOLD}[${STEP_NUM}/${TOTAL_STEPS}] $*${COLOR_RESET}"
  log "STEP ${STEP_NUM}/${TOTAL_STEPS}: $*"
}

ok()   { echo "  ${COLOR_GREEN}${SYM_OK}${COLOR_RESET} $*"; log "OK: $*"; }
warn() { echo "  ${COLOR_YELLOW}!${COLOR_RESET} $*" >&2; log "WARN: $*"; }
info() { echo "  $*"; log "INFO: $*"; }
verbose() { [[ "$QRX_VERBOSE" == "1" ]] && echo "    $*" || true; log "VERBOSE: $*"; }

die() {
  echo "  ${COLOR_RED}${SYM_FAIL}${COLOR_RESET} $*" >&2
  log "FATAL: $*"
  exit 1
}

on_err() {
  local exit_code=$? line="$1"
  echo "" >&2
  echo "${COLOR_RED}Installation failed${COLOR_RESET} (install.sh:${line}, exit ${exit_code})." >&2
  echo "No changes were made beyond what earlier steps already logged as done;" >&2
  echo "your existing QRX Core installation and QRX Node Suite data (if any) were not touched." >&2
  echo "" >&2
  echo "Log: ${LOG_FILE}" >&2
  cleanup_workdir
}

cleanup_workdir() {
  if [[ -n "$WORKDIR" && -d "$WORKDIR" ]]; then
    rm -rf "$WORKDIR"
  fi
}

# install_traps and everything below is only ever *called* from main(), and
# main() itself only runs when this file is executed, not sourced (see the
# bottom of this file) -- so sourcing install.sh for tests (installer/test/)
# never installs a trap, prints the banner, requires root, or touches the
# filesystem. Nothing at this file's top level runs any of that itself.
install_traps() {
  trap 'on_err $LINENO' ERR
  trap cleanup_workdir EXIT
}

# ============================================================
# 0. Root check + logging setup
# ============================================================
require_root() {
  if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
    die "install.sh must be run as root -- it creates a system user, installs a systemd unit, and writes to ${QRX_PREFIX}, ${QRX_CONFIG_DIR}. Try: curl -sSL https://raw.githubusercontent.com/${QRX_REPO_OWNER}/${QRX_REPO_NAME}/main/install.sh | sudo bash"
  fi
}

setup_logging() {
  # Once QRX_LOG_DIR can plausibly be created, move logging there; until
  # then (or if creation fails, e.g. read-only /var in a container) keep
  # the /tmp default set above.
  if mkdir -p "$QRX_LOG_DIR" 2>/dev/null; then
    LOG_FILE="${QRX_LOG_DIR}/install.log"
  fi
  : >"$LOG_FILE" 2>/dev/null || true
  log "QRX Node Suite installer v${INSTALLER_VERSION} starting"
}

# ============================================================
# 1. Detect system
# ============================================================
DISTRO=""; DISTRO_VERSION=""; DISTRO_PRETTY=""
ARCH_RAW=""; GOARCH=""
HAS_SYSTEMD="0"
PI_MODEL=""
PRIMARY_IP=""

detect_arch() {
  ARCH_RAW="$(uname -m)"
  case "$ARCH_RAW" in
    x86_64|amd64) GOARCH="amd64" ;;
    aarch64|arm64) GOARCH="arm64" ;;
    *)
      die "unsupported CPU architecture '${ARCH_RAW}'. QRX Node Suite currently ships prebuilt releases for linux/amd64 and linux/arm64 only. See docs/installer.md for building from source on other architectures."
      ;;
  esac
}

detect_os() {
  # QRX_OS_RELEASE_FILE exists purely for installer/test/'s unit tests to
  # exercise this against synthetic os-release content for every supported
  # distro without needing a real container per distro; it's never set in
  # a real install.
  local os_release_file="${QRX_OS_RELEASE_FILE:-/etc/os-release}"
  if [[ ! -r "$os_release_file" ]]; then
    die "cannot read ${os_release_file} -- this doesn't look like a supported Linux distribution."
  fi
  # shellcheck disable=SC1090,SC1091
  . "$os_release_file"
  DISTRO="${ID:-unknown}"
  DISTRO_VERSION="${VERSION_ID:-unknown}"
  DISTRO_PRETTY="${PRETTY_NAME:-$DISTRO $DISTRO_VERSION}"

  case "$DISTRO" in
    ubuntu|debian|raspbian) : ;;
    *)
      warn "distribution '${DISTRO}' is not one of the officially tested targets (Ubuntu 22.04/24.04, Debian 12, Raspberry Pi OS). Continuing anyway since it's Debian-family-compatible-looking, but you're off the tested path."
      ;;
  esac

  if command -v systemctl >/dev/null 2>&1 && [[ -d /run/systemd/system ]]; then
    HAS_SYSTEMD="1"
  fi

  if [[ -r /proc/device-tree/model ]]; then
    PI_MODEL="$(tr -d '\0' </proc/device-tree/model 2>/dev/null || true)"
  fi
}

detect_ip() {
  PRIMARY_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
  if [[ -z "$PRIMARY_IP" ]] && command -v ip >/dev/null 2>&1; then
    PRIMARY_IP="$(ip -4 route get 1.1.1.1 2>/dev/null | awk '/src/ {for(i=1;i<=NF;i++) if ($i=="src") print $(i+1)}' | head -n1)"
  fi
}

print_system_summary() {
  info "Operating System   ${DISTRO_PRETTY}"
  info "Architecture       $([[ "$GOARCH" == "arm64" ]] && echo ARM64 || echo x86_64)"
  if [[ -n "$PI_MODEL" ]]; then
    info "Platform           ${PI_MODEL}"
  fi
  info "Init System        $([[ "$HAS_SYSTEMD" == "1" ]] && echo systemd || echo "none detected")"
}

# ============================================================
# 2. Pre-flight checks + dependency installation
# ============================================================
PKG_MANAGER=""

detect_package_manager() {
  if command -v apt-get >/dev/null 2>&1; then
    PKG_MANAGER="apt"
  fi
}

apt_update_done="0"
apt_update_once() {
  if [[ "$apt_update_done" != "1" ]]; then
    verbose "running apt-get update"
    DEBIAN_FRONTEND=noninteractive apt-get update -qq >>"$LOG_FILE" 2>&1 || warn "apt-get update failed; continuing with whatever package lists are already cached"
    apt_update_done="1"
  fi
}

# ensure_packages installs any of the given apt packages that aren't
# already present. Only ever called with small runtime tools (section 7/8
# of the design brief) -- never a compiler toolchain.
ensure_packages() {
  local missing=()
  local pkg
  for pkg in "$@"; do
    if ! dpkg -s "$pkg" >/dev/null 2>&1; then
      missing+=("$pkg")
    fi
  done
  if [[ ${#missing[@]} -eq 0 ]]; then
    return 0
  fi
  if [[ "$PKG_MANAGER" != "apt" ]]; then
    die "missing required packages (${missing[*]}) and no supported package manager (apt) was found to install them automatically."
  fi
  apt_update_once
  verbose "installing: ${missing[*]}"
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "${missing[@]}" >>"$LOG_FILE" 2>&1 \
    || die "failed to install required packages: ${missing[*]} (see ${LOG_FILE})"
}

check_connectivity() {
  local fetch_bin="$1"
  if [[ "$fetch_bin" == "curl" ]]; then
    curl -fsSL -m 8 -o /dev/null "https://api.github.com" 2>>"$LOG_FILE" \
      || die "cannot reach api.github.com. Check your internet connection (and any firewall/proxy) and try again."
  else
    wget -q -T 8 -O /dev/null "https://api.github.com" 2>>"$LOG_FILE" \
      || die "cannot reach api.github.com. Check your internet connection (and any firewall/proxy) and try again."
  fi
}

check_disk_space() {
  # Require at least 512MB free where we're about to install to. QRX_PREFIX
  # itself doesn't exist yet pre-install, so walk up to the nearest
  # existing ancestor directory before asking df about it.
  local target_dir="$QRX_PREFIX" avail_kb
  while [[ ! -d "$target_dir" && "$target_dir" != "/" ]]; do
    target_dir="$(dirname "$target_dir")"
  done
  avail_kb="$(df -Pk "$target_dir" 2>/dev/null | awk 'NR==2 {print $4}')"
  if [[ -n "$avail_kb" && "$avail_kb" -lt 524288 ]]; then
    die "less than 512MB free disk space available under ${target_dir}. Free up space and try again."
  fi
}

FETCH_BIN=""

preflight() {
  detect_package_manager
  # libsqlite3-0 is a RUNTIME dependency, not just a build one: agentd is
  # cgo-linked against the system libsqlite3 (docs/deployment.md#sqlite-
  # runtime-dependency, agent/storage/sqlite/README.md) and fails to start
  # at all without it -- caught by installer-ci.yml's Docker smoke test,
  # where the base image has no reason to already have it installed.
  ensure_packages ca-certificates curl tar gzip jq libsqlite3-0
  FETCH_BIN="curl"

  if [[ -z "$QRX_LOCAL_TARBALL" ]]; then
    check_connectivity "$FETCH_BIN"
  fi
  check_disk_space

  if [[ "$HAS_SYSTEMD" != "1" ]]; then
    die "systemd is required (QRX Node Suite installs and manages a systemd service). No systemd was detected on this system."
  fi
  ok "root privileges"
  ok "supported architecture (${GOARCH})"
  ok "systemd available"
  ok "network + disk space"
}

# ============================================================
# 3. Release discovery + download
# ============================================================
RELEASE_TAG=""
RELEASE_VERSION=""
ASSET_URL=""
ASSET_NAME=""
SUMS_URL=""
SIG_URL=""

github_api() {
  local url="$1"
  curl -fsSL -H "Accept: application/vnd.github+json" -m 20 "$url" 2>>"$LOG_FILE"
}

discover_release() {
  if [[ -n "$QRX_LOCAL_TARBALL" ]]; then
    [[ -f "$QRX_LOCAL_TARBALL" ]] || die "QRX_LOCAL_TARBALL=${QRX_LOCAL_TARBALL} does not exist"
    ASSET_NAME="$(basename "$QRX_LOCAL_TARBALL")"
    RELEASE_VERSION="local"
    info "using local tarball: ${QRX_LOCAL_TARBALL} (offline/test install -- see docs/installer.md)"
    return 0
  fi

  local api_url="https://api.github.com/repos/${QRX_REPO_OWNER}/${QRX_REPO_NAME}/releases"
  local release_json

  if [[ "$QRX_VERSION" != "latest" ]]; then
    release_json="$(github_api "${api_url}/tags/${QRX_VERSION}")" \
      || die "release ${QRX_VERSION} not found for ${QRX_REPO_OWNER}/${QRX_REPO_NAME}"
  elif [[ "$QRX_CHANNEL" == "stable" ]]; then
    release_json="$(github_api "${api_url}/latest")" \
      || die "could not query the latest stable release. See ${LOG_FILE}."
  else
    # beta channel: newest release overall, prerelease or not.
    release_json="$(github_api "${api_url}?per_page=1")" \
      || die "could not list releases for the beta channel. See ${LOG_FILE}."
    release_json="$(echo "$release_json" | jq -c '.[0]')"
  fi

  [[ -n "$release_json" && "$release_json" != "null" ]] \
    || die "no matching QRX Node Suite release found (channel=${QRX_CHANNEL}, version=${QRX_VERSION})"

  RELEASE_TAG="$(echo "$release_json" | jq -r '.tag_name')"
  RELEASE_VERSION="${RELEASE_TAG#v}"
  ASSET_NAME="qrx-node-suite_${RELEASE_VERSION}_linux_${GOARCH}.tar.gz"

  ASSET_URL="$(echo "$release_json" | jq -r --arg name "$ASSET_NAME" '.assets[] | select(.name == $name) | .browser_download_url')"
  SUMS_URL="$(echo "$release_json" | jq -r '.assets[] | select(.name == "SHA256SUMS") | .browser_download_url')"
  SIG_URL="$(echo "$release_json" | jq -r '.assets[] | select(.name == "SHA256SUMS.sig") | .browser_download_url')"

  [[ -n "$ASSET_URL" && "$ASSET_URL" != "null" ]] \
    || die "release ${RELEASE_TAG} has no ${ASSET_NAME} asset. This architecture/release combination may not be published yet."
  [[ -n "$SUMS_URL" && "$SUMS_URL" != "null" ]] \
    || die "release ${RELEASE_TAG} has no SHA256SUMS asset -- refusing to install an unverifiable release."
}

download() {
  local url="$1" dest="$2"
  curl -fsSL -m 120 -o "$dest" "$url" 2>>"$LOG_FILE" || die "download failed: ${url} (see ${LOG_FILE})"
}

# verify_release downloads (or reuses, for QRX_LOCAL_TARBALL) the tarball
# and its SHA256SUMS, checks the tarball's checksum against SHA256SUMS, and
# -- for a real GitHub release, never for a local test tarball, which has
# no signed manifest -- verifies SHA256SUMS itself was signed by
# QRX_TRUSTED_PUBLIC_KEY_B64 before trusting anything in it at all. Order
# matters: the signature covers the checksums, so it's checked before the
# checksum is used for anything.
verify_release() {
  local tarball="${WORKDIR}/${ASSET_NAME}"
  local sums="${WORKDIR}/SHA256SUMS"

  if [[ -n "$QRX_LOCAL_TARBALL" ]]; then
    cp "$QRX_LOCAL_TARBALL" "$tarball"
    local local_sums="${QRX_LOCAL_TARBALL}.sha256sums"
    if [[ -f "$local_sums" ]]; then
      cp "$local_sums" "$sums"
    else
      warn "no ${local_sums} found next to QRX_LOCAL_TARBALL -- computing a checksum locally instead of verifying an independently-published one. This is only appropriate for local/offline testing, never for a real install."
      (cd "$WORKDIR" && sha256sum "$ASSET_NAME" >SHA256SUMS)
    fi
  else
    download "$ASSET_URL" "$tarball"
    download "$SUMS_URL" "$sums"

    if [[ -n "$SIG_URL" && "$SIG_URL" != "null" ]]; then
      local sig="${WORKDIR}/SHA256SUMS.sig"
      download "$SIG_URL" "$sig"
      verify_signature "$sums" "$sig" || die "SHA256SUMS signature verification FAILED -- this release does not match the project's trusted signing key. Refusing to install a release that fails signature verification. This could mean a compromised release, a MITM, or a corrupted download; it is never safe to bypass."
      ok "release signature verified against the project's trusted key"
    else
      warn "release ${RELEASE_TAG} has no SHA256SUMS.sig -- falling back to checksum-only verification (no signature to check). See docs/installer.md#release-security."
    fi
  fi

  local expected actual
  expected="$(grep " ${ASSET_NAME}\$" "$sums" | awk '{print $1}')"
  [[ -n "$expected" ]] || die "SHA256SUMS has no entry for ${ASSET_NAME}"
  actual="$(sha256sum "$tarball" | awk '{print $1}')"
  if [[ "$expected" != "$actual" ]]; then
    die "checksum mismatch for ${ASSET_NAME}: expected ${expected}, got ${actual}. The download is corrupted or tampered with -- refusing to install it."
  fi
  ok "checksum verified (sha256:${actual:0:12}...)"
}

# hex_to_bin writes the bytes encoded by hex string $1 to file $2, one
# printf per byte so that a 0x00 byte survives (a shell *variable* holding
# raw bytes would truncate at the first NUL, but writing each byte straight
# to the file via redirection does not). Deliberately depends on nothing
# but bash + printf -- no xxd, no python, no perl -- so it works on a
# minimal base image without installing anything extra.
hex_to_bin() {
  local hex="$1" out="$2" i byte
  : >"$out"
  for ((i = 0; i < ${#hex}; i += 2)); do
    byte="${hex:i:2}"
    # The hex digits must be interpolated directly into printf's format
    # string (not passed as a %s argument) for printf's own \xHH escape
    # processing to turn them into a single byte -- see install.sh's test
    # coverage (installer/test/) for why the %s form was wrong.
    # shellcheck disable=SC2059
    printf "\\x${byte}" >>"$out"
  done
}

# verify_signature checks an Ed25519 signature (base64, over the raw file
# bytes) using only the shell's own coreutils/openssl -- no dependency on
# the Agent binary being installed yet. openssl's pkeyutl needs the public
# key in DER/PEM form, so it's derived here from the raw 32-byte Ed25519
# public key baked into this script.
verify_signature() {
  local data_file="$1" sig_file="$2"
  command -v openssl >/dev/null 2>&1 || ensure_packages openssl

  local pub_der="${WORKDIR}/release-signing-pub.der"
  local pub_pem="${WORKDIR}/release-signing-pub.pem"
  # DER prefix for an Ed25519 SubjectPublicKeyInfo (RFC 8410): fixed
  # 12-byte ASN.1 header + the 32 raw public key bytes.
  local der_prefix="302a300506032b6570032100"
  local pub_hex
  pub_hex="$(echo -n "$QRX_TRUSTED_PUBLIC_KEY_B64" | base64 -d | od -An -tx1 | tr -d ' \n')"
  [[ ${#pub_hex} -eq 64 ]] || { warn "trusted public key is not 32 bytes -- installer is misconfigured"; return 1; }
  hex_to_bin "${der_prefix}${pub_hex}" "$pub_der"
  openssl pkey -pubin -inform DER -in "$pub_der" -outform PEM -out "$pub_pem" >>"$LOG_FILE" 2>&1 \
    || return 1

  local sig_der="${WORKDIR}/SHA256SUMS.sig.der"
  base64 -d "$sig_file" >"$sig_der" 2>>"$LOG_FILE" || return 1
  openssl pkeyutl -verify -pubin -inkey "$pub_pem" -rawin -in "$data_file" -sigfile "$sig_der" >>"$LOG_FILE" 2>&1
}

# ============================================================
# 4. Install
# ============================================================
create_service_user() {
  if id -u "$QRX_SERVICE_USER" >/dev/null 2>&1; then
    verbose "service user ${QRX_SERVICE_USER} already exists"
  else
    useradd --system --no-create-home --home-dir "$QRX_DATA_DIR" --shell /usr/sbin/nologin "$QRX_SERVICE_USER"
    ok "created service user ${QRX_SERVICE_USER} (system account, no login shell, no password)"
  fi
}

install_release() {
  local tarball="${WORKDIR}/${ASSET_NAME}"
  local extract_dir="${WORKDIR}/extracted"
  mkdir -p "$extract_dir"
  tar -xzf "$tarball" -C "$extract_dir"

  [[ -f "${extract_dir}/agentd" ]] || die "release package is missing the agentd binary -- this looks like a broken/incomplete release artifact."

  install -d -m 0755 "${QRX_PREFIX}/bin"
  install -m 0755 "${extract_dir}/agentd" "${QRX_PREFIX}/bin/agentd"

  if [[ -d "${extract_dir}/dashboard" ]]; then
    rm -rf "${QRX_PREFIX}/dashboard"
    mkdir -p "${QRX_PREFIX}/dashboard"
    cp -a "${extract_dir}/dashboard/." "${QRX_PREFIX}/dashboard/"
  else
    warn "release package has no dashboard/ directory -- the web dashboard will not be available until you install one (see docs/development.md)."
  fi

  install -d -m 0750 -o "$QRX_SERVICE_USER" -g "$QRX_SERVICE_USER" "$QRX_DATA_DIR"
  install -d -m 0755 "$QRX_LOG_DIR"

  chown -R root:root "${QRX_PREFIX}"
  chmod -R a+rX "${QRX_PREFIX}"
  ok "installed to ${QRX_PREFIX} (version ${RELEASE_VERSION})"
}

# ============================================================
# 5. QRX Core detection (never invents a download source)
# ============================================================
QRX_CLI_PATH=""
QRX_CORE_STATE="not_installed" # not_installed | detected

detect_qrx_core() {
  local candidates=(
    "$(command -v qrx-cli 2>/dev/null || true)"
    "/usr/local/bin/qrx-cli"
    "/opt/qrx/current/bin/qrx-cli"
    "/opt/qrx/bin/qrx-cli"
    "/usr/bin/qrx-cli"
  )
  local c
  for c in "${candidates[@]}"; do
    if [[ -n "$c" && -x "$c" ]]; then
      QRX_CLI_PATH="$c"
      QRX_CORE_STATE="detected"
      break
    fi
  done

  if [[ "$QRX_CORE_STATE" == "detected" ]]; then
    ok "existing QRX Core detected (${QRX_CLI_PATH}) -- reusing it, nothing overwritten"
    return 0
  fi

  if [[ "$QRX_INSTALL_QRX_CORE" == "skip" ]]; then
    info "QRX Core installation skipped (QRX_INSTALL_QRX_CORE=skip)"
    return 0
  fi

  # There is deliberately no bundled QRX Core download source: this
  # installer must never invent an official QRX Core release URL (see
  # docs/installer.md#qrx-core). installer/qrx-core-sources.sh (shipped
  # inside the release tarball -- see .github/workflows/release.yml -- so
  # this works the same whether install.sh was piped or run from a local
  # checkout) is the extension point a maintainer wires up once an
  # official, verifiable binary source exists; until then this always
  # reports "unavailable" and QRX Node Suite installs successfully
  # without it.
  local sources_script="${WORKDIR}/extracted/qrx-core-sources.sh"
  if [[ -f "$sources_script" ]]; then
    # shellcheck disable=SC1090
    . "$sources_script"
    if declare -f qrx_core_official_source >/dev/null 2>&1 && qrx_core_official_source "$GOARCH" "${WORKDIR}"; then
      QRX_CORE_STATE="detected"
      QRX_CLI_PATH="${QRX_CORE_INSTALLED_CLI_PATH:-}"
      ok "installed QRX Core from the configured official source"
      return 0
    fi
  fi

  info "QRX Core was not installed because no verified, official QRX Core binary source is currently configured."
  info "QRX Node Suite installed successfully without it -- you can install or connect QRX Core later from the Dashboard."
}

# ============================================================
# 6. Configuration
# ============================================================
ADMIN_TOKEN=""

generate_admin_token() {
  # 32 random bytes, base64url-ish (no padding characters to fight with in
  # shells/URLs), generated with the kernel CSPRNG via /dev/urandom -- no
  # dependency on openssl being installed just for this.
  ADMIN_TOKEN="$(head -c 32 /dev/urandom | base64 | tr -d '=+/\n' | cut -c1-40)"
}

write_config() {
  local config_file="${QRX_CONFIG_DIR}/agent.json"
  install -d -m 0750 "$QRX_CONFIG_DIR"

  if [[ -f "$config_file" ]]; then
    info "existing config found at ${config_file} -- leaving it untouched"
    return 0
  fi

  generate_admin_token

  local listen_addr="127.0.0.1:${QRX_LISTEN_PORT}"
  if [[ "$QRX_DASHBOARD_BIND" == "lan" ]]; then
    listen_addr="0.0.0.0:${QRX_LISTEN_PORT}"
  fi

  local adapter_name="" adapter_cli_path="/usr/local/bin/qrx-cli"
  if [[ -n "$QRX_CLI_PATH" ]]; then
    adapter_cli_path="$QRX_CLI_PATH"
    # adapter_name deliberately left empty: automatic selection
    # (agent/adapters.Registry.SelectAutomatic) picks the right adapter for
    # whatever QRX Core version is actually detected at runtime, and safely
    # reports "unsupported" rather than silently using Mock if it can't --
    # see docs/updates.md#adapter-registry. Never hardcode a guess here.
  fi

  cat >"$config_file" <<JSON
{
  "config_schema_version": 1,
  "listen_addr": "${listen_addr}",
  "data_dir": "${QRX_DATA_DIR}",
  "dashboard_dir": "${QRX_PREFIX}/dashboard",
  "adapter": {
    "name": "${adapter_name}",
    "manual_override": false,
    "cli_path": "${adapter_cli_path}",
    "network": "mainnet",
    "assumed_qrx_core_version": "0.0.7"
  },
  "admin_token": "${ADMIN_TOKEN}",
  "telegram": { "enabled": false, "token": "", "chat_id": "" },
  "updates": {
    "public_key_base64": "",
    "source_kind": "github",
    "github_owner": "${QRX_REPO_OWNER}",
    "github_repo": "${QRX_REPO_NAME}",
    "retain_versions": 2,
    "max_boot_attempts": 3
  },
  "poll": { "node_status_seconds": 5, "network_seconds": 10, "system_seconds": 5, "version_hours": 6 },
  "is_validator_node": false,
  "log_format": "json",
  "log_level": "info"
}
JSON
  chown "${QRX_SERVICE_USER}:${QRX_SERVICE_USER}" "$config_file"
  chmod 0640 "$config_file"
  ok "wrote configuration to ${config_file}"
}

# ============================================================
# 7. systemd service
# ============================================================
install_systemd_service() {
  local unit_file="/etc/systemd/system/qrx-agent.service"
  cat >"$unit_file" <<UNIT
[Unit]
Description=QRX Node Suite Agent
Documentation=https://github.com/${QRX_REPO_OWNER}/${QRX_REPO_NAME}
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${QRX_SERVICE_USER}
Group=${QRX_SERVICE_USER}
ExecStart=${QRX_PREFIX}/bin/agentd -config ${QRX_CONFIG_DIR}/agent.json
WorkingDirectory=${QRX_PREFIX}
Restart=always
RestartSec=2

NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=${QRX_DATA_DIR} ${QRX_CONFIG_DIR} ${QRX_LOG_DIR}

[Install]
WantedBy=multi-user.target
UNIT

  systemctl daemon-reload
  systemctl enable qrx-agent.service >>"$LOG_FILE" 2>&1
  systemctl restart qrx-agent.service
  ok "systemd service installed, enabled, and started"
}

install_helper_scripts() {
  install -d -m 0755 "${QRX_PREFIX}/bin"
  # uninstall.sh ships inside the release tarball itself (not read from a
  # local repo checkout) so this works identically whether install.sh was
  # piped straight from GitHub or run from a cloned repo -- see
  # .github/workflows/release.yml's packaging step.
  local extracted_uninstall="${WORKDIR}/extracted/uninstall.sh"
  if [[ -f "$extracted_uninstall" ]]; then
    install -m 0755 "$extracted_uninstall" "${QRX_PREFIX}/bin/uninstall.sh"
  else
    warn "release package has no uninstall.sh -- 'qrx-node-suite uninstall' will not work until you upgrade."
  fi
  cat >/usr/local/bin/qrx-node-suite <<WRAP
#!/usr/bin/env bash
# Thin convenience wrapper around QRX Node Suite's installed helpers.
set -euo pipefail
case "\${1:-}" in
  uninstall) shift; exec "${QRX_PREFIX}/bin/uninstall.sh" "\$@" ;;
  status) exec systemctl status qrx-agent.service ;;
  logs) exec journalctl -u qrx-agent.service -f ;;
  *)
    echo "usage: qrx-node-suite {status|logs|uninstall}" >&2
    exit 2
    ;;
esac
WRAP
  chmod 0755 /usr/local/bin/qrx-node-suite
}

# ============================================================
# 8. Health checks
# ============================================================
wait_for_health() {
  local url="http://127.0.0.1:${QRX_LISTEN_PORT}/health"
  local tries=30
  local i
  for ((i = 1; i <= tries; i++)); do
    if curl -fsS -m 2 "$url" >/dev/null 2>&1; then
      ok "Agent health check passed"
      return 0
    fi
    sleep 1
  done
  die "Agent did not become healthy within ${tries}s. Check: systemctl status qrx-agent.service && journalctl -u qrx-agent.service -e"
}

fetch_version_info() {
  curl -fsS -m 5 "http://127.0.0.1:${QRX_LISTEN_PORT}/api/v1/version" 2>/dev/null || echo "{}"
}

# ============================================================
# Main
# ============================================================
main() {
  echo "${COLOR_BOLD}QRX Node Suite Installer${COLOR_RESET}"
  echo "Run. Monitor. Validate."

  require_root
  install_traps
  setup_logging

  WORKDIR="$(mktemp -d /tmp/qrx-node-suite-install.XXXXXX)"

  step "Detecting system"
  detect_arch
  detect_os
  detect_ip
  print_system_summary

  step "Checking prerequisites"
  preflight

  # Idempotency: an existing install just gets its own OTA system to
  # handle upgrades rather than this bootstrap script re-doing that work
  # (design brief section 29) -- see docs/installer.md#upgrades.
  if systemctl list-unit-files 2>/dev/null | grep -q '^qrx-agent\.service'; then
    warn "QRX Node Suite already appears to be installed (qrx-agent.service exists)."
    info "Re-running this installer will not touch your existing configuration or database."
    info "To upgrade, use the Dashboard's Settings -> Updates page, or POST /api/v1/updates/install (see docs/updates.md)."
  fi

  step "Finding the ${QRX_CHANNEL} release"
  discover_release
  info "target: ${ASSET_NAME:-$QRX_LOCAL_TARBALL} (${RELEASE_VERSION})"

  step "Downloading and verifying"
  verify_release

  step "Installing QRX Node Suite"
  create_service_user
  install_release
  install_helper_scripts

  step "Detecting QRX Core"
  detect_qrx_core

  step "Configuring and starting services"
  write_config
  install_systemd_service

  step "Running health checks"
  wait_for_health

  local version_json
  version_json="$(fetch_version_info)"
  local reported_qrx_version reported_adapter
  reported_qrx_version="$(echo "$version_json" | jq -r '.qrx_core_version // "unknown"' 2>/dev/null || echo unknown)"
  reported_adapter="$(echo "$version_json" | jq -r '.adapter_name // ""' 2>/dev/null || echo "")"

  print_summary "$reported_qrx_version" "$reported_adapter"
}

print_summary() {
  local reported_qrx_version="$1" reported_adapter="$2"
  local dashboard_local="http://127.0.0.1:${QRX_LISTEN_PORT}"
  local dashboard_lan=""
  [[ -n "$PRIMARY_IP" ]] && dashboard_lan="http://${PRIMARY_IP}:${QRX_LISTEN_PORT}"

  echo ""
  echo "================================================"
  echo ""
  echo "${COLOR_BOLD}${COLOR_GREEN}QRX Node Suite installed successfully${COLOR_RESET}"
  echo ""
  echo "Dashboard"
  echo ""
  if [[ "$QRX_DASHBOARD_BIND" == "lan" && -n "$dashboard_lan" ]]; then
    echo "  ${dashboard_lan}"
  fi
  echo "  ${dashboard_local}"
  echo ""
  if [[ -n "$ADMIN_TOKEN" ]]; then
    echo "Admin token (keep this secret -- it grants access to administrative"
    echo "endpoints: updates, rollbacks, service restarts):"
    echo ""
    echo "  ${ADMIN_TOKEN}"
    echo ""
    echo "  It is stored in ${QRX_CONFIG_DIR}/agent.json (root-readable only) and"
    echo "  will not be shown again by this installer."
  fi
  echo ""
  echo "Status"
  echo ""
  printf "  %-18s%s\n" "Agent" "RUNNING"
  if [[ -d "${QRX_PREFIX}/dashboard" ]]; then
    printf "  %-18s%s\n" "Dashboard" "RUNNING"
  else
    printf "  %-18s%s\n" "Dashboard" "NOT INSTALLED"
  fi
  if [[ "$QRX_CORE_STATE" == "detected" && "$reported_qrx_version" != "unknown" && "$reported_qrx_version" != "" ]]; then
    printf "  %-18s%s\n" "QRX Core" "CONNECTED"
    printf "  %-18s%s\n" "QRX Version" "$reported_qrx_version"
    printf "  %-18s%s\n" "Adapter" "${reported_adapter:-unknown}"
    printf "  %-18s%s\n" "Network" "mainnet"
  else
    printf "  %-18s%s\n" "QRX Core" "NOT INSTALLED"
    printf "  %-18s%s\n" "Adapter" "WAITING"
    echo ""
    echo "  Open the Dashboard to complete QRX Core setup."
  fi
  echo ""
  if [[ "$QRX_DASHBOARD_BIND" != "lan" ]]; then
    echo "The Dashboard is only reachable from this machine right now. To allow"
    echo "access from your local network, edit ${QRX_CONFIG_DIR}/agent.json's"
    echo "\"listen_addr\" to \"0.0.0.0:${QRX_LISTEN_PORT}\" and run:"
    echo "  systemctl restart qrx-agent"
    echo ""
  fi
  echo "QRX Node Suite will start automatically after reboot."
  echo "Manage it with: qrx-node-suite {status|logs|uninstall}"
  echo ""
  echo "================================================"
  log "install completed successfully in $(( $(date +%s) - INSTALL_START_EPOCH ))s"
}

# Only auto-run when actually executed (`bash install.sh`, `./install.sh`,
# or piped as `curl ... | sudo bash`) -- never when sourced, which is how
# installer/test/*.sh load these functions to unit-test them individually
# without running a real install. `(return 0 2>/dev/null)` only succeeds
# inside a sourced context; a plain `[[ "$0" == "${BASH_SOURCE[0]}" ]]`
# check looks similar but is wrong here, because piping this script into
# `bash` (no script file at all) leaves BASH_SOURCE[0] unset even though
# it must still run as the main script.
if ! (return 0 2>/dev/null); then
  main "$@"
fi
