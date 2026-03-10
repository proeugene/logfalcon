#!/usr/bin/env bash
# inspect-image.sh — Mount a LogFalcon SD card image and audit all configs/services.
#
# Usage:
#   ./scripts/inspect-image.sh logfalcon.img
#   ./scripts/inspect-image.sh logfalcon.img.xz   (auto-decompresses)
#   ./scripts/inspect-image.sh                    (downloads latest release image)
#
# Requires: Docker (running), xz (for .xz images), curl
# Works on macOS and Linux. No root needed — runs inside a privileged container.
set -euo pipefail

# ── Colours ──────────────────────────────────────────────────────────────────
RED='\033[0;31m'; YEL='\033[1;33m'; GRN='\033[0;32m'; CYN='\033[0;36m'; NC='\033[0m'
ok()   { echo -e "${GRN}  ✓ $*${NC}"; }
warn() { echo -e "${YEL}  ⚠ $*${NC}"; }
err()  { echo -e "${RED}  ✗ $*${NC}"; }
hdr()  { echo -e "\n${CYN}══ $* ══${NC}"; }

# ── Prereqs ───────────────────────────────────────────────────────────────────
if ! docker info &>/dev/null; then
  err "Docker is not running. Please start Docker Desktop and retry."
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

# ── Resolve image path ────────────────────────────────────────────────────────
IMG_ARG="${1:-}"
IMG_FILE=""

