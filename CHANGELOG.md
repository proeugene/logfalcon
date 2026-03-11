# Changelog

All notable changes to LogFalcon are documented here.

## [v0.4.5] — 2026-03

### Fixed — FC detection restored + broader FC support (CP2102, CH340)

**FC was never detected (OTG port stuck in USB device mode):**
In v0.4.2, `dtoverlay=dwc2` + `modules-load=dwc2,g_ether` were added to enable USB gadget SSH debugging. Loading `g_ether` puts the Pi's OTG port into USB **device/peripheral mode** — in this mode the Pi presents itself as a USB network adapter and cannot enumerate any devices plugged into it. The FC was invisible to udev and the sync service never triggered.

Fix: removed `g_ether` from the default image. The OTG port now operates in host mode, as required for FC detection. SSH via Wi-Fi hotspot (`ssh pi@192.168.4.1` with password `logfalcon`) is the standard access method. USB gadget SSH is documented as an opt-in advanced feature.

**Added support for CP2102 and CH340 USB-to-serial FCs:**
The udev rule previously only matched STM32 native USB (VID `0x0483`), used by most modern F4/F7/H7 boards. Many budget FCs and older designs use CP2102 or CH340 USB-to-serial bridge chips. These now trigger sync automatically:
- CP2102 (Silicon Labs, VID `0x10c4` PID `0xea60`) → appears as `ttyUSB*`
- CH340/CH341 (WinChipHead, VID `0x1a86` PID `0x7523`/`0x7522`) → appears as `ttyUSB*`

---

## [v0.4.4] — 2026-03


### Fixed — SSH password (pi/logfalcon) and captive portal URL routing

**SSH password was never being set (account locked):**
In v0.4.3 we masked `userconfig.service` to prevent the Bookworm first-boot wizard. This was wrong — `userconfig.service` is also responsible for reading `/boot/firmware/userconf.txt` and applying the password. With it masked, the `pi` account was left locked, making SSH impossible with any password.

Fix: removed the masking. `userconf.txt` (with the `logfalcon` password hash) is still created at build time. When `userconf.txt` exists, the service applies it **silently** with no interactive wizard. The wizard only appears when the file is missing.

**SSH credentials: `pi` / `logfalcon`**

**Any URL now works while connected to LogFalcon WiFi:**
iOS and Android route all app traffic over cellular when they detect "no internet" on a WiFi network. Our captive portal probe handlers were returning a redirect page, which told the OS "no internet here". After the user dismissed the captive portal popup once, all subsequent traffic went over cellular — making every URL unreachable on the hotspot.

Fix: captive probe paths now return the OS-specific "internet OK" responses:
- `/hotspot-detect.html`, `/library/test/success.html` → Apple success HTML (iOS/macOS)
- `/generate_204`, `/gen_204` → HTTP 204 (Android)
- `/ncsi.txt` → `Microsoft NCSI` (Windows)
- `/connecttest.txt` → `Microsoft Connect Test` (Windows)

iOS/Android now keep all traffic on the Wi-Fi interface. Since dnsmasq resolves all DNS to `192.168.4.1`, any URL in the phone's browser hits our server and shows the LogFalcon dashboard.

---

## [v0.4.3] — 2026-03

### Fixed — Wi-Fi hotspot (again): NetworkManager was taking over wlan0

The root cause of the persistent "no hotspot" issue was **NetworkManager** — Raspberry Pi OS Bookworm installs and enables it by default. NetworkManager takes over `wlan0` in managed/client mode before hostapd can start an AP, silently preventing the hotspot from ever appearing.

Additionally, `userconfig.service` (the "enter username" first-boot wizard) was being re-enabled by pi-gen's export-image stage, which installs `userconf-pi` **after** our custom stage. The previous `rm -f` fix was being overwritten.

**Fixes:**
1. **NetworkManager masked** — `NetworkManager.service`, `NetworkManager-wait-online.service`, and `ModemManager.service` are all masked (`→ /dev/null`). NetworkManager can no longer manage `wlan0`.
2. **Belt-and-suspenders NM config** — `/etc/NetworkManager/conf.d/logfalcon-unmanaged.conf` marks `wlan0` and `usb0` as unmanaged, even if NM were un-masked by a future package install.
3. **`userconfig.service` properly masked** — now masked via `ln -sf /dev/null /etc/systemd/system/userconfig.service` so it survives pi-gen's export-image stage reinstalling `userconf-pi`.
4. **`logfalcon-wifi-init.service`** now also runs `Before=systemd-networkd.service` so rfkill is unblocked and the regulatory domain is set before anything tries to configure wlan0.

## [v0.4.2] — 2026-03

### Added
- **USB gadget SSH baked into every image** — `dtoverlay=dwc2` added to `config.txt` and `modules-load=dwc2,g_ether` added to `cmdline.txt` at build time. No manual configuration needed. Connect Pi's OTG data port to Mac, set Mac adapter to `10.55.55.2`, then `ssh pi@10.55.55.1`.

---

## [v0.4.1] — 2026-03

### Fixed — Wi-Fi hotspot on Pi Zero 2 W

Three root causes diagnosed via USB gadget inspection:

1. **`rfkill unblock` ran at build time** — completely ineffective; rfkill resets on every boot. Replaced with `logfalcon-wifi-init.service`, a oneshot systemd unit that runs `rfkill unblock all` before hostapd on every boot.

