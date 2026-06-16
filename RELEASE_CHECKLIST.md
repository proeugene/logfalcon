# Release Checklist

## Tag format

```shell
git tag -a v0.5.0 -m "Release v0.5.0"
git push origin v0.5.0
```

Tags must start with `v` followed by a semver version. The CI release workflow
triggers on `v*` tag pushes.

## CI release workflow

`.github/workflows/release.yml` runs on every `v*` tag push and on
`workflow_dispatch` (for testing).

Pipeline stages:

1. **Lint** — golangci-lint
2. **Test** — `go test -race -count=1 ./...`
3. **Govulncheck** — known CVE scan
4. **Build binaries** — arm6, arm64, amd64 (with 10 MB size gate)
5. **Build SD card image** — pi-gen + PiShrink + xz (with 600 MB size gate)
6. **Inspect image** — gating for tag releases, non-gating for manual runs
7. **Release** — asserts all 5 assets present, publishes GitHub Release

## Expected release assets

Every `v*` tag release must contain:

| Asset                    | Description                       |
|--------------------------|-----------------------------------|
| `logfalcon-arm6`         | ARMv6 binary (Pi Zero W)          |
| `logfalcon-arm64`        | ARM64 binary (Pi Zero 2 W)        |
| `logfalcon-amd64`        | AMD64 binary                      |
| `logfalcon-*.img.xz`     | Compressed Raspberry Pi OS image  |
| `checksums.txt`          | SHA-256 checksums for all assets  |

The release job asserts these files exist before uploading.
If any asset is missing the release is blocked with an `::error` annotation.

## Pre-release smoke tests

Run these on a Pi Zero W or Zero 2 W with the flashed image before publishing.

### Image smoke test

1. Flash the `.img.xz` to a microSD card (16 GB+):

   ```bash
   xz -d logfalcon-*.img.xz
   sudo dd if=logfalcon-*.img of=/dev/sdX bs=4M status=progress conv=fsync
   ```

2. Insert the card, power on the Pi, and wait ~90 seconds for boot.

3. From your laptop, connect to the `LogFalcon` Wi-Fi (password `fpvpilot`).

4. Verify the web UI loads:

   ```bash
   curl -s http://192.168.4.1/health | python3 -m json.tool
   ```

   Expected: JSON with `"ok": true`.

5. Verify SSH:

   ```bash
   ssh pi@logfalcon.local  # password: logfalcon
   ```

6. Verify the sync LED is solid green (no blinking after boot).

### First boot test

```bash
# On the Pi via SSH:

# Services should be running
systemctl is-active logfalcon-web hostapd dnsmasq avahi-daemon

# config-apply should have completed
systemctl status logfalcon-config-apply.service

# Verify hostapd is running with the correct config
systemctl status hostapd --no-pager
cat /etc/hostapd/hostapd.conf | head -5

# Verify the expected binary is installed
file /opt/logfalcon/logfalcon
/opt/logfalcon/logfalcon --version
```

### Web settings save test

1. Connect to the `LogFalcon` hotspot.

2. Open `http://log.falcon` (or `http://192.168.4.1`).

3. Go to Settings, change the SSID to `TestFalcon` and save.

4. On the Pi, verify:

   ```bash
   # hostapd.conf should reflect the new SSID
   grep "^ssid=" /etc/hostapd/hostapd.conf
   # Expected: ssid=TestFalcon

   # logfalcon.toml should reflect the new SSID
   grep "^hotspot_ssid" /etc/logfalcon/logfalcon.toml
   # Expected: hotspot_ssid = "TestFalcon"

   # hostapd should have been restarted
   systemctl is-active hostapd
   # Expected: active
   ```

5. Change SSID back to `LogFalcon` and save to restore defaults.

6. Verify `bbsyncer` can restart `hostapd` without sudo:

   ```bash
   sudo -u bbsyncer systemctl restart hostapd.service
   # Expected: success (polkit rule allows this)
   ```

### Verify manifest durability

```bash
# Sync with a Betaflight FC, then check:
ls -la /mnt/logfalcon-logs/fc_*/20*/manifest.json

# No .tmp files should exist
find /mnt/logfalcon-logs -name "*.tmp"

# Manifest should be valid JSON
python3 -m json.tool /mnt/logfalcon-logs/fc_*/20*/manifest.json
```
