#!/usr/bin/env bash
# LogFalcon — Full Install Script
# Run as root on a fresh Raspberry Pi OS Lite (bookworm, 64-bit)
#
# Usage:
#   sudo bash install.sh [--ssid "MySSID"] [--password "MyPass"]
#
# What this does:
#   1. Install system packages (hostapd, dnsmasq, avahi)
#   2. Create bbsyncer system user (legacy name kept for compatibility)
#   3. Install Go binary into /opt/logfalcon/
#   4. Set up Wi-Fi hotspot (hostapd + dnsmasq + static IP)
#   5. Mount point for log storage (/mnt/logfalcon-logs)
#   6. Install systemd units (sync service + web server)
#   7. Install udev rule
#   8. Enable and start services

set -euo pipefail

### --- Defaults --- ###
INSTALL_DIR="/opt/logfalcon"
LOG_DIR="/mnt/logfalcon-logs"
CONFIG_DIR="/etc/logfalcon"
SSID="LogFalcon"
WIFI_PASSWORD="fpvpilot"
HOTSPOT_IP="192.168.4.1"
HOTSPOT_DHCP_START="192.168.4.2"
HOTSPOT_DHCP_END="192.168.4.20"
PASSWORD_SET_BY_ARG=0

### --- Parse arguments --- ###
while [[ $# -gt 0 ]]; do
  case $1 in
    --ssid)      SSID="$2"; shift 2 ;;
    --password)  WIFI_PASSWORD="$2"; PASSWORD_SET_BY_ARG=1; shift 2 ;;
    *)           echo "Unknown argument: $1"; exit 1 ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "=== LogFalcon Install ==="
echo "Install dir:   $INSTALL_DIR"
echo "Log dir:       $LOG_DIR"
echo "Hotspot SSID:  $SSID"
echo ""

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: Must run as root (sudo)."
  exit 1
fi

# Establish upgrade state before generating or writing any defaults.
UPGRADING=0
if [[ -f "$CONFIG_DIR/logfalcon.toml" || -f "$CONFIG_DIR/hostapd.conf" || -f /etc/hostapd/hostapd.conf ]]; then
    UPGRADING=1
    if [[ ! -f "$CONFIG_DIR/logfalcon.toml" ]] || [[ ! -f "$CONFIG_DIR/hostapd.conf" && ! -f /etc/hostapd/hostapd.conf ]]; then
        echo "Incomplete existing configuration. Restore the application/hotspot files before upgrading; no defaults applied." >&2
        exit 1
    fi
    if [[ $PASSWORD_SET_BY_ARG -eq 1 || "$SSID" != LogFalcon ]]; then
        echo "Upgrade preserves settings. Edit the boot config to change credentials." >&2; exit 1
    fi
fi
# Detect architecture and copy appropriate binary
ARCH=$(uname -m)
if [[ "$(getconf LONG_BIT)" == 32 && "$ARCH" == aarch64 ]]; then ARCH=armv7l; fi
case "$ARCH" in
  armv6l|armv7l)  BINARY_SUFFIX="arm6" ;;
  aarch64) BINARY_SUFFIX="arm64" ;;
  x86_64)  BINARY_SUFFIX="amd64" ;;
  *)       echo "ERROR: Unsupported architecture: $ARCH"; exit 1 ;;
esac

BINARY_SRC="$SCRIPT_DIR/bin/logfalcon-${BINARY_SUFFIX}"
if [[ ! -f "$BINARY_SRC" ]]; then
  # Try single binary name
  BINARY_SRC="$SCRIPT_DIR/logfalcon"
fi
if [[ ! -f "$BINARY_SRC" ]]; then
  echo "ERROR: Binary not found. Build first: make build-pi"
  exit 1
fi

# Required source assets must exist before changing the machine.
for asset in logfalcon-update.service logfalcon-update.path logfalcon@.service logfalcon-web.service logfalcon-config-apply.service \
  logfalcon-wifi-init.service logfalcon-ready-led.service logfalcon-boot-led.service \
  logfalcon-runtime.service logfalcon-diagnostics.service logfalcon-diagnostics-export.service \
  logfalcon-diagnostics-export.timer logfalcon-shutdown.service 50-logfalcon.rules \
  config-apply.sh wifi-init.sh logfalcon-boot-led.sh 99-betaflight-fc.rules journald.conf; do
  [[ -f "$SCRIPT_DIR/system/$asset" ]] || { echo "Missing source asset: $asset; no system changes made." >&2; exit 1; }
