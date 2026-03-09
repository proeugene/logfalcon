#!/bin/sh
# LogFalcon ready LED — solid on once the Pi is fully booted.
# Managed by logfalcon-ready-led.service.
# Stopped during sync (ExecStartPre in logfalcon@.service), resumed after.

LED="/sys/class/leds/led0"
TRIGGER="$LED/trigger"
BRIGHTNESS="$LED/brightness"

# Take control of the ACT LED
echo none > "$TRIGGER" 2>/dev/null

cleanup() {
    echo 0 > "$BRIGHTNESS" 2>/dev/null
    exit 0
}
trap cleanup TERM INT

# Solid on — Pi is ready
echo 1 > "$BRIGHTNESS" 2>/dev/null

# Hold LED on until killed
sleep infinity &
wait
