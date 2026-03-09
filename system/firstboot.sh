#!/usr/bin/env bash
# LogFalcon — Boot-time config applicator
# Reads /boot/firmware/logfalcon-config.txt and updates hostapd + app config.
# Runs on every boot so users can edit config.txt and reboot to apply.

set -euo pipefail

CONFIG_FILE="/boot/firmware/logfalcon-config.txt"
HOSTAPD_CONF="/etc/hostapd/hostapd.conf"
LOGFALCON_CONF="/etc/logfalcon/logfalcon.toml"
TAG="logfalcon-firstboot"

log() { logger -t "$TAG" "$*"; }
escape_sed() { printf '%s' "$1" | sed -e 's/[\/&]/\\&/g'; }

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
  # Skip comments and blank lines
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

log "Applying config: SSID='$SSID'"

# Update hostapd.conf
if [[ -f "$HOSTAPD_CONF" ]]; then
  sed -i "s/^ssid=.*/ssid=$SSID_ESCAPED/" "$HOSTAPD_CONF"
  sed -i "s/^wpa_passphrase=.*/wpa_passphrase=$PASSWORD_ESCAPED/" "$HOSTAPD_CONF"
  log "Updated $HOSTAPD_CONF"
else
  log "WARNING: $HOSTAPD_CONF not found — skipped hostapd update."
fi

# Update logfalcon.toml
if [[ -f "$LOGFALCON_CONF" ]]; then
  sed -i "s/^hotspot_ssid = .*/hotspot_ssid = \"$SSID_ESCAPED\"/" "$LOGFALCON_CONF"
  sed -i "s/^hotspot_password = .*/hotspot_password = \"$PASSWORD_ESCAPED\"/" "$LOGFALCON_CONF"
  log "Updated $LOGFALCON_CONF"
else
  log "WARNING: $LOGFALCON_CONF not found — skipped logfalcon config update."
fi

log "Boot config applied successfully."
