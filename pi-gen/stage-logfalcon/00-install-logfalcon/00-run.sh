#!/bin/bash -e
# Copy source code into chroot for installation
# BASE_DIR is exported by pi-gen's build.sh and points to the pi-gen root.
# The CI workflow and local build.sh copy the project source to logfalcon-src/.
REPO_ROOT="${BASE_DIR}/logfalcon-src"

if [ ! -d "${REPO_ROOT}" ]; then
    echo "ERROR: logfalcon source not found at ${REPO_ROOT}"
    echo "Ensure the project source is copied into the pi-gen directory before building."
    exit 1
fi

mkdir -p "${ROOTFS_DIR}/tmp/logfalcon-src"
# Copy source (exclude .git, .venv, etc.)
rsync -a --exclude='.git' --exclude='.venv' --exclude='__pycache__' \
    --exclude='*.egg-info' --exclude='.pytest_cache' --exclude='pi-gen' \
    "${REPO_ROOT}/" "${ROOTFS_DIR}/tmp/logfalcon-src/"

# Copy systemd units
install -m 644 "${REPO_ROOT}/system/logfalcon@.service" "${ROOTFS_DIR}/etc/systemd/system/"
install -m 644 "${REPO_ROOT}/system/logfalcon-web.service" "${ROOTFS_DIR}/etc/systemd/system/"
install -m 644 "${REPO_ROOT}/system/logfalcon-firstboot.service" "${ROOTFS_DIR}/etc/systemd/system/"
install -m 644 "${REPO_ROOT}/system/logfalcon-boot-led.service" "${ROOTFS_DIR}/etc/systemd/system/"
install -m 644 "${REPO_ROOT}/system/logfalcon-ready-led.service" "${ROOTFS_DIR}/etc/systemd/system/"

# Copy boot LED heartbeat script
install -m 755 "${REPO_ROOT}/system/logfalcon-boot-led.sh" "${ROOTFS_DIR}/opt/logfalcon/boot-led.sh"

# Copy ready LED script
install -m 755 "${REPO_ROOT}/system/logfalcon-ready-led.sh" "${ROOTFS_DIR}/opt/logfalcon/ready-led.sh"

# Copy udev rule
install -m 644 "${REPO_ROOT}/system/99-betaflight-fc.rules" "${ROOTFS_DIR}/etc/udev/rules.d/"

# Copy boot config
install -m 644 "${REPO_ROOT}/boot/logfalcon-config.txt" "${ROOTFS_DIR}/boot/firmware/" 2>/dev/null || \
install -m 644 "${REPO_ROOT}/boot/logfalcon-config.txt" "${ROOTFS_DIR}/boot/"

# Enable services in chroot
on_chroot << CHEOF
systemctl enable logfalcon-web.service
systemctl enable logfalcon-firstboot.service
systemctl enable logfalcon-boot-led.service
systemctl enable logfalcon-ready-led.service
systemctl enable hostapd
systemctl enable dnsmasq
systemctl enable avahi-daemon
CHEOF
