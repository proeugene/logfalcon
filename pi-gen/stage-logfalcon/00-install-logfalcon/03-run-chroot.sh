#!/bin/bash -e
# Minimize image size.
# Runs inside the image chroot after the logfalcon package is fully installed.
# The C extension (_msp_fast.so) is already compiled at this point so build
# tools are safe to remove.

echo "=== [logfalcon] Pre-cleanup disk usage ==="
df -h /

# ---- 1. Remove build-only packages ----------------------------------------
# gcc / python3-dev were needed only to compile the C extension via pip.
apt-get purge -y \
    gcc python3-dev linux-libc-dev 2>/dev/null || true
apt-get autoremove -y --purge

# ---- 2. Remove appliance-irrelevant packages --------------------------------
# nfs-common, triggerhappy, lua5.1 come from stage2 but serve no purpose here.
apt-get purge -y \
    nfs-common triggerhappy \
    lua5.1 liblua5.1-0 \
    man-db manpages \
    tasksel tasksel-data \
    info install-info \
    2>/dev/null || true
apt-get autoremove -y --purge

# ---- 3. Clean apt package download cache ------------------------------------
# NOTE: Do NOT remove /var/lib/apt/lists — the export-image stage that pi-gen
# runs after all custom stages needs those lists to locate and install packages
# (e.g. userconf-pi). The lists are tiny and compress well inside the final .xz.
apt-get clean

# ---- 4. Remove documentation ------------------------------------------------
rm -rf \
    /usr/share/doc/* \
    /usr/share/man/* \
    /usr/share/info/* \
    /usr/share/groff/* \
    /usr/share/lintian/*

# ---- 5. Remove locale data (keep en / en_US only) ---------------------------
find /usr/share/locale -mindepth 1 -maxdepth 1 \
    ! -name 'locale.alias' \
    ! -name 'en' \
    ! -name 'en_US' \
    -exec rm -rf {} + 2>/dev/null || true

# ---- 6. Remove Python bytecode / pip caches ---------------------------------
find /opt /usr/lib/python3 -name '*.pyc' -delete 2>/dev/null || true
find /opt /usr/lib/python3 -name '__pycache__' -type d \
    -exec rm -rf {} + 2>/dev/null || true
rm -rf /root/.cache/pip /home/*/.cache/pip 2>/dev/null || true

# ---- 7. Pre-compile Python bytecode for faster startup ----------------------
python3 -m compileall -q /opt/logfalcon/ 2>/dev/null || true

# ---- 8. Boot speed optimizations -------------------------------------------
# Disable Bluetooth (unused — saves ~5s)
if [ -f /boot/firmware/config.txt ]; then
    grep -q 'dtoverlay=disable-bt' /boot/firmware/config.txt 2>/dev/null || \
        echo -e '\n# LogFalcon: disable Bluetooth for faster boot\ndtoverlay=disable-bt\ndisable_splash=1\nboot_delay=0' >> /boot/firmware/config.txt
elif [ -f /boot/config.txt ]; then
    grep -q 'dtoverlay=disable-bt' /boot/config.txt 2>/dev/null || \
        echo -e '\n# LogFalcon: disable Bluetooth for faster boot\ndtoverlay=disable-bt\ndisable_splash=1\nboot_delay=0' >> /boot/config.txt
fi

# Quiet kernel boot (reduces console output, saves ~2-3s)
for cmdfile in /boot/firmware/cmdline.txt /boot/cmdline.txt; do
    if [ -f "$cmdfile" ]; then
        grep -q 'quiet' "$cmdfile" || \
            sed -i 's/$/ quiet loglevel=3/' "$cmdfile"
        break
    fi
done

# Mask unused services that slow down boot
systemctl mask triggerhappy.service 2>/dev/null || true
systemctl mask apt-daily.timer 2>/dev/null || true
systemctl mask apt-daily-upgrade.timer 2>/dev/null || true
systemctl mask man-db.timer 2>/dev/null || true

# Disable serial console on BT UART (paired with disable-bt overlay)
systemctl disable hciuart.service 2>/dev/null || true
systemctl disable bluetooth.service 2>/dev/null || true

# ---- 9. Temp / history ------------------------------------------------------
rm -rf /tmp/* /var/tmp/* /root/.bash_history 2>/dev/null || true

echo "=== [logfalcon] Post-cleanup disk usage ==="
df -h /
