# Minimal systemd-capable image for install.sh's Docker smoke test
# (.github/workflows/installer-ci.yml), built FROM the official base image
# for each supported distro -- no third-party base images, only
# `apt-get install systemd` on top of Ubuntu/Debian's own official images.
ARG BASE_IMAGE=ubuntu:22.04
FROM ${BASE_IMAGE}

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    systemd systemd-sysv ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    # Standard trim for a container-run systemd: disable units that assume
    # real hardware/console and just add noise/slowness inside a container.
    && rm -f /lib/systemd/system/multi-user.target.wants/* \
    && rm -f /etc/systemd/system/*.wants/* \
    && rm -f /lib/systemd/system/local-fs.target.wants/* \
    && rm -f /lib/systemd/system/sockets.target.wants/*udev* \
    && rm -f /lib/systemd/system/sockets.target.wants/*initctl* \
    && rm -f /lib/systemd/system/sysinit.target.wants/systemd-tmpfiles-setup* \
    && rm -f /lib/systemd/system/systemd-update-utmp*

STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
