#!/usr/bin/env bash
# LogFalcon — One-step installer for Raspberry Pi
# Usage: curl -sSL https://github.com/proeugene/logfalcon/raw/main/scripts/install.sh | sudo bash
#
# Installs LogFalcon on an existing Raspberry Pi running Raspberry Pi OS.
# Supports Pi Zero W (armv6l), Pi 3/4 (armv7l), and Pi Zero 2 W / Pi 4+ (aarch64).

set -euo pipefail

# --- Configuration -----------------------------------------------------------
REPO="proeugene/logfalcon"
INSTALL_DIR="/opt/logfalcon"
CONFIG_DIR="/etc/logfalcon"
LOG_DIR="/mnt/logfalcon-logs"
SERVICE_USER="bbsyncer"
HOTSPOT_IP="192.168.4.1"

# --- Colors & helpers --------------------------------------------------------
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()  { echo -e "${GREEN}[logfalcon]${NC} $*"; }
warn()  { echo -e "${YELLOW}[logfalcon]${NC} $*"; }
error() { echo -e "${RED}[logfalcon]${NC} $*" >&2; }
die()   { error "$*"; exit 1; }

# --- Preflight checks --------------------------------------------------------
[[ $EUID -eq 0 ]] || die "This script must be run as root (use sudo)."

command -v curl  >/dev/null 2>&1 || command -v wget >/dev/null 2>&1 || die "curl or wget is required."
command -v systemctl >/dev/null 2>&1 || die "systemd is required."

# --- Detect architecture -----------------------------------------------------
ARCH=$(uname -m)
case "$ARCH" in
    armv6l)          SUFFIX="arm6"  ;;
    armv7l)          SUFFIX="arm6"  ;;  # ARMv6 binary runs on v7
    aarch64|arm64)   SUFFIX="arm64" ;;
    *)               die "Unsupported architecture: $ARCH (need armv6l, armv7l, or aarch64)" ;;
esac
info "Detected architecture: $ARCH → logfalcon-$SUFFIX"

