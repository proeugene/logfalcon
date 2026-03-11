#!/bin/bash -e
# Configure Wi-Fi hotspot

HOTSPOT_IP="192.168.4.1"

# Static IP on wlan0 via dhcpcd (standard RPi OS Bookworm networking)
if [ -f /etc/dhcpcd.conf ]; then
    cat >> /etc/dhcpcd.conf <<EOF

# LogFalcon hotspot static IP
interface wlan0
static ip_address=${HOTSPOT_IP}/24
nohook wpa_supplicant
EOF
else
    # Fallback: systemd-networkd for non-dhcpcd images
    mkdir -p /etc/systemd/network
    cat > /etc/systemd/network/10-wlan0-static.network <<EOF
[Match]
Name=wlan0

[Network]
Address=${HOTSPOT_IP}/24
EOF
    systemctl enable systemd-networkd 2>/dev/null || true
fi

# hostapd config (defaults — overridden by firstboot from logfalcon-config.txt)
cat > /etc/hostapd/hostapd.conf <<EOF
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
EOF

# Enable hostapd config
sed -i 's|#DAEMON_CONF=.*|DAEMON_CONF="/etc/hostapd/hostapd.conf"|' /etc/default/hostapd 2>/dev/null || true

# Belt-and-suspenders: tell NetworkManager to never manage wlan0 or usb0,
# in case NM gets un-masked by a later package install or user action.
mkdir -p /etc/NetworkManager/conf.d
cat > /etc/NetworkManager/conf.d/logfalcon-unmanaged.conf <<EOF
[keyfile]
unmanaged-devices=interface-name:wlan0
EOF

# dnsmasq config
cat > /etc/dnsmasq.d/logfalcon.conf <<EOF
interface=wlan0
bind-interfaces
dhcp-range=192.168.4.2,192.168.4.20,24h
dhcp-option=option:router,${HOTSPOT_IP}
address=/#/${HOTSPOT_IP}
EOF

grep -q "^no-resolv" /etc/dnsmasq.conf 2>/dev/null || echo "no-resolv" >> /etc/dnsmasq.conf

# dnsmasq override: wait for wlan0 to be configured before starting.
# Without this, dnsmasq races with systemd-networkd and fails to bind to
# 192.168.4.1 because the interface isn't up yet.
mkdir -p /etc/systemd/system/dnsmasq.service.d
cat > /etc/systemd/system/dnsmasq.service.d/wait-for-wlan0.conf <<EOF
[Unit]
After=sys-subsystem-net-devices-wlan0.device network-online.target logfalcon-wifi-init.service
Requires=sys-subsystem-net-devices-wlan0.device
[Service]
Restart=on-failure
RestartSec=3
EOF

# hostapd override: also ensure it starts after wlan0 device is ready.
mkdir -p /etc/systemd/system/hostapd.service.d
cat > /etc/systemd/system/hostapd.service.d/wait-for-wlan0.conf <<EOF
[Unit]
After=sys-subsystem-net-devices-wlan0.device logfalcon-wifi-init.service
Requires=sys-subsystem-net-devices-wlan0.device
[Service]
Restart=on-failure
RestartSec=3
EOF

# avahi mDNS hostname
sed -i 's/^#*host-name=.*/host-name=logfalcon/' /etc/avahi/avahi-daemon.conf 2>/dev/null || true

# ── Wi-Fi regulatory domain + rfkill ─────────────────────────────────────────
# The Linux wireless subsystem (cfg80211) must have a country code set before
# hostapd can start AP mode. Without it the chip stays in world regulatory
# mode (00) which blocks or restricts AP mode — especially on Pi Zero 2 W.
#
# wpa_supplicant normally sets the country, but we disable it.
# Fix: a oneshot systemd service that runs iw/rfkill before hostapd.
#
# Also write a minimal wpa_supplicant.conf with country= so cfg80211 picks it
# up from the file even with wpa_supplicant disabled.
mkdir -p /etc/wpa_supplicant
cat > /etc/wpa_supplicant/wpa_supplicant.conf <<EOF
country=US
ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev
update_config=1
EOF

# crda fallback
if [ -f /etc/default/crda ]; then
    sed -i 's/^REGDOMAIN=.*/REGDOMAIN=US/' /etc/default/crda
    grep -q "^REGDOMAIN=" /etc/default/crda || echo "REGDOMAIN=US" >> /etc/default/crda
fi

# Boot-time service: sets regulatory domain and unblocks rfkill BEFORE hostapd.
# This is the reliable fix — rfkill state and regulatory domain are not
# persisted across reboots; they must be set each boot.
cat > /etc/systemd/system/logfalcon-wifi-init.service <<EOF
[Unit]
Description=LogFalcon Wi-Fi regulatory domain and rfkill init
# Must run before anything that tries to use wlan0 (networkd, hostapd, dnsmasq)
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

systemctl enable logfalcon-wifi-init.service
