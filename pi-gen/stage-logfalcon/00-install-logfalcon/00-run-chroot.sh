#!/bin/bash -e
# Install logfalcon into the image

INSTALL_DIR="/opt/logfalcon"
CONFIG_DIR="/etc/logfalcon"
LOG_DIR="/mnt/logfalcon-logs"
REPO_DIR="/tmp/logfalcon-src"

# Create bbsyncer system user (legacy name kept for compatibility)
if ! id bbsyncer &>/dev/null; then
    useradd --system --no-create-home --shell /sbin/nologin \
        --groups dialout bbsyncer 2>/dev/null || \
    useradd --system --no-create-home --shell /sbin/nologin bbsyncer
fi

# Add to gpio group if it exists
usermod -a -G gpio bbsyncer 2>/dev/null || true

# Install Go binary
mkdir -p "$INSTALL_DIR"

# Detect architecture and copy appropriate binary
ARCH=$(uname -m)
case "$ARCH" in
    armv6l|armv7l) BINARY_NAME="logfalcon-arm6" ;;
    aarch64)       BINARY_NAME="logfalcon-arm64" ;;
    *)             BINARY_NAME="logfalcon" ;;
esac

if [ -f "$REPO_DIR/bin/$BINARY_NAME" ]; then
    install -m 755 "$REPO_DIR/bin/$BINARY_NAME" "$INSTALL_DIR/logfalcon"
elif [ -f "$REPO_DIR/logfalcon" ]; then
    install -m 755 "$REPO_DIR/logfalcon" "$INSTALL_DIR/logfalcon"
else
    echo "ERROR: No logfalcon binary found in $REPO_DIR"
    exit 1
fi

# Config
mkdir -p "$CONFIG_DIR"
cp "$REPO_DIR/config/logfalcon.toml" "$CONFIG_DIR/logfalcon.toml"

# Log storage
mkdir -p "$LOG_DIR"
chown bbsyncer:bbsyncer "$LOG_DIR"
chmod 755 "$LOG_DIR"

# Config-apply script (replaces firstboot.sh — runs every boot, idempotent)
cp "$REPO_DIR/system/config-apply.sh" "$INSTALL_DIR/config-apply.sh"
chmod +x "$INSTALL_DIR/config-apply.sh"

# State directory for config-apply hash tracking
mkdir -p /var/lib/logfalcon

# Ownership
chown -R bbsyncer:bbsyncer "$INSTALL_DIR" "$CONFIG_DIR"

# ── Disable conflicting services ────────────────────────────────────────────
# This is an AP-only appliance. Several services fight hostapd for wlan0.
# Strategy: mask everything that manages Wi-Fi in client mode.
# Masking (symlink → /dev/null) beats any later `systemctl enable` call
# and survives pi-gen's export-image stage which can reinstall packages.

# wpa_supplicant — Wi-Fi client supplicant, fights hostapd for wlan0
rm -f /etc/systemd/system/multi-user.target.wants/wpa_supplicant.service
rm -f /etc/systemd/system/multi-user.target.wants/wpa_supplicant@wlan0.service
rm -f /etc/systemd/system/dbus-fi.w1.wpa_supplicant1.service
ln -sf /dev/null /etc/systemd/system/wpa_supplicant.service
ln -sf /dev/null /etc/systemd/system/wpa_supplicant@wlan0.service

# NetworkManager — Raspberry Pi OS Bookworm installs NM enabled by default.
# NM takes over wlan0 in managed/client mode, completely preventing hostapd
# from starting an AP. Must be masked, not just disabled.
rm -f /etc/systemd/system/multi-user.target.wants/NetworkManager.service
rm -f /etc/systemd/system/network-online.target.wants/NetworkManager-wait-online.service
rm -f /etc/systemd/system/multi-user.target.wants/ModemManager.service
rm -f /etc/systemd/system/dbus-org.freedesktop.ModemManager1.service
ln -sf /dev/null /etc/systemd/system/NetworkManager.service
ln -sf /dev/null /etc/systemd/system/NetworkManager-wait-online.service
ln -sf /dev/null /etc/systemd/system/ModemManager.service

# userconfig.service — Bookworm first-boot credential setup.
# This service reads /boot/firmware/userconf.txt and applies the username/password.
# We DO want it to run — our 00-run.sh creates userconf.txt with the hashed
# 'logfalcon' password. When the file exists, the service applies it silently
# with no interactive prompt. The wizard only appears if userconf.txt is MISSING.
# Do NOT mask this service: masking it prevents the password from ever being set,
# locking the pi account and making SSH impossible.
#
# The export-image stage re-enables this via its userconf-pi package install,
# which is fine — we want it to run exactly once on first boot.
# (No action needed here — just let it run.)

# Cleanup
rm -rf "$REPO_DIR"