done
[[ -f "$SCRIPT_DIR/config/logfalcon.toml" && -f "$SCRIPT_DIR/boot/logfalcon-config.txt" ]] || { echo "Missing configuration templates; no system changes made." >&2; exit 1; }

# Validate existing values before any machine change. Bootstrap is limited to
# complete Go/TOML installations; legacy Python layouts require manual recovery.
if [[ $UPGRADING -eq 1 ]]; then
 [[ -x "$INSTALL_DIR/logfalcon" ]] || { echo "Legacy runtime has no recoverable Go binary; use the documented migration/recovery route." >&2; exit 1; }
  "$BINARY_SRC" --check-upgrade-config --config "$CONFIG_DIR/logfalcon.toml"
  mkdir -p /run/logfalcon
  exec 9>/run/logfalcon/transfer.lock
  chown bbsyncer:bbsyncer /run/logfalcon/transfer.lock
  [[ ! -e /run/logfalcon/shutdown.pending ]] || { echo "Shutdown pending; retry after boot." >&2; exit 1; }
  flock -n 9 || { echo "Copy or shutdown is active; retry bootstrap when idle." >&2; exit 1; }
fi

### --- 1. System packages --- ###

echo "[1/8] Installing system packages..."
apt-get update -q
apt-get install -y \
  hostapd dnsmasq avahi-daemon \
  rfkill polkitd iw

# Unblock Wi-Fi
rfkill unblock wlan


### --- 2. System user --- ###
echo "[2/8] Creating bbsyncer system user (legacy name kept for compatibility)..."
if ! id bbsyncer &>/dev/null; then
  useradd --system --no-create-home --shell /sbin/nologin \
    --groups dialout,gpio bbsyncer 2>/dev/null || \
  useradd --system --no-create-home --shell /sbin/nologin \
    --groups dialout bbsyncer
fi

### --- 3. Install Go binary --- ###
echo "[3/8] Installing LogFalcon binary..."
mkdir -p "$INSTALL_DIR"

install -m 755 "$BINARY_SRC" "$INSTALL_DIR/logfalcon"

# Config file
mkdir -p "$CONFIG_DIR"
if [[ ! -f "$CONFIG_DIR/logfalcon.toml" ]]; then
  cp "$SCRIPT_DIR/config/logfalcon.toml" "$CONFIG_DIR/logfalcon.toml"
fi
chown -R root:root "$INSTALL_DIR"
chown -R bbsyncer:bbsyncer "$CONFIG_DIR"
chmod 600 "$CONFIG_DIR/logfalcon.toml"

### --- 4. Log storage --- ###
echo "[4/8] Setting up log storage directory..."
mkdir -p "$LOG_DIR"
chown bbsyncer:bbsyncer "$LOG_DIR"
chmod 755 "$LOG_DIR"

# Optional: if a dedicated partition is available, mount it
# Add to /etc/fstab:  /dev/sdXn  /mnt/logfalcon-logs  ext4  defaults,noatime  0  2

### --- 5. Wi-Fi hotspot --- ###
echo "[5/8] Configuring Wi-Fi hotspot..."

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

# hostapd config
if [[ ! -f "$CONFIG_DIR/hostapd.conf" && -f /etc/hostapd/hostapd.conf ]]; then
    cp -L /etc/hostapd/hostapd.conf "$CONFIG_DIR/hostapd.conf"
fi
if [[ ! -f "$CONFIG_DIR/hostapd.conf" ]]; then
cat > /etc/logfalcon/hostapd.conf <<EOF
interface=wlan0
driver=nl80211
ssid=$SSID
wpa_passphrase=$WIFI_PASSWORD
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
EOF
fi

# Allow bbsyncer (web server) to rewrite hostapd.conf for settings save
chown bbsyncer:bbsyncer /etc/logfalcon/hostapd.conf
chmod 600 /etc/logfalcon/hostapd.conf
ln -sfn ../logfalcon/hostapd.conf /etc/hostapd/hostapd.conf

# Enable hostapd
sed -i 's|#DAEMON_CONF=.*|DAEMON_CONF="/etc/hostapd/hostapd.conf"|' /etc/default/hostapd

# Keep Bookworm's Wi-Fi client managers off the AP interface.
mkdir -p /etc/NetworkManager/conf.d
cat > /etc/NetworkManager/conf.d/logfalcon-unmanaged.conf <<EOF
[keyfile]
unmanaged-devices=interface-name:wlan0
EOF

