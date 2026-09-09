#!/usr/bin/env bash
# Install QRX Core bundled inside the already verified Node Suite release.

qrx_core_random_hex() {
  od -An -N32 -tx1 /dev/urandom | tr -d ' \n'
}

qrx_core_port_for_network() {
  case "$1" in
    mainnet) echo 26660 ;;
    alpha) echo 26661 ;;
    testnet) echo 26662 ;;
    regtest) echo 26663 ;;
    *) return 1 ;;
  esac
}

qrx_core_rpc_port_for_network() {
  case "$1" in
    mainnet) echo 37660 ;;
    alpha) echo 37661 ;;
    testnet) echo 37662 ;;
    regtest) echo 37663 ;;
    *) return 1 ;;
  esac
}

qrx_core_write_unit() {
  local binary_dir="$1" network="$2" p2p_port="$3" rpc_port="$4" seed_arg=""
  [[ "$network" == "alpha" ]] && seed_arg=" --seednode seed1.qrxchain.org:${p2p_port}"
  cat >/etc/systemd/system/qrxd.service <<UNIT
[Unit]
Description=QRX Core 0.0.7 node
Documentation=https://github.com/phoenixkonsole/qrx/tree/0.0.7
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
Type=simple
User=qrx-core
Group=qrx-core
UMask=0077
EnvironmentFile=/etc/qrx-core/rpc.env
EnvironmentFile=/etc/qrx-core/wallet.env
ExecStartPre=/usr/bin/test -f /var/lib/qrx/wallets/node/address.txt
ExecStart=${binary_dir}/qrxd --network ${network} --datadir /var/lib/qrx --wallet node --listen 0.0.0.0:${p2p_port} --rpc-bind 127.0.0.1:${rpc_port}${seed_arg}
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
ProtectClock=true
RestrictSUIDSGID=true
LockPersonality=true
MemoryDenyWriteExecute=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
ReadWritePaths=/var/lib/qrx

[Install]
WantedBy=multi-user.target
UNIT
}

qrx_core_initialize_wallet() {
  local binary_dir="$1" network="$2" p2p_port="$3" rpc_port="$4"
  local wallet_address="/var/lib/qrx/wallets/node/address.txt"
  [[ -f "$wallet_address" ]] && return 0
  local init_log="${WORKDIR}/qrx-core-initialize.log"
  chmod 0700 "$WORKDIR"
  : >"$init_log"
  chmod 0600 "$init_log"
  local seed_args=()
  [[ "$network" == "alpha" ]] && seed_args=(--seednode "seed1.qrxchain.org:${p2p_port}")
  local init_status=0
  (
    # Do not pass credentials as argv, and do not leak unrelated root
    # environment variables into the unprivileged initialization process.
    local env_name
    for env_name in $(compgen -e); do unset "$env_name"; done
    export HOME=/var/lib/qrx PATH=/usr/sbin:/usr/bin:/sbin:/bin
    set -a
    # shellcheck disable=SC1091
    . /etc/qrx-core/rpc.env
    # shellcheck disable=SC1091
    . /etc/qrx-core/wallet.env
    set +a
    exec /usr/bin/timeout --signal=TERM --kill-after=3s 5s \
      /usr/sbin/runuser -u qrx-core --preserve-environment -- \
      "${binary_dir}/qrxd" --network "$network" --datadir /var/lib/qrx \
        --wallet node --listen "0.0.0.0:${p2p_port}" --rpc-bind "127.0.0.1:${rpc_port}" \
        --no-block-producer "${seed_args[@]}"
  ) >"$init_log" 2>&1 || init_status=$?
  if [[ "$init_status" != 0 && "$init_status" != 124 && "$init_status" != 143 ]] || [[ ! -f "$wallet_address" ]]; then
    sed '/recovery_phrase=/d' "$init_log" >&"${LOG_FD:-2}" || true
    return 1
  fi
  local phrase
  phrase="$(sed -n 's/^recovery_phrase=//p' "$init_log" | tail -n 1)"
  [[ -n "$phrase" ]] || {
    echo "QRX Core created a wallet but did not return its recovery phrase; refusing an unrecoverable installation" >&2
    return 1
  }
  {
    echo "QRX Core wallet recovery phrase"
    echo "Network: ${network}"
    echo "Wallet: node"
    echo "Address: $(cat "$wallet_address")"
    echo "Recovery phrase: ${phrase}"
    echo "Keep this file together with recovery.qrxseed, then delete both server-side copies."
  } >/etc/qrx-core/recovery.txt
  install -o root -g root -m 0600 /var/lib/qrx/wallets/node/recovery.qrxseed /etc/qrx-core/recovery.qrxseed
  chmod 0600 /etc/qrx-core/recovery.txt
  : >"$init_log"
  # Read by the parent install.sh after this sourced helper returns.
  # shellcheck disable=SC2034
  QRX_CORE_RECOVERY_CREATED=1
}

