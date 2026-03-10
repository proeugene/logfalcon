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

# Disable Bookworm first-boot user config wizard — user is pre-configured via
# FIRST_USER_NAME/FIRST_USER_PASS in pi-gen/config, and userconf.txt is already
# written to the boot partition by 00-run.sh. Belt-and-suspenders: disable the
# service here too so it can never trigger interactively on a fresh boot.
systemctl disable userconfig 2>/dev/null || true

# Cleanup
rm -rf "$REPO_DIR"