# dnsmasq config (DHCP + DNS redirect for captive portal)
cat > /etc/dnsmasq.d/logfalcon.conf <<EOF
# logfalcon: DHCP + captive portal DNS
interface=wlan0
bind-interfaces
dhcp-range=$HOTSPOT_DHCP_START,$HOTSPOT_DHCP_END,24h
dhcp-option=option:router,$HOTSPOT_IP
log-dhcp
# Redirect ALL DNS queries to Pi (captive portal)
address=/#/$HOTSPOT_IP
EOF

# Prevent dnsmasq from managing resolv.conf
grep -q "^no-resolv" /etc/dnsmasq.conf 2>/dev/null || echo "no-resolv" >> /etc/dnsmasq.conf

# dnsmasq and hostapd should wait until wlan0 exists and Wi-Fi init has run.
mkdir -p /etc/systemd/system/dnsmasq.service.d
cat > /etc/systemd/system/dnsmasq.service.d/wait-for-wlan0.conf <<EOF
[Unit]
After=sys-subsystem-net-devices-wlan0.device systemd-networkd.service logfalcon-wifi-init.service
Requires=sys-subsystem-net-devices-wlan0.device systemd-networkd.service logfalcon-wifi-init.service

[Service]
Restart=on-failure
RestartSec=3
EOF

mkdir -p /etc/systemd/system/hostapd.service.d
cat > /etc/systemd/system/hostapd.service.d/wait-for-wlan0.conf <<EOF
[Unit]
After=sys-subsystem-net-devices-wlan0.device logfalcon-wifi-init.service
Requires=sys-subsystem-net-devices-wlan0.device systemd-networkd.service logfalcon-wifi-init.service

[Service]
Restart=on-failure
RestartSec=3
EOF

# avahi mDNS hostname
sed -i 's/^#*host-name=.*/host-name=logfalcon/' /etc/avahi/avahi-daemon.conf 2>/dev/null || true

# Regulatory domain + rfkill init for AP mode.
mkdir -p /etc/wpa_supplicant
if [[ ! -f /etc/wpa_supplicant/wpa_supplicant.conf ]]; then
cat > /etc/wpa_supplicant/wpa_supplicant.conf <<EOF
country=GB
ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev
update_config=1
EOF
fi
if [[ -f /etc/default/crda ]] && ! grep -q '^REGDOMAIN=' /etc/default/crda; then
    echo "REGDOMAIN=$(sed -n 's/^country_code=//p' /etc/logfalcon/hostapd.conf)" >> /etc/default/crda
fi

systemctl mask wpa_supplicant.service 2>/dev/null || true
systemctl mask wpa_supplicant@wlan0.service 2>/dev/null || true
systemctl mask NetworkManager.service 2>/dev/null || true
systemctl mask NetworkManager-wait-online.service 2>/dev/null || true
systemctl mask ModemManager.service 2>/dev/null || true

# Bring up wlan0 with static IP now
ip addr add "$HOTSPOT_IP/24" dev wlan0 2>/dev/null || true
ip link set wlan0 up 2>/dev/null || true

### --- 6. systemd units --- ###
echo "[6/8] Installing systemd units..."
cp "$SCRIPT_DIR/system/logfalcon@.service" /etc/systemd/system/
cp "$SCRIPT_DIR/system/logfalcon-web.service" /etc/systemd/system/
cp "$SCRIPT_DIR/system/logfalcon-boot-led.service" /etc/systemd/system/
cp "$SCRIPT_DIR/system/logfalcon-ready-led.service" /etc/systemd/system/
cp "$SCRIPT_DIR/system/logfalcon-wifi-init.service" /etc/systemd/system/

for unit in logfalcon-runtime.service logfalcon-diagnostics.service logfalcon-diagnostics-export.service logfalcon-diagnostics-export.timer logfalcon-shutdown.service; do
  install -m 644 "$SCRIPT_DIR/system/$unit" /etc/systemd/system/
done
install -m 755 "$SCRIPT_DIR/system/wifi-init.sh" "$INSTALL_DIR/wifi-init.sh"
mkdir -p /etc/systemd/journald.conf.d /var/log/journal
install -m 644 "$SCRIPT_DIR/system/journald.conf" /etc/systemd/journald.conf.d/logfalcon.conf

# Boot LED heartbeat script
install -m 755 "$SCRIPT_DIR/system/logfalcon-boot-led.sh" "$INSTALL_DIR/boot-led.sh"