# --- Determine latest version ------------------------------------------------
VERSION="${LOGFALCON_VERSION:-}"
if [[ -z "$VERSION" ]]; then
    info "Fetching latest release..."
    if command -v curl >/dev/null 2>&1; then
        VERSION=$(curl -sSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
    else
        VERSION=$(wget -qO- "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
    fi
fi
[[ -n "$VERSION" ]] || die "Could not determine latest version. Set LOGFALCON_VERSION=vX.Y.Z manually."
info "Installing LogFalcon $VERSION"

# --- Download binary ---------------------------------------------------------
BINARY_URL="https://github.com/$REPO/releases/download/$VERSION/logfalcon-$SUFFIX"
BINARY_TMP="/tmp/logfalcon-$SUFFIX"

info "Downloading logfalcon-$SUFFIX..."
if command -v curl >/dev/null 2>&1; then
    curl -sSL -o "$BINARY_TMP" "$BINARY_URL" || die "Download failed. Check version $VERSION exists at $BINARY_URL"
else
    wget -qO "$BINARY_TMP" "$BINARY_URL" || die "Download failed. Check version $VERSION exists at $BINARY_URL"
fi
chmod +x "$BINARY_TMP"

# Quick sanity check
file "$BINARY_TMP" | grep -qi "ELF.*ARM\|ELF.*aarch64" || warn "Binary may not be a valid ARM executable."
info "Downloaded successfully."

# --- Install packages --------------------------------------------------------
info "Installing required packages..."
apt-get update -qq
apt-get install -y -qq hostapd dnsmasq avahi-daemon rfkill polkitd iw >/dev/null 2>&1
info "Packages installed."

# --- Create system user ------------------------------------------------------
if ! id "$SERVICE_USER" &>/dev/null; then
    info "Creating system user: $SERVICE_USER"
    useradd --system --no-create-home --shell /sbin/nologin \
        --groups dialout "$SERVICE_USER" 2>/dev/null || \
    useradd --system --no-create-home --shell /sbin/nologin "$SERVICE_USER"
fi
usermod -a -G gpio "$SERVICE_USER" 2>/dev/null || true

# --- Install binary and directories -----------------------------------------
info "Installing files..."
mkdir -p "$INSTALL_DIR" "$CONFIG_DIR" "$LOG_DIR"

install -m 755 "$BINARY_TMP" "$INSTALL_DIR/logfalcon"
rm -f "$BINARY_TMP"

chown -R "$SERVICE_USER":"$SERVICE_USER" "$INSTALL_DIR" "$CONFIG_DIR" "$LOG_DIR"
chmod 755 "$LOG_DIR"

# --- Install config ----------------------------------------------------------
if [[ ! -f "$CONFIG_DIR/logfalcon.toml" ]]; then
    cat > "$CONFIG_DIR/logfalcon.toml" <<'TOMLEOF'
serial_baud = 921600
serial_port = ""

storage_path = "/mnt/logfalcon-logs"
min_free_space_mb = 200
storage_pressure_cleanup = true

erase_after_sync = true
flash_chunk_size = 4096
erase_timeout_sec = 120
flash_read_compression = false

led_backend = "sysfs"
led_gpio_pin = 17

hotspot_ssid = "LogFalcon"
hotspot_password = "fpvpilot"
web_port = 80

idle_shutdown_minutes = 0
TOMLEOF
    chown "$SERVICE_USER":"$SERVICE_USER" "$CONFIG_DIR/logfalcon.toml"
    info "Created default config at $CONFIG_DIR/logfalcon.toml"
else
    info "Config already exists at $CONFIG_DIR/logfalcon.toml — keeping it."
fi

# --- Install boot config (first-boot SSID/password) -------------------------
BOOT_DIR=""
[[ -d /boot/firmware ]] && BOOT_DIR="/boot/firmware"
[[ -z "$BOOT_DIR" && -d /boot ]] && BOOT_DIR="/boot"

if [[ -n "$BOOT_DIR" && ! -f "$BOOT_DIR/logfalcon-config.txt" ]]; then
    cat > "$BOOT_DIR/logfalcon-config.txt" <<'EOF'
# LogFalcon Wi-Fi hotspot settings
# Edit these and reboot to apply.
SSID=LogFalcon
PASSWORD=fpvpilot
EOF
    info "Created $BOOT_DIR/logfalcon-config.txt (edit to change Wi-Fi name/password)."
fi

# --- Install config-apply script (boot-time SSID/password applicator) ---------
info "Installing config-apply.sh..."
cat > "$INSTALL_DIR/config-apply.sh" <<'CONFIGAPPLYEOF'
#!/usr/bin/env bash
# LogFalcon — Boot-time config applicator
# Reads /boot/firmware/logfalcon-config.txt or /boot/logfalcon-config.txt
# and updates hostapd + app config.
# Runs on every boot so users can edit config.txt and reboot to apply changes.
# Install to: /opt/logfalcon/config-apply.sh

set -euo pipefail

HOSTAPD_CONF="/etc/hostapd/hostapd.conf"
LOGFALCON_CONF="/etc/logfalcon/logfalcon.toml"
STATE_FILE="/var/lib/logfalcon/config-applied-hash"
TAG="logfalcon-config-apply"

log() { logger -t "$TAG" "$*"; }
escape_sed() { printf '%s' "$1" | sed -e 's/[\/&\\]/\\&/g'; }
toml_escape() {
    local value=$1
    value=${value//\\/\\\\}
    value=${value//\"/\\\"}
    printf '%s' "$value"
}

config_file() {
    if [[ -f /boot/firmware/logfalcon-config.txt ]]; then
        printf '%s\n' /boot/firmware/logfalcon-config.txt
    elif [[ -f /boot/logfalcon-config.txt ]]; then
        printf '%s\n' /boot/logfalcon-config.txt
    else
        printf '%s\n' /boot/firmware/logfalcon-config.txt
    fi
}

CONFIG_FILE="$(config_file)"

# Compute a hash of the current config file for change detection
config_hash() {
    if [[ -f "$CONFIG_FILE" ]]; then
        sha256sum "$CONFIG_FILE" | awk '{print $1}'
    else
        echo "none"
    fi
}

# Check if config has already been applied (no-op fast path)
APPLIED_HASH=""
if [[ -f "$STATE_FILE" ]]; then
    APPLIED_HASH="$(cat "$STATE_FILE")"
fi
CURRENT_HASH="$(config_hash)"

if [[ "$APPLIED_HASH" == "$CURRENT_HASH" ]]; then
    log "Config unchanged (hash=$CURRENT_HASH) — skipping."
    exit 0
fi

# ── Parse config file ───────────────────────────────────────────────────────
if [[ ! -f "$CONFIG_FILE" ]]; then
    log "No config file at $CONFIG_FILE — skipping."
    exit 0
fi

if [[ ! -r "$CONFIG_FILE" ]]; then
    log "Cannot read $CONFIG_FILE — skipping."
    exit 0
fi

SSID=""
PASSWORD=""

while IFS= read -r line || [[ -n "$line" ]]; do
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue
    case "$line" in
        SSID=*)     SSID="${line#SSID=}" ;;
        PASSWORD=*) PASSWORD="${line#PASSWORD=}" ;;
    esac
done < "$CONFIG_FILE"

# Trim leading/trailing whitespace
SSID="$(echo "$SSID" | xargs)"
PASSWORD="$(echo "$PASSWORD" | xargs)"

if [[ -z "$SSID" || -z "$PASSWORD" ]]; then
    log "SSID or PASSWORD not found in $CONFIG_FILE — skipping."
    exit 0
fi

# Validate SSID: 1-32 characters
if [[ ${#SSID} -lt 1 || ${#SSID} -gt 32 ]]; then
    log "ERROR: SSID must be 1-32 characters (got ${#SSID}) — skipping."
    exit 0
fi

# Validate PASSWORD: 8-63 characters (WPA2 requirement)
if [[ ${#PASSWORD} -lt 8 || ${#PASSWORD} -gt 63 ]]; then
    log "ERROR: PASSWORD must be 8-63 characters (got ${#PASSWORD}) — skipping."
    exit 0
fi

if printf '%s' "$SSID$PASSWORD" | LC_ALL=C grep -q '[^[:print:]]'; then
    log "ERROR: SSID or PASSWORD contained non-printable characters — skipping."
    exit 0
fi

SSID_ESCAPED="$(escape_sed "$SSID")"
PASSWORD_ESCAPED="$(escape_sed "$PASSWORD")"
SSID_TOML_ESCAPED="$(toml_escape "$SSID")"
PASSWORD_TOML_ESCAPED="$(toml_escape "$PASSWORD")"
SSID_TOML_SED="$(escape_sed "$SSID_TOML_ESCAPED")"
PASSWORD_TOML_SED="$(escape_sed "$PASSWORD_TOML_ESCAPED")"

log "Applying config change: SSID='$SSID'"

# ── Update hostapd.conf ─────────────────────────────────────────────────────
if [[ -f "$HOSTAPD_CONF" ]]; then
    sed -i "s/^ssid=.*/ssid=$SSID_ESCAPED/" "$HOSTAPD_CONF"
    sed -i "s/^wpa_passphrase=.*/wpa_passphrase=$PASSWORD_ESCAPED/" "$HOSTAPD_CONF"
    log "Updated $HOSTAPD_CONF"
else
    log "WARNING: $HOSTAPD_CONF not found — skipped hostapd update."
fi

# ── Update logfalcon.toml ───────────────────────────────────────────────────
if [[ -f "$LOGFALCON_CONF" ]]; then
    sed -i "s/^hotspot_ssid = .*/hotspot_ssid = \"$SSID_TOML_SED\"/" "$LOGFALCON_CONF"
    sed -i "s/^hotspot_password = .*/hotspot_password = \"$PASSWORD_TOML_SED\"/" "$LOGFALCON_CONF"
    log "Updated $LOGFALCON_CONF"
else
    log "WARNING: $LOGFALCON_CONF not found — skipped logfalcon config update."
fi

# ── Record applied hash for next-boot comparison ────────────────────────────
mkdir -p "$(dirname "$STATE_FILE")"
echo "$CURRENT_HASH" > "$STATE_FILE"

log "Config applied successfully (hash=$CURRENT_HASH)."

# ── Restart dependent services to pick up new config ────────────────────────
# Only restart if the services are running (avoid starting them if they failed)
if systemctl is-active --quiet hostapd; then
    systemctl reload hostapd 2>/dev/null || systemctl restart hostapd
    log "Restarted hostapd to apply new SSID/passphrase."
fi
CONFIGAPPLYEOF
chmod +x "$INSTALL_DIR/config-apply.sh"
mkdir -p /var/lib/logfalcon

# --- Install LED scripts -----------------------------------------------------
cat > "$INSTALL_DIR/boot-led.sh" <<'SCRIPTEOF'
#!/bin/sh
LED="/sys/class/leds/led0"
echo none > "$LED/trigger" 2>/dev/null
cleanup() { echo 0 > "$LED/brightness" 2>/dev/null; echo mmc0 > "$LED/trigger" 2>/dev/null; exit 0; }
trap cleanup TERM INT
while true; do echo 1 > "$LED/brightness"; sleep 1; echo 0 > "$LED/brightness"; sleep 1; done
SCRIPTEOF

cat > "$INSTALL_DIR/ready-led.sh" <<'SCRIPTEOF'
#!/bin/sh
LED="/sys/class/leds/led0"
echo none > "$LED/trigger" 2>/dev/null
cleanup() { echo 0 > "$LED/brightness" 2>/dev/null; exit 0; }
trap cleanup TERM INT
echo 1 > "$LED/brightness" 2>/dev/null
sleep infinity & wait
SCRIPTEOF

chmod +x "$INSTALL_DIR/boot-led.sh" "$INSTALL_DIR/ready-led.sh"
chown -R "$SERVICE_USER":"$SERVICE_USER" "$INSTALL_DIR"

# --- Install udev rule -------------------------------------------------------
cat > /etc/udev/rules.d/99-betaflight-fc.rules <<'EOF'
# LogFalcon: auto-sync when Betaflight FC is plugged in via USB
SUBSYSTEM=="tty", ATTRS{idVendor}=="0483", ATTRS{idProduct}!="df11", KERNEL=="ttyACM*", TAG+="systemd", ENV{SYSTEMD_WANTS}="logfalcon@%k.service"
EOF
udevadm control --reload-rules 2>/dev/null || true

# --- Install polkit rule (allow bbsyncer to restart hostapd) -----------------
mkdir -p /etc/polkit-1/rules.d
cat > /etc/polkit-1/rules.d/50-logfalcon-hostapd.rules <<'EOF'
polkit.addRule(function(action, subject) {
    if (action.id == "org.freedesktop.systemd1.manage-units" &&
        action.lookup("unit") == "hostapd.service" &&
        subject.user == "bbsyncer") {
        return polkit.Result.YES;
    }
});
EOF

# --- Install systemd units ---------------------------------------------------
cat > /etc/systemd/system/logfalcon@.service <<'EOF'
[Unit]
Description=LogFalcon (%I)
Documentation=https://github.com/proeugene/logfalcon
BindsTo=dev-%i.device
After=dev-%i.device network.target
Conflicts=logfalcon@*.service

[Service]
Type=oneshot
ExecStartPre=+/usr/bin/systemctl stop logfalcon-ready-led.service
ExecStartPre=/bin/sleep 3
User=bbsyncer
Group=dialout
WorkingDirectory=/opt/logfalcon
ExecStart=/opt/logfalcon/logfalcon --port /dev/%I
StandardOutput=journal
StandardError=journal
SyslogIdentifier=logfalcon
MemoryHigh=300M
MemoryMax=350M
TimeoutStartSec=600
TimeoutStopSec=10
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
RestrictSUIDSGID=true
LockPersonality=true
SystemCallArchitectures=native
ReadWritePaths=/mnt/logfalcon-logs /etc/logfalcon /sys/class/leds
ExecStopPost=+/usr/bin/systemctl start logfalcon-ready-led.service
Restart=no

[Install]
WantedBy=multi-user.target
EOF

cat > /etc/systemd/system/logfalcon-web.service <<'EOF'
[Unit]
Description=LogFalcon Web Server
Documentation=https://github.com/proeugene/logfalcon
After=network.target hostapd.service

[Service]
Type=simple
User=bbsyncer
Group=bbsyncer
WorkingDirectory=/opt/logfalcon
ExecStart=/opt/logfalcon/logfalcon --web
StandardOutput=journal
StandardError=journal
SyslogIdentifier=logfalcon-web
Restart=always
RestartSec=5
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
MemoryHigh=300M
MemoryMax=350M
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectKernelTunables=true
RestrictSUIDSGID=true
LockPersonality=true
SystemCallArchitectures=native
ReadWritePaths=/mnt/logfalcon-logs /etc/logfalcon /etc/hostapd

[Install]
WantedBy=multi-user.target
EOF

cat > /etc/systemd/system/logfalcon-config-apply.service <<'EOF'
[Unit]
Description=LogFalcon Config Apply
After=local-fs.target
Before=hostapd.service logfalcon-web.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/opt/logfalcon/config-apply.sh

[Install]
WantedBy=multi-user.target
EOF

cat > /etc/systemd/system/logfalcon-wifi-init.service <<'EOF'
[Unit]
Description=LogFalcon Wi-Fi regulatory domain and rfkill init
Before=hostapd.service dnsmasq.service systemd-networkd.service
After=sys-subsystem-net-devices-wlan0.device
Requires=sys-subsystem-net-devices-wlan0.device

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c 'rfkill unblock all; iw reg set US; sleep 1'

[Install]
WantedBy=multi-user.target
EOF

cat > /etc/systemd/system/logfalcon-boot-led.service <<'EOF'
[Unit]
Description=LogFalcon Boot LED Heartbeat
DefaultDependencies=no
Before=logfalcon-web.service
Conflicts=logfalcon-web.service

[Service]
Type=simple
ExecStart=/opt/logfalcon/boot-led.sh
Restart=no

[Install]
WantedBy=sysinit.target
EOF

cat > /etc/systemd/system/logfalcon-ready-led.service <<'EOF'
[Unit]
Description=LogFalcon Ready LED
After=logfalcon-web.service
Requires=logfalcon-web.service
Conflicts=logfalcon-boot-led.service

[Service]
Type=simple
ExecStart=/opt/logfalcon/ready-led.sh
Restart=no

[Install]
WantedBy=multi-user.target
EOF

info "Systemd units installed."

# --- Configure Wi-Fi hotspot -------------------------------------------------
info "Configuring Wi-Fi hotspot..."

# Static IP on wlan0
if [[ -f /etc/dhcpcd.conf ]]; then
    if ! grep -q "# LogFalcon hotspot" /etc/dhcpcd.conf 2>/dev/null; then
        cat >> /etc/dhcpcd.conf <<DHCPEOF

# LogFalcon hotspot static IP
interface wlan0
static ip_address=${HOTSPOT_IP}/24
nohook wpa_supplicant
DHCPEOF
    fi
else
    mkdir -p /etc/systemd/network
    cat > /etc/systemd/network/10-wlan0-static.network <<NETEOF
[Match]
Name=wlan0

[Network]
Address=${HOTSPOT_IP}/24
NETEOF
    systemctl enable systemd-networkd 2>/dev/null || true
fi

# hostapd
mkdir -p /etc/hostapd
cat > /etc/hostapd/hostapd.conf <<'HAPEOF'
interface=wlan0
driver=nl80211
ssid=LogFalcon
wpa_passphrase=fpvpilot
hw_mode=g
channel=6
ieee80211n=1
wmm_enabled=1
auth_algs=1
wpa=2
wpa_key_mgmt=WPA-PSK
rsn_pairwise=CCMP
beacon_int=100
dtim_period=2
max_num_sta=8
country_code=US
ieee80211d=1
HAPEOF
# Allow bbsyncer (web server) to rewrite hostapd.conf for settings save
chown bbsyncer:bbsyncer /etc/hostapd/hostapd.conf
sed -i 's|#DAEMON_CONF=.*|DAEMON_CONF="/etc/hostapd/hostapd.conf"|' /etc/default/hostapd 2>/dev/null || true

# Keep Bookworm's Wi-Fi client managers off the AP interface.
mkdir -p /etc/NetworkManager/conf.d
cat > /etc/NetworkManager/conf.d/logfalcon-unmanaged.conf <<'NMEOF'
[keyfile]
unmanaged-devices=interface-name:wlan0
NMEOF

# dnsmasq
cat > /etc/dnsmasq.d/logfalcon.conf <<DNSEOF
interface=wlan0
bind-interfaces
dhcp-range=192.168.4.2,192.168.4.20,24h
dhcp-option=option:router,${HOTSPOT_IP}
address=/#/${HOTSPOT_IP}
DNSEOF
grep -q "^no-resolv" /etc/dnsmasq.conf 2>/dev/null || echo "no-resolv" >> /etc/dnsmasq.conf

# dnsmasq and hostapd should wait until wlan0 exists and Wi-Fi init has run.
mkdir -p /etc/systemd/system/dnsmasq.service.d
cat > /etc/systemd/system/dnsmasq.service.d/wait-for-wlan0.conf <<'EOF'
[Unit]
After=sys-subsystem-net-devices-wlan0.device network-online.target logfalcon-wifi-init.service
Requires=sys-subsystem-net-devices-wlan0.device

[Service]
Restart=on-failure
RestartSec=3
EOF

mkdir -p /etc/systemd/system/hostapd.service.d
cat > /etc/systemd/system/hostapd.service.d/wait-for-wlan0.conf <<'EOF'
[Unit]
After=sys-subsystem-net-devices-wlan0.device logfalcon-wifi-init.service
Requires=sys-subsystem-net-devices-wlan0.device

[Service]
Restart=on-failure
RestartSec=3
EOF

# avahi mDNS
sed -i 's/^#*host-name=.*/host-name=logfalcon/' /etc/avahi/avahi-daemon.conf 2>/dev/null || true

# Wi-Fi regulatory domain + rfkill for AP mode.
mkdir -p /etc/wpa_supplicant
cat > /etc/wpa_supplicant/wpa_supplicant.conf <<'EOF'
country=US
ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev
update_config=1
EOF
if [[ -f /etc/default/crda ]]; then
    sed -i 's/^REGDOMAIN=.*/REGDOMAIN=US/' /etc/default/crda
    grep -q "^REGDOMAIN=" /etc/default/crda || echo "REGDOMAIN=US" >> /etc/default/crda
fi

rfkill unblock wlan 2>/dev/null || true

# This is an AP-only appliance; client-mode managers fight hostapd for wlan0.
systemctl mask wpa_supplicant.service 2>/dev/null || true
systemctl mask wpa_supplicant@wlan0.service 2>/dev/null || true
systemctl mask NetworkManager.service 2>/dev/null || true
systemctl mask NetworkManager-wait-online.service 2>/dev/null || true
systemctl mask ModemManager.service 2>/dev/null || true

info "Hotspot configured (SSID: LogFalcon, Password: fpvpilot)."

# --- Enable services ---------------------------------------------------------
info "Enabling services..."
systemctl daemon-reload

systemctl enable logfalcon-web.service
if [[ -x "$INSTALL_DIR/config-apply.sh" ]]; then
    systemctl enable logfalcon-config-apply.service
else
    warn "config-apply.sh not found — skipping logfalcon-config-apply.service"
fi
systemctl enable logfalcon-wifi-init.service
systemctl enable logfalcon-boot-led.service
systemctl enable logfalcon-ready-led.service
systemctl enable hostapd
systemctl enable dnsmasq
systemctl enable avahi-daemon

# --- Optional: boot optimizations -------------------------------------------
info "Applying boot optimizations..."

# Disable Bluetooth (saves ~5s boot time)
for cfgfile in /boot/firmware/config.txt /boot/config.txt; do
    if [[ -f "$cfgfile" ]]; then
        grep -q 'dtoverlay=disable-bt' "$cfgfile" 2>/dev/null || \
            echo -e '\n# LogFalcon: disable Bluetooth for faster boot\ndtoverlay=disable-bt\ndisable_splash=1\nboot_delay=0' >> "$cfgfile"
        break
    fi
done

# Quiet kernel boot
for cmdfile in /boot/firmware/cmdline.txt /boot/cmdline.txt; do
    if [[ -f "$cmdfile" ]]; then
        grep -q 'quiet' "$cmdfile" || sed -i 's/$/ quiet loglevel=3/' "$cmdfile"
        break
    fi
done

# Mask slow/unused services
systemctl mask apt-daily.timer 2>/dev/null || true
systemctl mask apt-daily-upgrade.timer 2>/dev/null || true
systemctl mask man-db.timer 2>/dev/null || true
systemctl disable hciuart.service 2>/dev/null || true
systemctl disable bluetooth.service 2>/dev/null || true

# --- Done! -------------------------------------------------------------------
echo ""
info "============================================"
info "  LogFalcon $VERSION installed successfully!"
info "============================================"
echo ""
info "  Wi-Fi SSID:     LogFalcon"
info "  Wi-Fi Password: fpvpilot"
info "  Web UI:         http://192.168.4.1"
info "  mDNS:           http://logfalcon.local"
echo ""
info "  Config:    $CONFIG_DIR/logfalcon.toml"
info "  Logs:      $LOG_DIR/"
info "  Binary:    $INSTALL_DIR/logfalcon"
echo ""
info "  Reboot to start all services: sudo reboot"
echo ""
