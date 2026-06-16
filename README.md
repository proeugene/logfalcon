# LogFalcon

[![CI](https://github.com/proeugene/logfalcon/actions/workflows/go-ci.yml/badge.svg)](https://github.com/proeugene/logfalcon/actions/workflows/go-ci.yml)
[![Latest Release](https://img.shields.io/github/v/release/proeugene/logfalcon?label=download&color=success)](https://github.com/proeugene/logfalcon/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**Clear your FC's blackbox flash in the field. No laptop. No dongles. Keep flying.**

LogFalcon is a Betaflight & iNav companion tool — a tiny Raspberry Pi Zero W that copies and clears your flight controller's blackbox data in ~30 seconds, so you never have to stop your session.

---

## 😤 The Problem

Your FC's SPI flash is small. A few packs and it's full — mid-session, logging just stops.

Your options today:

| Option | The catch |
|--------|-----------|
| 🖥️ Laptop + Configurator | Haul a laptop to the field, cable up, manually export or erase |
| 🔌 Third-party USB dongle | Extra hardware to buy, carry, and keep charged |
| 🤷 Fly without logs | Lose your blackbox data entirely |

All three mean either **leaving the field**, **carrying extra gear**, or **losing data**.

---

## 🦅 The Solution

A Pi Zero W in your bag. That's it.

> **Plug in → LED → Fly again.**  
> ~30 seconds. Repeat all session. Logs are timestamped and ready at home.

Your FC's flash is **never erased** until the copy is verified with SHA-256. Every sync creates a timestamped folder on the Pi, organised by FC. When you get home, connect your phone to the Pi's Wi-Fi to download everything.

---

## 🔄 How It Works — The Pilot's Flow

```
 ┌──────────────────────────────────────────────────────┐
 │                                                      │
 │   ① FC flash full mid-session? Land your quad.       │
 │                    ↓                                  │
 │   ② Plug FC into Pi Zero W (USB OTG cable)           │
 │      (solid LED = ready)                             │
 │                    ↓                                  │
 │   ③ Watch the LED — about 30 seconds                 │
 │      steady blink = working → long solid = DONE ✓   │
 │                    ↓                                  │
 │   ④ Unplug. Fly again. Repeat as needed.             │
 │                                                      │
 └──────────────────────────────────────────────────────┘
```

Later, from any phone: connect to **`LogFalcon`** Wi-Fi → open **`http://log.falcon`** → download `.bbl` files → open in Blackbox Explorer.

---

## 🚀 Getting Started

### Step 1 — Download the image

Grab the latest release from [**Releases**](https://github.com/proeugene/logfalcon/releases/latest). Each `v*` tag release includes:

| Asset | Description |
|-------|-------------|
| `logfalcon-*.img.xz` | Pre-built Raspberry Pi OS image (flash to SD, boot, done) |
| `logfalcon-arm6` | ARMv6 binary (Pi Zero W) |
| `logfalcon-arm64` | ARM64 binary (Pi Zero 2 W) |
| `logfalcon-amd64` | AMD64 binary (dev/testing on x86_64) |
| `checksums.txt` | SHA-256 checksums for all assets |

For most pilots, download the `.img.xz` file and flash it to a microSD card.

### Step 2 — Burn to microSD

Use [Raspberry Pi Imager](https://www.raspberrypi.com/software/) or [Balena Etcher](https://etcher.balena.io/). Any 16 GB+ card works.

### Step 3 — (Optional) Customise Wi-Fi

Before ejecting the SD card, open the `boot` partition and edit **`logfalcon-config.txt`**:

```ini
SSID=LogFalcon
PASSWORD=your-password
```

> 💡 Default password is `fpvpilot` — change it before flying at a shared field.
> You can also change it later from the web UI (connect to the hotspot → ⚙ Settings).

### Step 4 — Insert, power on, fly

Put the SD card in your Pi Zero W, power it with a USB battery bank. Wait ~90 seconds for boot. Done — the Pi is ready for your FC.

---

## 🔧 Install on Existing Pi

Already running Raspberry Pi OS on your Pi Zero W? Install LogFalcon with one command:

```bash
curl -sSL https://github.com/proeugene/logfalcon/raw/main/scripts/install.sh | sudo bash
```

This automatically:
- Downloads the correct ARM binary from GitHub Releases
- Installs hostapd, dnsmasq, avahi-daemon
- Sets up Wi-Fi hotspot, systemd services, udev auto-trigger
- Configures LED feedback and boot optimizations

To uninstall:
```bash
curl -sSL https://github.com/proeugene/logfalcon/raw/main/scripts/uninstall.sh | sudo bash
```

> 💡 You can also set a specific version: `LOGFALCON_VERSION=v0.4.6 curl -sSL ... | sudo bash`

---

## 🔐 SSH Access

LogFalcon ships with SSH enabled so you can administer the Pi from your laptop or phone.

| | |
|---|---|
| **Hostname** | `logfalcon.local` (or `192.168.4.1` when connected to the hotspot) |
| **Username** | `pi` |
| **Default password** | `logfalcon` |

```bash
ssh pi@logfalcon.local
```

**Change your password immediately** — especially before flying at a shared field:

```bash
passwd
```

**Useful SSH tasks:**
- Check the sync log: `journalctl -u "logfalcon@*" -f`
- Browse log files: `ls /mnt/logfalcon-logs/`
- Manual update: see [Development & Building](#development--building) below

> 🔑 The Wi-Fi hotspot password is separate: **`fpvpilot`** (SSID `LogFalcon`).  
> SSH password and hotspot password are independent — changing one does not affect the other.

---

## 🛒 What You Need

| Part | Where to get it |
|------|-----------------|
| **[Raspberry Pi Zero 2 W](https://www.raspberrypi.com/products/raspberry-pi-zero-2-w/)** | [Raspberry Pi](https://www.raspberrypi.com/products/raspberry-pi-zero-2-w/) · [Adafruit](https://www.adafruit.com/product/5291) · [Pimoroni](https://shop.pimoroni.com/products/raspberry-pi-zero-2-w) · Amazon |
| **microSD card** (16 GB+) | Any Class 10 or faster — SanDisk, Samsung, etc. |
| **[USB OTG cable](https://www.amazon.com/s?k=micro+usb+otg+cable)** | Micro-USB → USB-A female (short is better) |
| **[USB-A to micro-USB cable](https://www.amazon.com/s?k=usb+a+to+micro+usb+cable+short)** | Connects the OTG adapter to your FC |
| **USB battery bank** | Any 5V/1A+ bank — one you already have is fine |

> ⚠️ **Pi Zero has two micro-USB ports:**  
> **Inner port** = OTG/data → plug your FC here  
> **Outer port** = PWR_IN → plug your battery bank here

> 💡 **Pi Zero W vs Zero 2 W:** Both work. Zero 2 W syncs ~2× faster (~30 s vs ~60 s on a full 16 MB flash).

---

## 💡 LED Guide

Only four patterns — unmistakable at a glance, even in direct sunlight:

| LED | Meaning | What to do |
|-----|---------|------------|
| 💛 Slow pulse (1 s on / 1 s off) | Pi is booting up | Wait ~60 s |
| 🟢 Solid on | **Ready** — Pi booted, waiting for FC | Plug in your FC |
| ⚡ Steady blink (fast) | Sync in progress — copying, verifying, or erasing | **Don't unplug** |
| ✅ Rapid burst → 3 s solid → back to solid | **Done — safe to unplug** | Unplug and fly! |
| 🆘 SOS pattern (repeating) | Error — something went wrong | Check the web UI for details |

---

## 📱 Downloading Your Logs

1. **Connect** your phone or laptop to the **`LogFalcon`** Wi-Fi network
2. **Your phone automatically opens the log browser** (captive portal, like airport Wi-Fi)
3. **Browse** your sessions — grouped by FC, sorted by date
4. **Tap Download** → open `.bbl` in [Blackbox Explorer](https://github.com/betaflight/blackbox-log-viewer)

> If the captive portal doesn't pop up, type **`http://log.falcon`** in any browser — it works on any device connected to the hotspot.

While the FC is plugged in and syncing, the dashboard shows **real-time progress** — you can watch it update live from your phone:

```
┌─────────────────────────────────────────────────┐
│  LogFalcon                    ⚙  [Syncing 45%] │
├─────────────────────────────────────────────────┤
│  Syncing flash… 45%  (2.1 / 4.0 MB)            │
│  ████████████░░░░░░░░░░░░░░░░░                  │
│                          1.2 MB/s · ~18s left   │
└─────────────────────────────────────────────────┘
```

After the sync completes, the dashboard switches to the log browser:

```
┌─────────────────────────────────────────────────┐
│  LogFalcon                    ⚙    [Idle]       │
├─────────────────────────────────────────────────┤
│  fc_BTFL_uid-12ab34cd                           │
│  ─────────────────────────────────────────────  │
│  2026-03-01 09:10  2.1 MB  ✓ Erased            │
│  [Download .bbl]  [Manifest]  [Delete from Pi]  │
│                                                  │
│  fc_INAV_uid-aabb1122                           │
│  ─────────────────────────────────────────────  │
│  2026-03-02 10:15  1.5 MB  ✓ Erased            │
│  [Download .bbl]  [Manifest]  [Delete from Pi]  │
├─────────────────────────────────────────────────┤
│  Pi SD card: 12.3 GB used / 28.7 GB free       │
└─────────────────────────────────────────────────┘
```

---

## ✅ FC Compatibility

LogFalcon is an independent add-on — not affiliated with or endorsed by the Betaflight or iNav projects.

| | |
|---|---|
| **Firmware** | Betaflight 4.0+ · iNav 2.6+ (requires MSP v2) |
| **Blackbox device** | **SPI Flash only** — the most common setup |
| **Flash chips** | W25Q128FV, W25Q64FV, M25P16 (covers the vast majority of FCs) |
| **Not supported** | FC-side SD card blackbox · Betaflight < 4.0 · Ardupilot |

> **How to check:** In Betaflight/iNav Configurator → **Blackbox** tab. If it shows `FLASH` with a size (16M, 64M, 128M), you're good. If it shows `SD CARD` or `NONE`, LogFalcon can't read it.

---

## ⚙️ Configuration

The config file lives at `/etc/logfalcon/logfalcon.toml`. Defaults work out of the box:

```toml
erase_after_sync = true               # Set false to copy without erasing
hotspot_ssid = "LogFalcon"
hotspot_password = "fpvpilot"          # Change this!
storage_path = "/mnt/logfalcon-logs"   # Where logs are stored
min_free_space_mb = 200                # Always keep this much free
storage_pressure_cleanup = true        # Auto-delete oldest when full
```

---

## 🔧 Troubleshooting

<details>
<summary><strong>LED shows SOS / error pattern</strong></summary>

```bash
journalctl -u "logfalcon@ttyACM0" -n 50
```
</details>

<details>
<summary><strong>FC not detected (no LED response)</strong></summary>

- Make sure you're using the Pi's **inner** micro-USB port (OTG), not the power port
- Confirm your FC shows up as a serial port on a normal PC (`/dev/ttyACM0` for STM32, `/dev/ttyUSB0` for CP2102/CH340)
- Check the FC USB VID: `lsusb` — look for `0483` (STM32), `10c4` (CP2102), or `1a86` (CH340)
- Try a shorter or better-quality USB cable
</details>

<details>
<summary><strong>Web UI not loading</strong></summary>

```bash
journalctl -u logfalcon-web -f
```
If the dashboard doesn't load, try `http://log.falcon` or `http://192.168.4.1` directly.
</details>

<details>
<summary><strong>Can't SSH in / "connection refused"</strong></summary>

SSH is enabled by default. If you can't connect:

```bash
# From the Pi console or via web serial
sudo systemctl enable ssh --now
```

Default credentials: user `pi`, password `logfalcon`.  
If you changed the password and forgot it, you'll need to reflash the image.
</details>

<details>
<summary><strong>First boot asks for username/password on screen</strong></summary>

This shouldn't happen with the LogFalcon image — the `pi` user is pre-configured and the first-boot wizard is disabled. If you see it anyway, enter username `pi` and password `logfalcon`, then run `sudo raspi-config` to finish setup.
</details>

<details>
<summary><strong>Sync seems slow</strong></summary>

The Pi Zero W's single-core CPU is the bottleneck. Typical times:

| Flash size | Time |
|-----------|------|
| 1 MB | ~10–20s |
| 2 MB | ~30–40s |
| 4 MB | ~50–80s |

Pi Zero **2** W is about 2× faster. Also try a shorter USB cable.
</details>

<details>
<summary><strong>"FC uses SD card" error</strong></summary>

Your FC logs to an SD card, not internal flash. MSP can't read FC-side SD cards. In Configurator, set **Blackbox Device = SPI Flash**, or remove the FC's SD card and read it directly.
</details>

---

## 📂 How Logs Are Stored

```
/mnt/logfalcon-logs/
├── fc_BTFL_uid-12ab34cd/            ← Betaflight FC (by UID)
│   ├── 2026-02-26_143012/
│   │   ├── raw_flash.bbl            ← open directly in Blackbox Explorer
│   │   └── manifest.json            ← FC info, file size, SHA-256, erase status
│   ├── 2026-02-26_161500/
│   └── 2026-03-01_091000/
├── fc_INAV_uid-aabb1122/            ← iNav FC → separate directory
│   └── 2026-03-02_101500/
│       ├── raw_flash.bbl
│       └── manifest.json
└── fc_BTFL_uid-deadbeef/            ← different Betaflight FC
    └── ...
```

---

## 🏭 Production Deployment & Hardware Setup

| Board | Image / binary | Notes |
|-------|----------------|-------|
| Pi Zero W | `logfalcon-*.img.xz` (ARMv6 image) or `logfalcon-arm6` binary | Best compatibility, 512 MB RAM |
| Pi Zero 2 W | `logfalcon-*.img.xz` (ARMv6 image works; `logfalcon-arm64` binary for native speed) | ~2× faster sync |

**microSD card:** Quality 16 GB+ A1/A2 card (SanDisk Max Endurance, Samsung PRO Endurance recommended). Avoid pulling power during a sync — wait for the LED to go solid.

**Logs and monitoring:**

```bash
# Quick health check (exit 0 = healthy, prints status line)
/opt/logfalcon/healthcheck.sh

# Service logs
journalctl -u logfalcon-web -f              # web server
journalctl -u "logfalcon@ttyACM0" -f         # sync (per FC)

# CPU temperature
cat /sys/class/thermal/thermal_zone0/temp    # millidegrees C (e.g. 52000 = 52.0°C)
# or: curl -s http://127.0.0.1/health | python3 -m json.tool
```

**Log storage:** `/mnt/logfalcon-logs/` — mount a dedicated partition for best results.

**Journald:** Bounded to 50 MB by default (pi-gen image). Do not enable verbose debug logging in production — it fills the SD card.

---

<details>
<summary><h2>🛠️ Developer Guide</h2></summary>

### Developer Install

For contributors or manual Pi OS installs:

```bash
git clone https://github.com/proeugene/logfalcon
cd logfalcon
make build-pi            # ARM6 for Pi Zero W
# or: make build-pi2     # ARM64 for Pi Zero 2 W
scp bin/logfalcon-arm6 pi@logfalcon.local:/tmp/
ssh pi@logfalcon.local 'sudo install -m 755 /tmp/logfalcon-arm6 /opt/logfalcon/logfalcon && sudo systemctl restart logfalcon-web'
```

Or use the install script for a fresh setup: `sudo bash scripts/install.sh`

### Development Setup

```bash
git clone https://github.com/proeugene/logfalcon
cd logfalcon
go mod download
```

Requires Go 1.23+.

### Commands

```bash
make test                   # Run tests with race detector
make lint                   # Run golangci-lint
make build                  # Build native binary
make build-pi               # Cross-compile for Pi Zero W (ARM6)
make build-pi2              # Cross-compile for Pi Zero 2 W (ARM64)
```

Tests run entirely without hardware — serial ports, GPIO, and filesystem are mocked via interfaces.

### CLI Usage

```bash
logfalcon                                         # Sync (auto-detect port)
logfalcon --port /dev/ttyACM0                     # Specific port
logfalcon --port /dev/ttyACM0 --dry-run           # Copy only, don't erase
logfalcon --web                                   # Web server only
logfalcon --version                               # Show version
```

### Testing the Web UI Locally

```bash
mkdir -p /tmp/logfalcon-test/fc_BTFL_uid-deadbeef/2026-02-26_143012
echo '{"version":1,"created_utc":"2026-02-26T14:30:12Z","fc":{"variant":"BTFL","uid":"deadbeef12345678","api_version":"4.3","blackbox_device":3},"file":{"name":"raw_flash.bbl","bytes":10485760,"sha256":"abc123"},"erase_attempted":true,"erase_completed":true}' \
  > /tmp/logfalcon-test/fc_BTFL_uid-deadbeef/2026-02-26_143012/manifest.json
touch /tmp/logfalcon-test/fc_BTFL_uid-deadbeef/2026-02-26_143012/raw_flash.bbl

./bin/logfalcon --web --config /dev/null
# Then set storage_path in config or use default
# Open http://localhost:80
```

### Building the SD Card Image

Requires Docker. Uses [pi-gen](https://github.com/RPi-Distro/pi-gen):

```bash
cd pi-gen && bash build.sh
```

Takes 30–60 min on first run. Output: `pi-gen/pi-gen-repo/deploy/`. CI builds images automatically on every `v*` tag push via `.github/workflows/release.yml`. The release workflow also builds all three binaries (`arm6`, `arm64`, `amd64`), runs an image-size gate (600 MB max after PiShrink), and publishes a GitHub Release with checksums.

### Architecture

```
cmd/logfalcon/       CLI entry point, flag parsing
internal/
├── config/          TOML config loader with search paths
├── msp/             MSP protocol: framing (14-state decoder), CRC, Huffman, client
├── fc/              Flight controller detection and handshake
├── sync/            10-step sync orchestrator (state machine)
├── storage/         Session directories, manifest.json, stream writer
├── web/             stdlib HTTP server, SSE, captive portal, file downloads
├── led/             LED state machine (6 states, sysfs + GPIO backends)
└── util/            Disk space utilities
```

### How the Sync Works

The Pi speaks **MSP v2** (with v1 fallback for handshake) over USB CDC-ACM/serial. A udev rule detects the FC and fires a one-shot systemd service:

Supported FC USB chips:
- **STM32 native USB** (VID `0x0483`) → `ttyACM*` — most modern F4/F7/H7 boards
- **CP2102** (VID `0x10c4`, PID `0xea60`) → `ttyUSB*` — some budget boards
- **CH340/CH341** (VID `0x1a86`) → `ttyUSB*` — common on Chinese budget boards

1. Wait 3s for USB to settle
2. Identify FC — `MSP_FC_VARIANT` (must be `BTFL` or `INAV`) + `MSP_UID`
3. Query flash — `MSP_DATAFLASH_SUMMARY`
4. Check Pi has enough storage
5. Stream flash in 4 KB pipelined MSP v2 chunks → `.bbl` file
6. Verify SHA-256 of the saved file
7. Write `manifest.json` (audit trail)
8. Erase FC flash (only if verify passed)
9. LED signal: success or error

**The FC's flash is never erased unless SHA-256 verification passes.**

</details>

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for dev setup, how to submit PRs, and how to report hardware compatibility.

---

## Support the project

If LogFalcon saves your session, consider:

- ⭐ **Star the repo** — helps other pilots find it
- ☕ **[Buy me a coffee on Ko-fi](https://ko-fi.com/logfalcon)** — one-off donation
- 💬 **[Start a discussion](https://github.com/proeugene/logfalcon/discussions)** — share your setup, ask questions
- 🐛 **[Report your FC board](https://github.com/proeugene/logfalcon/issues/new?template=hardware_compat.md)** — every confirmed FC helps other pilots

---

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for the full version history.

---

## License

[MIT](LICENSE) — Eugene Prokopev