# Ready LED script
install -m 755 "$SCRIPT_DIR/system/logfalcon-ready-led.sh" "$INSTALL_DIR/ready-led.sh"

if [[ $UPGRADING -eq 0 ]]; then touch "$CONFIG_DIR/identity.pending"; fi

# Offline updater: isolated inbox, root-owned journal, executable and trust store.
install -m 755 "$INSTALL_DIR/logfalcon" "$INSTALL_DIR/updater"
install -d -m 755 /var/lib/logfalcon-update /var/lib/logfalcon-update/state /usr/share/logfalcon/update-keys
install -d -m 770 /var/lib/logfalcon-update/inbox
chown bbsyncer:bbsyncer /var/lib/logfalcon-update/inbox
install -m 644 "$SCRIPT_DIR/system/logfalcon-update.service" "$SCRIPT_DIR/system/logfalcon-update.path" /etc/systemd/system/
if [[ -d "$SCRIPT_DIR/update-keys" ]]; then
  for key in "$SCRIPT_DIR"/update-keys/*.pub; do
    [[ -f "$key" ]] && install -m 644 "$key" /usr/share/logfalcon/update-keys/
  done
fi
systemctl daemon-reload
systemctl enable logfalcon-update.service logfalcon-update.path
systemctl start logfalcon-update.path

# Config-apply script (boot-time SSID/password applicator)
cp "$SCRIPT_DIR/system/config-apply.sh" "$INSTALL_DIR/config-apply.sh"
chmod +x "$INSTALL_DIR/config-apply.sh"
mkdir -p /var/lib/logfalcon
cp "$SCRIPT_DIR/system/logfalcon-config-apply.service" /etc/systemd/system/

# Allow bbsyncer (web server) to restart hostapd for settings save.
mkdir -p /etc/polkit-1/rules.d
install -m 644 "$SCRIPT_DIR/system/50-logfalcon.rules" /etc/polkit-1/rules.d/50-logfalcon-hostapd.rules

install -m 644 "$SCRIPT_DIR/system/logfalcon-shutdown.service" /etc/systemd/system/
systemctl daemon-reload
systemctl enable logfalcon-config-apply.service
systemctl enable logfalcon-wifi-init.service
systemctl enable logfalcon-web.service
systemctl enable logfalcon-boot-led.service
systemctl enable logfalcon-runtime.service
systemctl enable logfalcon-ready-led.service
systemctl enable logfalcon-diagnostics.service
systemctl enable logfalcon-diagnostics-export.timer
systemctl unmask hostapd
systemctl enable hostapd
systemctl enable dnsmasq
systemctl enable avahi-daemon

# Boot partition config (user-editable)
BOOT_DIR=""
[[ -d /boot/firmware ]] && BOOT_DIR="/boot/firmware"
[[ -z "$BOOT_DIR" && -d /boot ]] && BOOT_DIR="/boot"
if [[ -n "$BOOT_DIR" && ! -f "$BOOT_DIR/logfalcon-config.txt" ]]; then
  if [[ $UPGRADING -eq 0 ]]; then
    printf 'SSID=%s\nPASSWORD=%s\nCOUNTRY_CODE=GB\nDEBUG=true\n' "$SSID" "$WIFI_PASSWORD" > "$BOOT_DIR/logfalcon-config.txt"
  else
    printf '# Existing application and Wi-Fi settings preserved.\n# Add SSID, PASSWORD, COUNTRY_CODE or DEBUG to change them on reboot.\n' > "$BOOT_DIR/logfalcon-config.txt"
  fi
fi

### --- 7. udev rule --- ###
echo "[7/8] Installing udev rule..."
cp "$SCRIPT_DIR/system/99-betaflight-fc.rules" /etc/udev/rules.d/
udevadm control --reload-rules

### --- 8. Activation --- ###
echo "Reboot to activate the coordinated network, status and diagnostic services."

echo ""
echo "=== Install complete! ==="
echo ""
echo "Wi-Fi hotspot: $SSID (password stored in the boot configuration)"
echo "Web interface: http://$HOTSPOT_IP  or  http://logfalcon.local"
echo ""
echo "The status LED reports copy outcomes; confirm Wi-Fi in your phone before use."
echo "To sync logs: plug a Betaflight SPI-flash FC into the Pi's USB OTG port."
echo "To view logs: connect to the '$SSID' Wi-Fi network."
echo ""
echo "Check sync service status:  journalctl -u logfalcon@ttyACM0 -f"
echo "Check web server status:    journalctl -u logfalcon-web -f"
