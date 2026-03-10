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

# Firstboot script
cp "$REPO_DIR/system/firstboot.sh" "$INSTALL_DIR/firstboot.sh"
chmod +x "$INSTALL_DIR/firstboot.sh"

# Ownership
chown -R bbsyncer:bbsyncer "$INSTALL_DIR" "$CONFIG_DIR"

# ── Disable conflicting services ────────────────────────────────────────────
# wpa_supplicant runs in Wi-Fi client mode and will fight hostapd for wlan0.
# We are an AP-only appliance — disable it.
# Use direct symlink removal: `systemctl disable` is unreliable in a chroot
# because there is no D-Bus / running systemd to communicate with.
rm -f /etc/systemd/system/multi-user.target.wants/wpa_supplicant.service
rm -f /etc/systemd/system/dbus-fi.w1.wpa_supplicant1.service

# Disable Bookworm first-boot user config wizard.
# userconf.txt on the boot partition (created by 00-run.sh) is the primary fix;
# removing this symlink is belt-and-suspenders. Again, direct removal is reliable
# where `systemctl disable` silently fails in chroot.
rm -f /etc/systemd/system/multi-user.target.wants/userconfig.service

# Cleanup
rm -rf "$REPO_DIR"
