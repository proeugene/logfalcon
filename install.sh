#!/usr/bin/env bash
# LogFalcon — Full Install Script
# Run as root on a fresh Raspberry Pi OS Lite (bookworm, 64-bit)
#
# Usage:
#   sudo bash install.sh [--ssid "MySSID"] [--password "MyPass"]
#
# What this does:
#   1. Install system packages (hostapd, dnsmasq, avahi, Python)
#   2. Create bbsyncer system user (legacy name kept for compatibility)
#   3. Install Python package into /opt/logfalcon/venv
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
SSID="BF-Blackbox"
WIFI_PASSWORD="fpvpilot"
HOTSPOT_IP="192.168.4.1"
HOTSPOT_NETMASK="255.255.255.0"
HOTSPOT_DHCP_START="192.168.4.2"
HOTSPOT_DHCP_END="192.168.4.20"
PASSWORD_SET_BY_ARG=0
GENERATED_PASSWORD=0

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

### --- 1. System packages --- ###
echo "[1/8] Installing system packages..."
apt-get update -q
apt-get install -y \
  python3 python3-pip python3-venv \
  hostapd dnsmasq avahi-daemon \
  rfkill

# Unblock Wi-Fi
rfkill unblock wlan

if [[ $PASSWORD_SET_BY_ARG -eq 0 ]]; then
  WIFI_PASSWORD="$(
    python3 - <<'PY'
import secrets
alphabet = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789'
print(''.join(secrets.choice(alphabet) for _ in range(12)))
PY
  )"
  GENERATED_PASSWORD=1
fi

### --- 2. System user --- ###
echo "[2/8] Creating bbsyncer system user (legacy name kept for compatibility)..."
if ! id bbsyncer &>/dev/null; then
  useradd --system --no-create-home --shell /sbin/nologin \
    --groups dialout,gpio bbsyncer 2>/dev/null || \
  useradd --system --no-create-home --shell /sbin/nologin \
    --groups dialout bbsyncer
fi

### --- 3. Python package --- ###
echo "[3/8] Installing Python package..."
mkdir -p "$INSTALL_DIR"
python3 -m venv "$INSTALL_DIR/venv"
"$INSTALL_DIR/venv/bin/pip" install --quiet --upgrade pip
"$INSTALL_DIR/venv/bin/pip" install --quiet "$SCRIPT_DIR"

# Config file
mkdir -p "$CONFIG_DIR"
if [[ ! -f "$CONFIG_DIR/logfalcon.toml" ]]; then
  cp "$SCRIPT_DIR/config/logfalcon.toml" "$CONFIG_DIR/logfalcon.toml"
  sed -i "s/hotspot_ssid = .*/hotspot_ssid = \"$SSID\"/" "$CONFIG_DIR/logfalcon.toml"
  sed -i "s/hotspot_password = .*/hotspot_password = \"$WIFI_PASSWORD\"/" "$CONFIG_DIR/logfalcon.toml"
fi
chown -R bbsyncer:bbsyncer "$INSTALL_DIR" "$CONFIG_DIR"

### --- 4. Log storage --- ###
echo "[4/8] Setting up log storage directory..."
mkdir -p "$LOG_DIR"
chown bbsyncer:bbsyncer "$LOG_DIR"
chmod 755 "$LOG_DIR"

# Optional: if a dedicated partition is available, mount it
# Add to /etc/fstab:  /dev/sdXn  /mnt/logfalcon-logs  ext4  defaults,noatime  0  2

### --- 5. Wi-Fi hotspot --- ###
echo "[5/8] Configuring Wi-Fi hotspot..."

# Static IP on wlan0
cat > /etc/network/interfaces.d/wlan0-static <<EOF
auto wlan0
iface wlan0 inet static
    address $HOTSPOT_IP
    netmask $HOTSPOT_NETMASK
EOF

# hostapd config
cat > /etc/hostapd/hostapd.conf <<EOF
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
country_code=US
ieee80211d=1
EOF

# Enable hostapd
sed -i 's|#DAEMON_CONF=.*|DAEMON_CONF="/etc/hostapd/hostapd.conf"|' /etc/default/hostapd

