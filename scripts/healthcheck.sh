#!/bin/sh
# LogFalcon health check — POSIX sh, zero temp files.
#
# Queries http://127.0.0.1/health and exits 0 only when the JSON
# response contains "ok":true.  Also reads the Pi thermal zone and
# warns above 80 °C.
#
# Usage: /opt/logfalcon/healthcheck.sh
#   Compose: healthcheck: ./opt/logfalcon/healthcheck.sh

set -e

HEALTH_URL="http://127.0.0.1/health"
THERMAL_ZONE="/sys/class/thermal/thermal_zone0/temp"
THERMAL_THRESHOLD=80000

# --- Fetch health JSON (curl or wget, whatever is available) ---
body=""
if command -v curl >/dev/null 2>&1; then
    body=$(curl -sS --max-time 5 "$HEALTH_URL" 2>/dev/null) || :
elif command -v wget >/dev/null 2>&1; then
    body=$(wget -qO- --timeout=5 "$HEALTH_URL" 2>/dev/null) || :
fi

if [ -z "$body" ]; then
    echo "UNHEALTHY: no response from $HEALTH_URL"
    exit 1
fi

# --- Check "ok":true in JSON (grep is more portable than jq/sh) ---
if ! echo "$body" | grep -q '"ok"[[:space:]]*:[[:space:]]*true'; then
    echo "UNHEALTHY: ok != true"
    exit 1
fi

# --- Thermal warning ---
status_line="HEALTHY"
if [ -f "$THERMAL_ZONE" ]; then
    millideg=$(cat "$THERMAL_ZONE" 2>/dev/null) || millideg=0
    if [ "$millideg" -ge "$THERMAL_THRESHOLD" ] 2>/dev/null; then
        temp_c=$(echo "$millideg" | awk '{printf "%.1f", $1/1000}')
        status_line="HEALTHY (thermal warning: ${temp_c}°C)"
    fi
fi

echo "$status_line"
exit 0