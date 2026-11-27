#!/bin/sh
# LogFalcon boot LED heartbeat — slow pulse while the Pi is starting up.
# Stopped and reaped before the status LED service starts.

LED=
for candidate in /sys/class/leds/ACT /sys/class/leds/led0; do
    if [ -w "$candidate/trigger" ] && [ -w "$candidate/brightness" ]; then
        LED=$candidate
        break
    fi
done
if [ -z "$LED" ]; then
    echo "LogFalcon boot LED unavailable: no writable ACT or led0 attributes" >&2
    exit 0
fi
TRIGGER="$LED/trigger"
BRIGHTNESS="$LED/brightness"

# Take control of the ACT LED
{ echo none > "$TRIGGER"; } 2>/dev/null || exit 0

cleanup() {
    { echo 0 > "$BRIGHTNESS"; } 2>/dev/null || :
    exit 0
}
trap cleanup TERM INT

# Slow heartbeat: 1s on / 1s off
while true; do
    { echo 1 > "$BRIGHTNESS"; } 2>/dev/null || exit 0
    sleep 1
    { echo 0 > "$BRIGHTNESS"; } 2>/dev/null || exit 0
    sleep 1
done
