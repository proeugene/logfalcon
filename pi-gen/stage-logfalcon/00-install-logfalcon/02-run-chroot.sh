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

# dnsmasq config
cat > /etc/dnsmasq.d/logfalcon.conf <<EOF
interface=wlan0
bind-interfaces
dhcp-range=192.168.4.2,192.168.4.20,24h
dhcp-option=option:router,${HOTSPOT_IP}
address=/#/${HOTSPOT_IP}
EOF

grep -q "^no-resolv" /etc/dnsmasq.conf 2>/dev/null || echo "no-resolv" >> /etc/dnsmasq.conf

# avahi mDNS hostname
sed -i 's/^#*host-name=.*/host-name=logfalcon/' /etc/avahi/avahi-daemon.conf 2>/dev/null || true

# Unblock Wi-Fi at boot
rfkill unblock wlan 2>/dev/null || true
