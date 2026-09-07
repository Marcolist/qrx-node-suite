#!/usr/bin/env bash
# QRX Core official binary source(s) -- extension point for install.sh.
#
# install.sh's detect_qrx_core() sources this file (if present) and, when it
# hasn't already found an existing QRX Core install, calls
# qrx_core_official_source() to ask whether one can be installed
# automatically. As shipped, this file intentionally defines no such
# function -- there is currently no verified, official, checksummed QRX
# Core binary release this project can point to, and install.sh must NEVER
# invent or guess a download URL for something as security-sensitive as a
# blockchain node binary (see docs/installer.md#qrx-core).
#
# When an official QRX Core release process exists (a real GitHub release,
# with checksums the maintainers control), a maintainer wires it up here:
#
#   qrx_core_official_source() {
#     local goarch="$1" workdir="$2"
#     local url="https://.../qrxd-linux-${goarch}.tar.gz"   # a REAL URL, not a placeholder
#     local sha256="..."                                     # published by the QRX Core project
#     curl -fsSL -o "${workdir}/qrxd.tar.gz" "$url" || return 1
#     echo "${sha256}  ${workdir}/qrxd.tar.gz" | sha256sum -c - || return 1
#     tar -xzf "${workdir}/qrxd.tar.gz" -C /opt/qrx/versions/<version>/
#     ln -sfn /opt/qrx/versions/<version> /opt/qrx/current
#     QRX_CORE_INSTALLED_CLI_PATH="/opt/qrx/current/bin/qrx-cli"
#     # Record that THIS installer put QRX Core here, so uninstall.sh's
#     # --remove-qrx-core can find it later -- write it under
#     # QRX_CONFIG_DIR (root:qrx-agent, root-only-writable), NEVER
#     # QRX_DATA_DIR: uninstall.sh trusts this file's content as an
#     # `rm -rf` target run as root, and QRX_DATA_DIR is writable by the
#     # unprivileged qrx-agent service user (see the F05 fix in
#     # installer/linux/uninstall.sh and docs/installer.md#qrx-core-removal-safety).
#     echo "/opt/qrx/versions/<version>" >"${QRX_CONFIG_DIR}/qrx-core-installed-by-this-installer"
#     return 0
#   }
#
# Return 0 only after QRX Core is actually installed and verified; return
# 1 (or leave the function undefined, as here) to make install.sh fall back
# to its documented "installed without QRX Core, connect it later from the
# Dashboard" outcome -- never a partial or unverified install.
