#!/bin/sh
set -eu
country=$(sed -n 's/^country_code=//p' /etc/logfalcon/hostapd.conf)
case "$country" in
    [A-Z][A-Z]) ;;
    *) logger -t logfalcon-wifi-init 'Invalid Wi-Fi country code'; exit 1 ;;
esac
rfkill unblock wlan
iw reg set "$country"
logger -t logfalcon-wifi-init "Wi-Fi unblocked; regulatory country=$country"
