#!/bin/bash -e
# Disable Bookworm first-boot user config wizard — user is pre-configured by pi-gen
# (FIRST_USER_NAME/FIRST_USER_PASS in pi-gen/config). Belt-and-suspenders: also
# create userconf.txt in the boot partition, which is the official Bookworm headless
# mechanism and prevents userconf-pi from prompting interactively on first boot.
#
# This file runs on the HOST (not in chroot) so ROOTFS_DIR is available.

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
mkdir -p "${ROOTFS_DIR}/opt/logfalcon"
# Copy source (exclude .git, pi-gen, etc.)
rsync -a --exclude='.git' --exclude='pi-gen' \
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

# --- Bookworm headless: pre-create userconf.txt so userconf-pi skips the wizard ---
# This is the official Bookworm mechanism for headless user setup.
# Without this, the Pi shows an interactive "enter username" prompt on first boot.
BOOT_DIR="${ROOTFS_DIR}/boot/firmware"
[ -d "$BOOT_DIR" ] || BOOT_DIR="${ROOTFS_DIR}/boot"
HASHED=$(openssl passwd -6 'logfalcon')
echo "pi:${HASHED}" > "${BOOT_DIR}/userconf.txt"
echo "[logfalcon] Created ${BOOT_DIR}/userconf.txt for headless first boot"

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

# NOTE: USB gadget SSH (g_ether) is NOT enabled by default.
# The Pi Zero 2 W has one OTG port. Loading g_ether puts it in USB device
# mode, which prevents the Pi from enumerating FC devices plugged into it.
# FC detection (the primary function of this device) requires host mode.
#
# SSH via Wi-Fi hotspot (ssh pi@192.168.4.1) is the standard access method.
#
# To enable USB gadget SSH for debugging, add these lines to config.txt:
#   dtoverlay=dwc2
# And append to cmdline.txt (same line, before rootwait):
#   modules-load=dwc2,g_ether
# Then set your host IP to 10.55.55.2 and ssh pi@10.55.55.1.
echo "[logfalcon] USB gadget SSH intentionally disabled — OTG port in host mode for FC detection"
