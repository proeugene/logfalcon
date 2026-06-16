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
