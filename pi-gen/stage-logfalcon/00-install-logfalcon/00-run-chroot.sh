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

# Create venv and install
mkdir -p "$INSTALL_DIR"
python3 -m venv "$INSTALL_DIR/venv"
"$INSTALL_DIR/venv/bin/pip" install --quiet --upgrade pip setuptools wheel
"$INSTALL_DIR/venv/bin/pip" install --quiet "$REPO_DIR"

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

# Cleanup
rm -rf "$REPO_DIR"
