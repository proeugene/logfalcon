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
if [[ "$(getconf LONG_BIT)" == 32 && "$ARCH" == aarch64 ]]; then ARCH=armv7l; fi
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

ASSET_DIR=$(mktemp -d /tmp/logfalcon-assets.XXXXXX)
trap 'rm -rf "$ASSET_DIR"' EXIT
ASSET_BASE="https://github.com/$REPO/releases/download/$VERSION"
fetch_asset() {
    if command -v curl >/dev/null 2>&1; then
        curl --fail --silent --show-error --location "$ASSET_BASE/$1" -o "$ASSET_DIR/$1"
    else
        wget -qO "$ASSET_DIR/$1" "$ASSET_BASE/$1"
    fi
}
for asset in logfalcon-update.service logfalcon-update.path logfalcon@.service logfalcon-web.service logfalcon-config-apply.service \
    logfalcon-wifi-init.service logfalcon-boot-led.service logfalcon-ready-led.service \
    logfalcon-runtime.service logfalcon-diagnostics.service logfalcon-diagnostics-export.service \
    logfalcon-diagnostics-export.timer config-apply.sh wifi-init.sh logfalcon-boot-led.sh \
    99-betaflight-fc.rules journald.conf 50-logfalcon.rules logfalcon-shutdown.service \
    logfalcon-default.toml logfalcon-boot-config.txt logfalcon-build.txt update-keys.txt checksums.txt; do
    fetch_asset "$asset" || die "Release $VERSION lacks required integration assets; no system changes made. Choose a complete candidate release."
done

while IFS= read -r key; do
 [[ "$key" =~ ^update-key-[A-Za-z0-9_-]+\.pub$ ]] || die "Unsafe update key name."
 fetch_asset "$key" || die "Trusted update key missing."
done < "$ASSET_DIR/update-keys.txt"

# --- Download binary
BINARY_URL="https://github.com/$REPO/releases/download/$VERSION/logfalcon-$SUFFIX"
BINARY_TMP="$ASSET_DIR/logfalcon-$SUFFIX"

info "Downloading logfalcon-$SUFFIX..."
if command -v curl >/dev/null 2>&1; then
    curl --fail -sSL -o "$BINARY_TMP" "$BINARY_URL" || die "Download failed. Check version $VERSION exists at $BINARY_URL"
else
    wget -qO "$BINARY_TMP" "$BINARY_URL" || die "Download failed. Check version $VERSION exists at $BINARY_URL"
