#!/bin/bash -e
# Minimize image size.
# Runs inside the image chroot after logfalcon is fully installed.

echo "=== [logfalcon] Pre-cleanup disk usage ==="
df -h /

# The canonical journald configuration was installed by 00-run.sh.
# Do not overwrite persistence/retention settings here.

# ---- 1. Remove appliance-irrelevant packages --------------------------------
# nfs-common, triggerhappy, lua5.1 come from stage2 but serve no purpose here.
apt-get purge -y \
    nfs-common triggerhappy \
    lua5.1 liblua5.1-0 \
    man-db manpages \
    tasksel tasksel-data \
    info install-info \
    2>/dev/null || true
apt-get autoremove -y --purge

# ---- 2. Clean apt package download cache ------------------------------------
# NOTE: Do NOT remove /var/lib/apt/lists — the export-image stage that pi-gen
# runs after all custom stages needs those lists to locate and install packages
# (e.g. userconf-pi). The lists are tiny and compress well inside the final .xz.
apt-get clean

# Preserve /usr/share/doc, including package copyright and licence notices.
# ---- 3. Remove optional manual pages ------------------------------------------------
rm -rf \
    /usr/share/man/* \
    /usr/share/info/* \
    /usr/share/groff/* \
    /usr/share/lintian/*

# ---- 4. Remove locale data (keep English, including UK locale) --------------
find /usr/share/locale -mindepth 1 -maxdepth 1 \
    ! -name 'locale.alias' \
    ! -name 'en' \
    ! -name 'en_US' \
    ! -name 'en_GB' \
    -exec rm -rf {} + 2>/dev/null || true

# ---- 5. Boot speed optimizations -------------------------------------------
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