qrx_core_validate_bundle() {
  local bundled="$1"
  local expected_version="0.0.7"
  local expected_commit="4a732c1a7d2b03fb299eabde437646c99c797e2d"
  local expected_openssl="3.6.4"
  local expected_openssl_sha256="9bffaa1ad1e07b354c21bd3324ec02fa15579f45a7d0494b3e74bc449b7333ef"
  local expected_patch_sha256="80b600d5a8aba915a262a9a658539c3d64ce660fb85e341eb3ea773ad618aa4f"
  [[ -d "${bundled}/bin" ]] || return 1
  local metadata
  for metadata in VERSION SOURCE_COMMIT OPENSSL_VERSION OPENSSL_SOURCE_SHA256 SUITE_PATCH_SHA256 LICENSE.qrx-core; do
    [[ -f "${bundled}/${metadata}" && ! -L "${bundled}/${metadata}" ]] || return 1
  done
  [[ "$(tr -d '[:space:]' <"${bundled}/VERSION")" == "$expected_version" ]] || return 1
  [[ "$(tr -d '[:space:]' <"${bundled}/SOURCE_COMMIT")" == "$expected_commit" ]] || return 1
  [[ "$(tr -d '[:space:]' <"${bundled}/OPENSSL_VERSION")" == "$expected_openssl" ]] || return 1
  [[ "$(tr -d '[:space:]' <"${bundled}/OPENSSL_SOURCE_SHA256")" == "$expected_openssl_sha256" ]] || return 1
  [[ "$(tr -d '[:space:]' <"${bundled}/SUITE_PATCH_SHA256")" == "$expected_patch_sha256" ]] || return 1
  local binary
  for binary in qrx qrxd qrx-cli qrxdb_verify qrxdb_salvage qrxdb_compact qrxdb_snapshot; do
    [[ -f "${bundled}/bin/${binary}" && -x "${bundled}/bin/${binary}" && ! -L "${bundled}/bin/${binary}" ]] || return 1
  done
  return 0
}