fi
# Verify every executable and integration asset before changing the machine.
command -v sha256sum >/dev/null || die "sha256sum is required."
while IFS= read -r downloaded; do
    asset=$(basename "$downloaded")
    [[ "$asset" == checksums.txt ]] && continue
    expected=$(awk -v name="$asset" '$2 == name { print $1 }' "$ASSET_DIR/checksums.txt")
    [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || die "Missing/ambiguous checksum for $asset; no changes made."
    actual=$(sha256sum "$downloaded" | awk '{print $1}')
    [[ "$actual" == "$expected" ]] || die "Checksum mismatch for $asset; no changes made."
done < <(find "$ASSET_DIR" -maxdepth 1 -type f)
chmod +x "$BINARY_TMP"

# Quick sanity check
file "$BINARY_TMP" | grep -qi "ELF.*ARM\|ELF.*aarch64" || die "Downloaded file is not an ARM executable."
release_version=$(sed -n '1p' "$ASSET_DIR/logfalcon-build.txt")
release_commit=$(sed -n '2p' "$ASSET_DIR/logfalcon-build.txt")
[[ "$release_version" == "$VERSION" && "$release_commit" =~ ^[0-9a-f]{40}$ ]] || die "Invalid release identity."
[[ "$("$BINARY_TMP" --version)" == "logfalcon $VERSION ($release_commit)" ]] || die "Binary and integration assets refer to different builds."
info "Verified release $VERSION at $release_commit."

# Establish upgrade state before generating or writing any defaults.
UPGRADING=0
if [[ -f "$CONFIG_DIR/logfalcon.toml" || -f "$CONFIG_DIR/hostapd.conf" || -f /etc/hostapd/hostapd.conf ]]; then
    UPGRADING=1
    if [[ ! -f "$CONFIG_DIR/logfalcon.toml" ]] || [[ ! -f "$CONFIG_DIR/hostapd.conf" && ! -f /etc/hostapd/hostapd.conf ]]; then
        echo "Incomplete existing configuration. Restore the application/hotspot files before upgrading; no defaults applied." >&2
        exit 1
    fi
fi
if [[ $UPGRADING -eq 1 ]]; then
 [[ -x "$INSTALL_DIR/logfalcon" ]] || { echo "Legacy runtime has no recoverable Go binary; use the documented migration/recovery route." >&2; exit 1; }
 "$BINARY_TMP" --check-upgrade-config --config "$CONFIG_DIR/logfalcon.toml"
 mkdir -p /run/logfalcon
 exec 9>/run/logfalcon/transfer.lock
 chown bbsyncer:bbsyncer /run/logfalcon/transfer.lock
 [[ ! -e /run/logfalcon/shutdown.pending ]] || die "Shutdown pending; retry after boot."
 flock -n 9 || die "Copy or shutdown is active; retry bootstrap when idle."
fi
# --- Install packages
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

chown -R "$SERVICE_USER":"$SERVICE_USER" "$CONFIG_DIR" "$LOG_DIR"
chown -R root:root "$INSTALL_DIR"
chmod 755 "$LOG_DIR"

# --- Install config ----------------------------------------------------------
if [[ ! -f "$CONFIG_DIR/logfalcon.toml" ]]; then
    install -m 600 "$ASSET_DIR/logfalcon-default.toml" "$CONFIG_DIR/logfalcon.toml"
    chown "$SERVICE_USER":"$SERVICE_USER" "$CONFIG_DIR/logfalcon.toml"
    chmod 600 "$CONFIG_DIR/logfalcon.toml"
    info "Created default config at $CONFIG_DIR/logfalcon.toml"
else
    info "Config already exists at $CONFIG_DIR/logfalcon.toml — keeping it."
fi

if [[ $UPGRADING -eq 0 ]]; then touch "$CONFIG_DIR/identity.pending"; fi

# Offline updater: isolated inbox, root-owned journal, executable and trust store.
install -m 755 "$INSTALL_DIR/logfalcon" "$INSTALL_DIR/updater"
install -d -m 755 /var/lib/logfalcon-update /var/lib/logfalcon-update/state /usr/share/logfalcon/update-keys
install -d -m 770 /var/lib/logfalcon-update/inbox
chown bbsyncer:bbsyncer /var/lib/logfalcon-update/inbox
install -m 644 "$ASSET_DIR/logfalcon-update.service" "$ASSET_DIR/logfalcon-update.path" /etc/systemd/system/
# Public trust keys must be distributed as authenticated release assets.
for key in "$ASSET_DIR"/update-key-*.pub; do
  [[ -f "$key" ]] || continue
  name="${key##*/}"; name="${name#update-key-}"
  install -m 644 "$key" "/usr/share/logfalcon/update-keys/$name"
done
systemctl daemon-reload
systemctl enable logfalcon-update.service logfalcon-update.path
systemctl start logfalcon-update.path

# --- Install boot config (first-boot SSID/password) -------------------------
BOOT_DIR=""
[[ -d /boot/firmware ]] && BOOT_DIR="/boot/firmware"
[[ -z "$BOOT_DIR" && -d /boot ]] && BOOT_DIR="/boot"

if [[ -n "$BOOT_DIR" && ! -f "$BOOT_DIR/logfalcon-config.txt" && $UPGRADING -eq 0 ]]; then
    install -m 644 "$ASSET_DIR/logfalcon-boot-config.txt" "$BOOT_DIR/logfalcon-config.txt"
    info "Created $BOOT_DIR/logfalcon-config.txt (edit to change Wi-Fi name/password)."
fi

# --- Install canonical integration assets from the same release as the binary ---
# Keep both installer paths on identical service units and USB rules.
for asset in logfalcon@.service logfalcon-web.service logfalcon-config-apply.service \
    logfalcon-wifi-init.service logfalcon-boot-led.service logfalcon-ready-led.service \
    logfalcon-runtime.service logfalcon-diagnostics.service \
    logfalcon-diagnostics-export.service logfalcon-diagnostics-export.timer logfalcon-shutdown.service; do
    install -m 644 "$ASSET_DIR/$asset" /etc/systemd/system/
done
install -m 755 "$ASSET_DIR/config-apply.sh" "$INSTALL_DIR/config-apply.sh"
install -m 755 "$ASSET_DIR/wifi-init.sh" "$INSTALL_DIR/wifi-init.sh"
install -m 755 "$ASSET_DIR/logfalcon-boot-led.sh" "$INSTALL_DIR/boot-led.sh"
install -m 644 "$ASSET_DIR/99-betaflight-fc.rules" /etc/udev/rules.d/
udevadm control --reload-rules
mkdir -p /etc/systemd/journald.conf.d /var/log/journal /var/lib/logfalcon
install -m 644 "$ASSET_DIR/journald.conf" /etc/systemd/journald.conf.d/logfalcon.conf

# Only permit the one restart verb needed by the settings page.
mkdir -p /etc/polkit-1/rules.d
install -m 644 "$ASSET_DIR/50-logfalcon.rules" /etc/polkit-1/rules.d/50-logfalcon-hostapd.rules

# --- Configure Wi-Fi hotspot -------------------------------------------------
info "Configuring Wi-Fi hotspot..."

# Configure the AP even before carrier appears; retain its address on Wi-Fi restart.
systemctl mask dhcpcd.service 2>/dev/null || true
mkdir -p /etc/systemd/network
cat > /etc/systemd/network/10-wlan0-static.network <<'NETEOF'
[Match]
Name=wlan0

[Network]
Address=192.168.4.1/24
ConfigureWithoutCarrier=yes
IgnoreCarrierLoss=yes
DHCP=no
IPv6AcceptRA=no
NETEOF
systemctl enable systemd-networkd

# hostapd
mkdir -p /etc/hostapd
if [[ ! -f "$CONFIG_DIR/hostapd.conf" && -f /etc/hostapd/hostapd.conf ]]; then
    cp -L /etc/hostapd/hostapd.conf "$CONFIG_DIR/hostapd.conf"
fi
if [[ ! -f "$CONFIG_DIR/hostapd.conf" ]]; then
cat > /etc/logfalcon/hostapd.conf <<'HAPEOF'
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
country_code=GB
ieee80211d=1
HAPEOF
fi
# Allow bbsyncer (web server) to rewrite hostapd.conf for settings save
chown bbsyncer:bbsyncer /etc/logfalcon/hostapd.conf
chmod 600 /etc/logfalcon/hostapd.conf
ln -sfn ../logfalcon/hostapd.conf /etc/hostapd/hostapd.conf
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
log-dhcp
address=/#/${HOTSPOT_IP}
DNSEOF
grep -q "^no-resolv" /etc/dnsmasq.conf 2>/dev/null || echo "no-resolv" >> /etc/dnsmasq.conf

# dnsmasq and hostapd should wait until wlan0 exists and Wi-Fi init has run.
mkdir -p /etc/systemd/system/dnsmasq.service.d
cat > /etc/systemd/system/dnsmasq.service.d/wait-for-wlan0.conf <<'EOF'
[Unit]
After=sys-subsystem-net-devices-wlan0.device systemd-networkd.service logfalcon-wifi-init.service
Requires=sys-subsystem-net-devices-wlan0.device systemd-networkd.service logfalcon-wifi-init.service

[Service]
Restart=on-failure
RestartSec=3
EOF

mkdir -p /etc/systemd/system/hostapd.service.d
cat > /etc/systemd/system/hostapd.service.d/wait-for-wlan0.conf <<'EOF'
[Unit]
After=sys-subsystem-net-devices-wlan0.device logfalcon-wifi-init.service
Requires=sys-subsystem-net-devices-wlan0.device systemd-networkd.service logfalcon-wifi-init.service

[Service]
Restart=on-failure
RestartSec=3
EOF

# avahi mDNS
sed -i 's/^#*host-name=.*/host-name=logfalcon/' /etc/avahi/avahi-daemon.conf 2>/dev/null || true

# Wi-Fi regulatory domain + rfkill for AP mode.
mkdir -p /etc/wpa_supplicant
if [[ ! -f /etc/wpa_supplicant/wpa_supplicant.conf ]]; then
cat > /etc/wpa_supplicant/wpa_supplicant.conf <<'EOF'
country=GB
ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev
update_config=1
EOF
fi
if [[ -f /etc/default/crda ]] && ! grep -q '^REGDOMAIN=' /etc/default/crda; then
    echo "REGDOMAIN=$(sed -n 's/^country_code=//p' /etc/logfalcon/hostapd.conf)" >> /etc/default/crda
fi

rfkill unblock wlan 2>/dev/null || true

# This is an AP-only appliance; client-mode managers fight hostapd for wlan0.
systemctl mask wpa_supplicant.service 2>/dev/null || true
systemctl mask wpa_supplicant@wlan0.service 2>/dev/null || true
systemctl mask NetworkManager.service 2>/dev/null || true
systemctl mask NetworkManager-wait-online.service 2>/dev/null || true
systemctl mask ModemManager.service 2>/dev/null || true

info "Hotspot configured; existing credentials and country preserved on upgrades."

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
systemctl enable logfalcon-runtime.service
systemctl enable logfalcon-ready-led.service
systemctl enable logfalcon-diagnostics.service
systemctl enable logfalcon-diagnostics-export.timer
systemctl unmask hostapd
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
info "  Wi-Fi settings: /etc/logfalcon/hostapd.conf (preserved on upgrade)"
info "  Web UI:         http://192.168.4.1"
info "  mDNS:           http://logfalcon.local"
echo ""
info "  Config:    $CONFIG_DIR/logfalcon.toml"
info "  Logs:      $LOG_DIR/"
info "  Binary:    $INSTALL_DIR/logfalcon"
echo ""
info "  Reboot to start all services: sudo reboot"
echo ""