if [[ -z "$IMG_ARG" ]]; then
  echo "No image specified — downloading latest release from GitHub..."
  RELEASE_JSON=$(curl -fsS https://api.github.com/repos/proeugene/logfalcon/releases/latest)
  IMG_URL=$(echo "$RELEASE_JSON" | python3 -c "
import sys, json
r = json.load(sys.stdin)
assets = [a for a in r.get('assets', []) if a['name'].endswith('.img.xz')]
if not assets:
    print('NOT_FOUND')
else:
    print(assets[0]['browser_download_url'])
")
  if [[ "$IMG_URL" == "NOT_FOUND" || -z "$IMG_URL" ]]; then
    err "No .img.xz found in latest release. Build may still be running."
    err "Either wait for CI to finish or pass a local image: $0 /path/to/logfalcon.img"
    exit 1
  fi
  TAG=$(echo "$RELEASE_JSON" | python3 -c "import sys,json; print(json.load(sys.stdin)['tag_name'])")
  IMG_COMPRESSED="$WORK_DIR/logfalcon-${TAG}.img.xz"
  echo "Downloading $TAG image…"
  curl -fL --progress-bar "$IMG_URL" -o "$IMG_COMPRESSED"
  IMG_ARG="$IMG_COMPRESSED"
fi

if [[ ! -f "$IMG_ARG" ]]; then
  err "File not found: $IMG_ARG"
  exit 1
fi

if [[ "$IMG_ARG" == *.xz ]]; then
  echo "Decompressing $(basename "$IMG_ARG")…"
  IMG_FILE="$WORK_DIR/logfalcon.img"
  xz -dk "$IMG_ARG" --stdout > "$IMG_FILE"
else
  IMG_FILE="$IMG_ARG"
fi

echo "Image: $IMG_FILE ($(du -h "$IMG_FILE" | cut -f1))"

# ── Write inner audit script to a temp file ──────────────────────────────────
# (heredoc-via-stdin is unreliable with docker run; temp file is robust)
INNER_SCRIPT="$WORK_DIR/inner.sh"
cat > "$INNER_SCRIPT" << 'INNER_EOF'

RED='\033[0;31m'; YEL='\033[1;33m'; GRN='\033[0;32m'; CYN='\033[0;36m'; NC='\033[0m'
ok()   { echo -e "${GRN}  ✓ $*${NC}"; }
warn() { echo -e "${YEL}  ⚠ $*${NC}"; }
err()  { echo -e "${RED}  ✗ $*${NC}"; }
hdr()  { echo -e "\n${CYN}══ $* ══${NC}"; }

# ── Mount both partitions using offset (works on macOS Docker / any Linux) ──
# Get sector size and partition start offsets from sfdisk
SECTOR_SIZE=512
BOOT_START=$(sfdisk -d /logfalcon.img 2>/dev/null | awk '/start=/{if(NR==2) gsub(",",""); gsub("start=",""); print $4; exit}')
ROOT_START=$(sfdisk -d /logfalcon.img 2>/dev/null | awk '/start=/{if(NR==3) gsub(",",""); gsub("start=",""); print $4; exit}')

# Fallback: parse via fdisk if sfdisk gives unexpected output
if [ -z "$BOOT_START" ] || [ -z "$ROOT_START" ]; then
  BOOT_START=$(fdisk -l /logfalcon.img 2>/dev/null | awk 'NR>10 && /Linux/{print $2; exit}')
  ROOT_START=$(fdisk -l /logfalcon.img 2>/dev/null | awk 'NR>10 && /Linux/{found++; if(found==2){print $2; exit}}')
fi

# More robust: use python to parse sfdisk json output
eval "$(python3 - /logfalcon.img << 'PYEOF'
import sys, json, subprocess
out = subprocess.check_output(['sfdisk', '--json', sys.argv[1]]).decode()
pt = json.loads(out)['partitiontable']
parts = pt['partitions']
print(f"BOOT_START={parts[0]['start']}")
print(f"ROOT_START={parts[1]['start']}")
print(f"SECTOR_SIZE={pt.get('sectorsize', 512)}")
PYEOF
)"

BOOT_OFFSET=$(( BOOT_START * SECTOR_SIZE ))
ROOT_OFFSET=$(( ROOT_START * SECTOR_SIZE ))

BOOT_LOOP=$(losetup -f --show -o "$BOOT_OFFSET" /logfalcon.img)
ROOT_LOOP=$(losetup -f --show -o "$ROOT_OFFSET" /logfalcon.img)

mkdir -p /mnt/root /mnt/boot
mount -o ro "$ROOT_LOOP" /mnt/root
mount -o ro "$BOOT_LOOP" /mnt/boot
echo "Partitions: boot@sector $BOOT_START  root@sector $ROOT_START (sector=$SECTOR_SIZE)"
echo "Mounted: $BOOT_LOOP → /mnt/boot  |  $ROOT_LOOP → /mnt/root"

# ── Cleanup on exit
cleanup() {
  umount /mnt/root /mnt/boot 2>/dev/null || true
  losetup -d "$ROOT_LOOP" "$BOOT_LOOP" 2>/dev/null || true
}
trap cleanup EXIT

# ─────────────────────────────────────────────────────────────────────────────
hdr "OS / kernel"
if [ -f /mnt/root/etc/os-release ]; then
  grep -E "^(PRETTY|VERSION)" /mnt/root/etc/os-release | sed 's/^/  /'
fi

# ─────────────────────────────────────────────────────────────────────────────
hdr "Boot partition contents"
ls /mnt/boot/ | tr '\n' '  '; echo

if [ -f /mnt/boot/logfalcon-config.txt ]; then
  ok "logfalcon-config.txt present"
  grep -v "^#" /mnt/boot/logfalcon-config.txt | grep -v "^$" | sed 's/^/  /'
else
  warn "logfalcon-config.txt MISSING from boot partition"
fi

if [ -f /mnt/boot/userconf.txt ]; then
  ok "userconf.txt present (Bookworm headless user fix)"
else
  warn "userconf.txt MISSING — first boot may prompt for username on HDMI"
fi

# ─────────────────────────────────────────────────────────────────────────────
hdr "hostapd"

echo "  /etc/default/hostapd:"
if [ -f /mnt/root/etc/default/hostapd ]; then
  grep "DAEMON_CONF" /mnt/root/etc/default/hostapd | sed 's/^/    /'
else
  err "  /etc/default/hostapd missing"
fi

echo "  /etc/hostapd/hostapd.conf:"
if [ -f /mnt/root/etc/hostapd/hostapd.conf ]; then
  cat /mnt/root/etc/hostapd/hostapd.conf | sed 's/^/    /'
else
  err "  /etc/hostapd/hostapd.conf MISSING"
fi

if systemctl --root=/mnt/root is-enabled hostapd &>/dev/null; then
  ok "hostapd.service is ENABLED"
else
  warn "hostapd.service is NOT enabled — it won't start on boot!"
  echo "  Checking symlinks manually..."
  ls -la /mnt/root/etc/systemd/system/multi-user.target.wants/ 2>/dev/null | grep hostapd || \
    echo "    no hostapd symlink found"
fi

echo "  hostapd.service unit:"
HOST_UNIT=""
for p in /mnt/root/lib/systemd/system/hostapd.service \
          /mnt/root/usr/lib/systemd/system/hostapd.service; do
  [ -f "$p" ] && HOST_UNIT="$p" && break
done
if [ -n "$HOST_UNIT" ]; then
  grep -E "^(After|Requires|Before|ExecStart|ConditionPathExists)" "$HOST_UNIT" | sed 's/^/    /'
else
  err "  hostapd.service unit file not found!"
fi

# ─────────────────────────────────────────────────────────────────────────────
hdr "dnsmasq"

echo "  /etc/dnsmasq.conf (relevant lines):"
if [ -f /mnt/root/etc/dnsmasq.conf ]; then
  grep -v "^#" /mnt/root/etc/dnsmasq.conf | grep -v "^$" | sed 's/^/    /'
else
  warn "  /etc/dnsmasq.conf not present"
fi

echo "  /etc/dnsmasq.d/logfalcon.conf:"
if [ -f /mnt/root/etc/dnsmasq.d/logfalcon.conf ]; then
  cat /mnt/root/etc/dnsmasq.d/logfalcon.conf | sed 's/^/    /'
else
  err "  /etc/dnsmasq.d/logfalcon.conf MISSING"
fi

if systemctl --root=/mnt/root is-enabled dnsmasq &>/dev/null; then
  ok "dnsmasq.service is ENABLED"
else
  warn "dnsmasq.service is NOT enabled"
fi

echo "  dnsmasq.service unit After= (ordering):"
DNS_UNIT=""
for p in /mnt/root/lib/systemd/system/dnsmasq.service \
          /mnt/root/usr/lib/systemd/system/dnsmasq.service; do
  [ -f "$p" ] && DNS_UNIT="$p" && break
done
if [ -n "$DNS_UNIT" ]; then
  grep -E "^(After|Before|Requires)" "$DNS_UNIT" | sed 's/^/    /'
fi

# ─────────────────────────────────────────────────────────────────────────────
hdr "dhcpcd / networking (wlan0 static IP)"

echo "  dhcpcd.conf (wlan0 section):"
if [ -f /mnt/root/etc/dhcpcd.conf ]; then
  grep -A5 "wlan0" /mnt/root/etc/dhcpcd.conf | sed 's/^/    /'
else
  warn "  /etc/dhcpcd.conf not found — may be using systemd-networkd"
  if [ -f /mnt/root/etc/systemd/network/10-wlan0-static.network ]; then
    ok "  systemd-networkd config found:"
    cat /mnt/root/etc/systemd/network/10-wlan0-static.network | sed 's/^/    /'
  fi
fi

# ─────────────────────────────────────────────────────────────────────────────
hdr "rfkill / Wi-Fi unblock"

echo "  Looking for rfkill unblock in firstboot / rc.local:"
for f in /mnt/root/etc/rc.local \
          /mnt/root/etc/systemd/system/logfalcon-firstboot.service \
          /mnt/root/lib/systemd/system/logfalcon-firstboot.service; do
  [ -f "$f" ] && grep -l "rfkill" "$f" 2>/dev/null && grep "rfkill" "$f" | sed 's/^/    /'
done || true

# ─────────────────────────────────────────────────────────────────────────────
hdr "logfalcon service"

SVC=""
for p in /mnt/root/etc/systemd/system/logfalcon.service \
          /mnt/root/lib/systemd/system/logfalcon.service; do
  [ -f "$p" ] && SVC="$p" && break
done
if [ -n "$SVC" ]; then
  ok "logfalcon.service found"
  cat "$SVC" | sed 's/^/  /'
else
  err "logfalcon.service NOT FOUND"
fi

if systemctl --root=/mnt/root is-enabled logfalcon &>/dev/null; then
  ok "logfalcon.service is ENABLED"
else
  warn "logfalcon.service is NOT enabled"
fi

# ─────────────────────────────────────────────────────────────────────────────
hdr "logfalcon binary"

BINARY=""
for p in /mnt/root/opt/logfalcon/logfalcon \
          /mnt/root/usr/local/bin/logfalcon \
          /mnt/root/usr/bin/logfalcon; do
  [ -f "$p" ] && BINARY="$p" && break
done
if [ -n "$BINARY" ]; then
  ok "Binary: $BINARY ($(du -h "$BINARY" | cut -f1), $(file "$BINARY" | cut -d: -f2 | xargs))"
else
  err "logfalcon binary NOT FOUND in /usr/local/bin or /usr/bin"
fi

# ─────────────────────────────────────────────────────────────────────────────
hdr "logfalcon config"

TOML=""
for p in /mnt/root/etc/logfalcon/logfalcon.toml /mnt/root/etc/logfalcon.toml; do
  [ -f "$p" ] && TOML="$p" && break
done
if [ -n "$TOML" ]; then
  ok "Config: $TOML"
  cat "$TOML" | sed 's/^/  /'
else
  warn "logfalcon.toml not found in /etc/logfalcon/ or /etc/"
fi

# ─────────────────────────────────────────────────────────────────────────────
hdr "Enabled services summary"

echo "  multi-user.target.wants:"
ls /mnt/root/etc/systemd/system/multi-user.target.wants/ 2>/dev/null | sort | sed 's/^/    /'

# ─────────────────────────────────────────────────────────────────────────────
hdr "Potential issues summary"

ISSUES=0

if ! [ -f /mnt/root/etc/hostapd/hostapd.conf ]; then
  err "CRITICAL: hostapd.conf missing — no hotspot possible"
  ISSUES=$((ISSUES+1))
fi

CONF=$(cat /mnt/root/etc/default/hostapd 2>/dev/null)
if ! echo "$CONF" | grep -q '^DAEMON_CONF=.*/hostapd.conf'; then
  err "CRITICAL: /etc/default/hostapd does not set DAEMON_CONF — hostapd starts with no config"
  ISSUES=$((ISSUES+1))
fi

if ! systemctl --root=/mnt/root is-enabled hostapd &>/dev/null; then
  err "CRITICAL: hostapd.service not enabled"
  ISSUES=$((ISSUES+1))
fi

if ! systemctl --root=/mnt/root is-enabled dnsmasq &>/dev/null; then
  warn "WARNING: dnsmasq not enabled (phones won't get IP from hotspot)"
  ISSUES=$((ISSUES+1))
fi

if ! [ -f /mnt/root/etc/dnsmasq.d/logfalcon.conf ]; then
  err "dnsmasq config missing — DHCP won't work"
  ISSUES=$((ISSUES+1))
fi

if [ -L /mnt/root/etc/systemd/system/multi-user.target.wants/wpa_supplicant.service ]; then
  err "CRITICAL: wpa_supplicant.service is enabled — it will fight hostapd for wlan0"
  ISSUES=$((ISSUES+1))
fi

if [ -L /mnt/root/etc/systemd/system/multi-user.target.wants/userconfig.service ]; then
  warn "WARNING: userconfig.service enabled — first boot will prompt for username on HDMI"
  ISSUES=$((ISSUES+1))
fi

if ! [ -f /mnt/boot/userconf.txt ]; then
  warn "WARNING: userconf.txt missing from boot — headless setup may prompt for username"
  ISSUES=$((ISSUES+1))
fi

if [ $ISSUES -eq 0 ]; then
  echo -e "${GRN}  No obvious config issues found.${NC}"
else
  echo -e "${YEL}  Found $ISSUES issue(s) — see above.${NC}"
fi

INNER_EOF

# ── Run audit inside a privileged Linux container ────────────────────────────
hdr "Mounting image partitions and auditing"

docker run --rm --privileged \
  -v "$IMG_FILE:/logfalcon.img:ro" \
  -v "$INNER_SCRIPT:/inner.sh:ro" \
  debian:bookworm-slim bash -euo pipefail /inner.sh

echo -e "\n${GRN}Inspection complete.${NC}"