qrx_core_official_source() {
  local _goarch="$1" _workdir="$2"
  local bundled="${WORKDIR}/extracted/core"
  qrx_core_validate_bundle "$bundled" || return 1

  local expected_version="0.0.7"
  local expected_commit="4a732c1a7d2b03fb299eabde437646c99c797e2d"
  local patch_id="80b600d5a8a"

  local network="${QRX_CORE_NETWORK:-alpha}" p2p_port rpc_port
  p2p_port="$(qrx_core_port_for_network "$network")" || { echo "unsupported QRX_CORE_NETWORK: ${network}" >&2; return 1; }
  rpc_port="$(qrx_core_rpc_port_for_network "$network")"
  if ! id -u qrx-core >/dev/null 2>&1; then
    useradd --system --home-dir /var/lib/qrx --shell /usr/sbin/nologin qrx-core
  fi
  install -d -o root -g root -m 0755 /opt/qrx /opt/qrx/versions
  install -d -o qrx-core -g qrx-core -m 0700 /var/lib/qrx
  install -d -o root -g root -m 0700 /etc/qrx-core

  local version_dir="/opt/qrx/versions/${expected_version}-${expected_commit:0:12}-${patch_id}"
  install -d -o root -g root -m 0755 "$version_dir/bin"
  for binary in qrx qrxd qrx-cli qrxdb_verify qrxdb_salvage qrxdb_compact qrxdb_snapshot; do
    install -o root -g root -m 0755 "${bundled}/bin/${binary}" "${version_dir}/bin/${binary}"
  done
  install -o root -g root -m 0644 "${bundled}/LICENSE.qrx-core" "${version_dir}/LICENSE"
  install -o root -g root -m 0644 "${bundled}/VERSION" "${version_dir}/VERSION"
  install -o root -g root -m 0644 "${bundled}/SOURCE_COMMIT" "${version_dir}/SOURCE_COMMIT"
  install -o root -g root -m 0644 "${bundled}/SUITE_PATCH_SHA256" "${version_dir}/SUITE_PATCH_SHA256"
  ln -sfn "$version_dir" /opt/qrx/current
  for binary in qrx qrxd qrx-cli; do ln -sfn "/opt/qrx/current/bin/${binary}" "/usr/local/bin/${binary}"; done

  if [[ ! -f /etc/qrx-core/rpc.env ]]; then
    { echo "QRX_RPC_USER=qrx-agent"; echo "QRX_RPC_PASSWORD=$(qrx_core_random_hex)"; } >/etc/qrx-core/rpc.env
  fi
  [[ -f /etc/qrx-core/wallet.env ]] || echo "QRX_PASSPHRASE=$(qrx_core_random_hex)" >/etc/qrx-core/wallet.env
  # systemd reads EnvironmentFile as PID 1 before dropping privileges, so
  # neither service account needs filesystem access to the credential file.
  chown root:root /etc/qrx-core/rpc.env
  chmod 0600 /etc/qrx-core/rpc.env
  chown root:root /etc/qrx-core/wallet.env
  chmod 0600 /etc/qrx-core/wallet.env

  systemctl stop qrxd.service >/dev/null 2>&1 || true
  qrx_core_write_unit "$version_dir/bin" "$network" "$p2p_port" "$rpc_port"
  systemctl daemon-reload
  qrx_core_initialize_wallet "$version_dir/bin" "$network" "$p2p_port" "$rpc_port" || return 1
  local external_host="${QRX_CORE_EXTERNAL_HOST:-${PRIMARY_IP:-127.0.0.1}}"
  [[ "$external_host" =~ ^[A-Za-z0-9._:-]+$ ]] || {
    echo "invalid QRX_CORE_EXTERNAL_HOST: ${external_host}" >&2
    return 1
  }
  local node_conf="/var/lib/qrx/${network}/nodes/node/node.conf"
  [[ -f "$node_conf" ]] || return 1
  # Modify Core-owned state as the Core account, so a compromised data tree
  # cannot turn a later root-run reinstall into a privileged symlink write.
  runuser -u qrx-core -- sed -i \
    -e 's/^host=.*/host=0.0.0.0/' \
    -e "s/^external_host=.*/external_host=${external_host}/" \
    "$node_conf"
  systemctl enable --now qrxd.service >&"${LOG_FD:-2}"
  set -a
  # shellcheck disable=SC1091
  . /etc/qrx-core/rpc.env
  set +a
  local ready=0
  for _ in $(seq 1 50); do
    if "$version_dir/bin/qrx-cli" --network "$network" --datadir /var/lib/qrx --wallet node getbuildinfo >/dev/null 2>&1; then ready=1; break; fi
    sleep 0.2
  done
  [[ "$ready" == 1 ]] || { systemctl status qrxd.service --no-pager >&"${LOG_FD:-2}" || true; return 1; }

  install -d -o root -g "$QRX_SERVICE_USER" -m 0750 "$QRX_CONFIG_DIR"
  echo "$version_dir" >"${QRX_CONFIG_DIR}/qrx-core-installed-by-this-installer"
  chmod 0640 "${QRX_CONFIG_DIR}/qrx-core-installed-by-this-installer"
  chown root:"$QRX_SERVICE_USER" "${QRX_CONFIG_DIR}/qrx-core-installed-by-this-installer"
  # These are the sourced helper's return values, consumed by install.sh.
  # shellcheck disable=SC2034
  QRX_CORE_INSTALLED_CLI_PATH="/opt/qrx/current/bin/qrx-cli"
  # shellcheck disable=SC2034
  QRX_CORE_DATA_DIR="/var/lib/qrx"
  # shellcheck disable=SC2034
  QRX_CORE_NETWORK_ACTIVE="$network"
  # shellcheck disable=SC2034
  QRX_CORE_WALLET_NAME="node"
  return 0
}
