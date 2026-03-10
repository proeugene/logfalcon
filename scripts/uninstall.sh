#!/usr/bin/env bash
# LogFalcon — Uninstaller
# Usage: sudo bash /opt/logfalcon/uninstall.sh
#    or: curl -sSL https://github.com/proeugene/logfalcon/raw/main/scripts/uninstall.sh | sudo bash

set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()  { echo -e "${GREEN}[logfalcon]${NC} $*"; }
warn()  { echo -e "${YELLOW}[logfalcon]${NC} $*"; }
error() { echo -e "${RED}[logfalcon]${NC} $*" >&2; }

[[ $EUID -eq 0 ]] || { error "This script must be run as root (use sudo)."; exit 1; }

info "Uninstalling LogFalcon..."

# --- Stop and disable services -----------------------------------------------
info "Stopping services..."
for svc in logfalcon-web logfalcon-firstboot logfalcon-boot-led logfalcon-ready-led "logfalcon@*"; do
    systemctl stop "$svc".service 2>/dev/null || true
    systemctl disable "$svc".service 2>/dev/null || true
done

# --- Remove systemd units ----------------------------------------------------
info "Removing systemd units..."
rm -f /etc/systemd/system/logfalcon@.service
rm -f /etc/systemd/system/logfalcon-web.service
rm -f /etc/systemd/system/logfalcon-firstboot.service
rm -f /etc/systemd/system/logfalcon-boot-led.service
rm -f /etc/systemd/system/logfalcon-ready-led.service
systemctl daemon-reload

# --- Remove udev rule ---------------------------------------------------------
info "Removing udev rule..."
rm -f /etc/udev/rules.d/99-betaflight-fc.rules
udevadm control --reload-rules 2>/dev/null || true

# --- Remove files -------------------------------------------------------------
info "Removing installed files..."
rm -rf /opt/logfalcon
rm -rf /etc/logfalcon

# --- Remove hotspot config ----------------------------------------------------
info "Removing hotspot configuration..."
rm -f /etc/dnsmasq.d/logfalcon.conf
rm -f /etc/hostapd/hostapd.conf

# Remove LogFalcon block from dhcpcd.conf
if [[ -f /etc/dhcpcd.conf ]]; then
    sed -i '/# LogFalcon hotspot static IP/,/nohook wpa_supplicant/d' /etc/dhcpcd.conf 2>/dev/null || true
fi

# Remove systemd-networkd config if we created it
rm -f /etc/systemd/network/10-wlan0-static.network

# Restore avahi hostname
sed -i 's/^host-name=logfalcon/#host-name=/' /etc/avahi/avahi-daemon.conf 2>/dev/null || true

# --- Remove boot config ------------------------------------------------------
for dir in /boot/firmware /boot; do
    rm -f "$dir/logfalcon-config.txt" 2>/dev/null || true
done

# --- Remove boot optimizations (leave them — they're harmless) ----------------
warn "Boot optimizations (disable-bt, quiet boot) were left in place."
warn "Edit /boot/firmware/config.txt and cmdline.txt manually to revert if needed."

# --- Remove user (optional) ---------------------------------------------------
if id bbsyncer &>/dev/null; then
    info "Removing system user: bbsyncer"
    userdel bbsyncer 2>/dev/null || true
fi

# --- Keep log files -----------------------------------------------------------
if [[ -d /mnt/logfalcon-logs ]]; then
    warn "Log files preserved at /mnt/logfalcon-logs/"
    warn "Remove manually: sudo rm -rf /mnt/logfalcon-logs"
fi

# --- Unmask services we masked ------------------------------------------------
systemctl unmask apt-daily.timer 2>/dev/null || true
systemctl unmask apt-daily-upgrade.timer 2>/dev/null || true
systemctl unmask man-db.timer 2>/dev/null || true

echo ""
info "LogFalcon has been uninstalled."
info "Reboot recommended: sudo reboot"
echo ""
