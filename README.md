# LogFalcon

[![CI](https://github.com/proeugene/logfalcon/actions/workflows/go-ci.yml/badge.svg)](https://github.com/proeugene/logfalcon/actions/workflows/go-ci.yml)

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

Later, from any phone: connect to **`LogFalcon`** Wi-Fi → logs open in your browser → download `.bbl` files → open in Blackbox Explorer.

---

## 🚀 Getting Started

### Step 1 — Download the image

Grab the latest **`logfalcon-*.img.xz`** from [**Releases**](https://github.com/proeugene/logfalcon/releases).

### Step 2 — Burn to microSD

Use [Raspberry Pi Imager](https://www.raspberrypi.com/software/) or [Balena Etcher](https://etcher.balena.io/). Any 16 GB+ card works.

### Step 3 — (Optional) Customise Wi-Fi

Before ejecting the SD card, open the `boot` partition and edit **`logfalcon-config.txt`**:

```ini
SSID=BF-Blackbox
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

> 💡 You can also set a specific version: `LOGFALCON_VERSION=v0.3.4 curl -sSL ... | sudo bash`

---

## 🛒 What You Need

| Part | Notes |
|------|-------|
| **Raspberry Pi Zero W** or **Zero 2 W** | Zero 2 W is faster. Both work. |
| **microSD card** (16 GB+) | Stores the OS + all your flight logs |
| **USB OTG cable** | Micro-USB → USB-A female |
| **USB-A to micro-USB cable** | Connects the OTG adapter → FC |
| **USB battery bank** | Powers the Pi |

No extra hardware needed — LogFalcon uses the Pi's built-in ACT LED.

> ⚠️ **Pi Zero has two micro-USB ports:**  
> **Inner port** = OTG/data → plug your FC here  
> **Outer port** = PWR_IN → plug your battery bank here

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

> If the captive portal doesn't pop up, open **`http://logfalcon.local`** or **`http://192.168.4.1`** in any browser.

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
hotspot_ssid = "BF-Blackbox"
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
- Confirm your FC shows up as `/dev/ttyACM0` on a normal PC
- Check the STM32 USB VID: `lsusb | grep 0483`
- Try a shorter or better-quality USB cable
</details>

<details>
<summary><strong>Web UI not loading</strong></summary>

```bash
journalctl -u logfalcon-web -f
```
If `logfalcon.local` doesn't resolve, use `http://192.168.4.1` directly.
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

Takes 30–60 min on first run. Output: `pi-gen/pi-gen-repo/deploy/`. CI builds images automatically on every release tag.

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

The Pi speaks **MSP v2** (with v1 fallback for handshake) over USB CDC-ACM. A udev rule detects the FC (STM VID `0x0483`) and fires a one-shot systemd service:

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

1. Fork → feature branch → make changes → add tests
2. Run `make lint && make test`
3. Open a Pull Request against `main`

CI checks linting, tests (with race detector), and multi-arch builds automatically.

---

## License

MIT