2. **Regulatory domain never set at runtime** — `country_code=US` in `hostapd.conf` is not enough. The Linux wireless subsystem (`cfg80211`) needs the country set at the kernel level before hostapd can start AP mode. On Pi Zero 2 W this blocked AP startup entirely. Fix: `wifi-init` service runs `iw reg set US`; also writes a minimal `/etc/wpa_supplicant/wpa_supplicant.conf` with `country=US` so `cfg80211` picks it up from the file.

3. **`wpa_supplicant@wlan0.service`** (per-interface instance unit, used by Pi Zero 2 W Bookworm) was not disabled — only the main unit was removed. Fix: both the instance and main unit are now removed **and masked** (`→ /dev/null`) so they cannot be activated by D-Bus or dependency pulls.

### Added
- **USB gadget SSH** (`10.55.55.1`) — `usb0` now gets a static IP via dhcpcd so SSH debugging over USB OTG works without needing the hotspot. Plug the Pi's data USB port into your Mac, set your Mac adapter to `10.55.55.2`, then `ssh pi@10.55.55.1`.

---

## [v0.4.0] — 2025-03

### Added
- **Hard block for too-old firmware**: Betaflight older than 4.0 (MSP API < 1.41) and iNav older than 2.6 (API < 1.40) are rejected at identification time with a clear, actionable error message. This prevents log corruption — `MSP_DATAFLASH_READ` changed wire format in BF 4.0.
- **Soft warning for untested-new firmware**: firmware above the max tested version (BF 4.6 / iNav 7.0) shows an amber banner in the dashboard. Sync proceeds — the warning is informational.
- **FC identity in dashboard**: after MSP handshake completes, the dashboard shows `⚡ FC: Betaflight 4.5.0 (API 1.46)`. Clears automatically at the start of each new sync cycle.

---

## [v0.3.9] — 2025-02

### Fixed
- **Wi-Fi hotspot not visible on first boot** — three root causes diagnosed via image inspection:
  1. `wpa_supplicant` was enabled by default in Bookworm Lite, stealing `wlan0` from `hostapd`; symlink now removed directly in pi-gen chroot
  2. `openssl passwd -6 -stdin` outputs nothing on Linux — changed to `openssl passwd -6 'logfalcon'` (portable direct argument)
  3. `dnsmasq` started before `wlan0` was configured — added systemd drop-in override (`After=sys-subsystem-net-devices-wlan0.device`, `Restart=on-failure`) for both `dnsmasq` and `hostapd`

### Added
- `scripts/inspect-image.sh` — mounts any `.img` / `.img.xz` via Parallels VM or Docker and audits hotspot config, service states, and binary presence

---

## [v0.3.8] — 2025-02

### Changed
- Wi-Fi SSID default renamed from `BF-Blackbox` → `LogFalcon` across Go code, config templates, pi-gen, install scripts, and all docs. Existing installs are unaffected (SSID is read from the deployed config file).

---

## [v0.3.7] — 2025-01

### Fixed
- Binary artifacts now correctly attached to GitHub Releases (was broken — `go-ci.yml` never triggered on tag pushes)
- `golangci-lint` pinned to `v1.64.8` (was `latest`)
- `xz -9e` → `xz -9` in image build (saves ~5–10 min per release)

### Added
- Real-time sync progress documented in README and guide (state badge colours, progress bar, speed, ETA)

---

## [v0.3.6] — 2025-01

### Added
- **Real-time sync progress**: web dashboard now shows `Syncing flash… 45%  (2.1 / 4.0 MB)  · 1.2 MB/s · ~18s remaining` with a live progress bar
- **SSH access documented**: credentials, `passwd`, SSH connection strings added to README and guide

### Fixed
- **Headless boot on Bookworm** (`userconfig.service` wizard): belt-and-suspenders fix — pi-gen sets `FIRST_USER_PASS`, `userconf.txt` written to boot partition, `userconfig.service` disabled in chroot

---

## [v0.3.5] — 2024-12

### Added
- `scripts/install.sh` — one-command installer for existing Raspberry Pi OS installs (`curl ... | sudo bash`)
- `scripts/uninstall.sh` — clean one-command uninstaller

---

## [v0.3.4] — 2024-12

### Performance
- **MSP v2 flash reads**: 16-bit length field → ~4 KB frames (was 255 B) — ~16× per-frame throughput
- **Baud rate 921,600**: 8× raw UART throughput
- **Estimated sync times (16 MB flash)**: UART ~2.5 min (was ~20 min), USB ~30–60 sec (was ~5–9 min)

---

## [v0.3.1] — 2024-11

### Changed
- Removed all legacy Python code; Go rewrite is now the sole implementation
- Multi-stage Dockerfile (golang:1.22-alpine → alpine:3.20)
- pi-gen no longer installs Python — copies pre-built Go binary instead
- All packages migrated from `log.Printf` to `slog`

### Added
- `docs/guide.html` — 9-section field pilot guide

---

## [v0.3.0] — 2024-11

### Added
- Complete rewrite from Python to Go
  - ~6 MB single static binary (was ~250 MB Python venv)
  - ~10 ms cold start (was 500 ms–2 s)
  - ~5–10 MB idle memory (was 45–55 MB)
  - Zero runtime dependencies on Pi
- Race-detector tested: all tests pass with `go test -race`