# dnsmasq config (DHCP + DNS redirect for captive portal)
cat > /etc/dnsmasq.d/logfalcon.conf <<EOF
# logfalcon: DHCP + captive portal DNS
interface=wlan0
bind-interfaces
dhcp-range=$HOTSPOT_DHCP_START,$HOTSPOT_DHCP_END,24h
dhcp-option=option:router,$HOTSPOT_IP
# Redirect ALL DNS queries to Pi (captive portal)
address=/#/$HOTSPOT_IP
EOF

# Prevent dnsmasq from managing resolv.conf
grep -q "^no-resolv" /etc/dnsmasq.conf 2>/dev/null || echo "no-resolv" >> /etc/dnsmasq.conf

# avahi mDNS hostname
sed -i 's/^#*host-name=.*/host-name=logfalcon/' /etc/avahi/avahi-daemon.conf 2>/dev/null || true

# Bring up wlan0 with static IP now
ip addr add "$HOTSPOT_IP/24" dev wlan0 2>/dev/null || true
ip link set wlan0 up 2>/dev/null || true

### --- 6. systemd units --- ###
echo "[6/8] Installing systemd units..."
cp "$SCRIPT_DIR/system/logfalcon@.service" /etc/systemd/system/
cp "$SCRIPT_DIR/system/logfalcon-web.service" /etc/systemd/system/
cp "$SCRIPT_DIR/system/logfalcon-boot-led.service" /etc/systemd/system/

# Boot LED heartbeat script
install -m 755 "$SCRIPT_DIR/system/logfalcon-boot-led.sh" "$INSTALL_DIR/boot-led.sh"

# Firstboot config service
cp "$SCRIPT_DIR/system/firstboot.sh" "$INSTALL_DIR/firstboot.sh"
chmod +x "$INSTALL_DIR/firstboot.sh"
cp "$SCRIPT_DIR/system/logfalcon-firstboot.service" /etc/systemd/system/
systemctl daemon-reload
systemctl enable logfalcon-firstboot.service
systemctl enable logfalcon-web.service
systemctl enable logfalcon-boot-led.service
systemctl enable hostapd
systemctl enable dnsmasq
systemctl enable avahi-daemon

# Boot partition config (user-editable)
if [[ ! -f /boot/firmware/logfalcon-config.txt ]]; then
  cp "$SCRIPT_DIR/boot/logfalcon-config.txt" /boot/firmware/logfalcon-config.txt
fi
sed -i "s/^SSID=.*/SSID=$SSID/" /boot/firmware/logfalcon-config.txt
sed -i "s/^PASSWORD=.*/PASSWORD=$WIFI_PASSWORD/" /boot/firmware/logfalcon-config.txt

### --- 7. udev rule --- ###
echo "[7/8] Installing udev rule..."
cp "$SCRIPT_DIR/system/99-betaflight-fc.rules" /etc/udev/rules.d/
udevadm control --reload-rules

### --- 8. Start services --- ###
echo "[8/8] Starting services..."
systemctl restart hostapd || true
systemctl restart dnsmasq || true
systemctl restart avahi-daemon || true
systemctl start logfalcon-web.service || true

echo ""
echo "=== Install complete! ==="
echo ""
echo "Wi-Fi hotspot: $SSID (password: $WIFI_PASSWORD)"
echo "Web interface: http://$HOTSPOT_IP  or  http://logfalcon.local"
echo ""
if [[ $GENERATED_PASSWORD -eq 1 ]]; then
  echo "A unique hotspot password was generated for this install."
  echo "Keep it somewhere safe before you leave for the field."
  echo ""
fi
echo "Startup note: after boot, give the Pi up to 90 seconds to bring up Wi-Fi and the web UI."
echo "The same SSID/password is mirrored into /boot/firmware/logfalcon-config.txt for later edits."
echo ""
echo "To sync logs: plug a Betaflight FC into the Pi's USB OTG port."
echo "To view logs: connect to the '$SSID' Wi-Fi network."
echo ""
echo "Check sync service status:  journalctl -u logfalcon@ttyACM0 -f"
echo "Check web server status:    journalctl -u logfalcon-web -f"
