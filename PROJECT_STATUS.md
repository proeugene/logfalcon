# LogFalcon - Project Status

## What this project is

FPV pilots who use Betaflight or iNav blackbox logging on internal SPI flash hit a recurring wall: the flash is small, fills up fast, and when it's full, logging stops mid-session. Before this project, the only ways to clear it were a laptop with Betaflight/iNav Configurator, or an expensive third-party USB dongle — both require extra hardware and often mean leaving the field.

LogFalcon is a pocket-sized Raspberry Pi Zero W / Zero 2 W appliance that removes that friction entirely. Plug the FC in, wait for the LED (~30–40 s), unplug, fly again. Repeat as many times as needed. All synced logs are timestamped, organised by FC, and available over the Pi's Wi-Fi hotspot from any phone when you're done flying.

Internally it:

1. detects a plugged-in Betaflight or iNav FC over USB
2. reads the blackbox flash over MSP
3. saves the raw log to the Pi SD card
4. verifies the copy with SHA-256
5. erases the FC flash only after verification passes
6. signals done via LED, makes logs available over Wi-Fi

## What problem it solves

FC onboard SPI flash is small — typically 2–4 MB. A few packs and it's full; Betaflight/iNav silently stops logging. Before this project, clearing it meant:

- connecting to a **laptop**, opening Betaflight Configurator, downloading or discarding logs manually — or —
- buying and carrying a **third-party USB dongle** that connects to a phone

Both options require leaving the field or spending money on extra hardware. This project turns a $15 Pi Zero W (already in most pilots' kit) into a dedicated sync box that works automatically at the field, with no laptop and no dongles.

## What it is capable of

### Core sync workflow

- automatic FC detection through USB CDC-ACM
- Betaflight and iNav compatibility checks over MSP
- flash summary query before sync
- full blackbox flash copy to SD card
- SHA-256 verification before erase
- optional erase skip for dry-run or testing flows
- safe handling of already-empty flash

### Storage and data handling

- stores logs by FC and session timestamp
- keeps raw `.bbl` files directly compatible with Blackbox Log Viewer / Blackbox Explorer
- writes `manifest.json` per session with FC metadata, file size, checksum, erase state, and timing data
- refuses unsafe syncs when free storage is below the configured threshold
- can auto-delete the oldest stored sessions under storage pressure to preserve reserve space for a new safe sync

### Web and field UX

- built-in hotspot / captive-portal experience
- browser-based session list and downloads
- live sync state visibility
- settings page for hotspot SSID/password changes
- health endpoint at `/health`
- LED guidance for syncing, verifying, erasing, success, already-empty, and error states

### Performance-oriented implementation

- pipelined MSP reads to reduce idle time
- pure Go implementation — no C extensions, no CGO required
- SHA-256 streaming verification during writes
- timing metrics recorded in manifests for visibility

### Packaging and deployment

- install script for Raspberry Pi OS
- systemd units for web service and sync jobs
- udev trigger for automatic sync on FC plug-in
- boot-partition config file for pre-boot hotspot customization
- Docker path for web-focused local testing

## Technology summary

- **Language:** Go 1.22+
- **Binary size:** ~6 MB (single static binary, no runtime dependencies)
- **External deps:** `go.bug.st/serial` (serial port), `pelletier/go-toml` (config), `golang.org/x/sys` (disk stats)
- **Protocol:** MSP v1/v2 (v2 for flash reads — 16× throughput vs v1)
- **Target hardware:** Raspberry Pi Zero W / Zero 2 W
- **Current version:** `0.3.8`

## Major project areas

- `cmd/logfalcon/` — CLI entry point, flag parsing, wiring
- `internal/msp/` — MSP framing (14-state decoder), CRC, client, constants, Huffman
- `internal/fc/` — FC detection and compatibility checks
- `internal/sync/` — copy / verify / erase orchestration (10-step state machine)
- `internal/storage/` — session layout, manifest handling, stream writer
- `internal/web/` — browser UI, SSE, downloads, settings, health, captive portal
- `internal/led/` — LED signaling (6 states, sysfs/GPIO backends)
- `internal/config/` — TOML config loader with search paths
- `internal/util/` — disk space utilities
- `config/`, `system/`, `boot/` — deploy-time integration for Pi images and installs

## Current quality/readiness snapshot

### Strengths

- clear product purpose and strong field-use fit
- safe copy -> verify -> erase ordering
- lightweight, dependency-minimal web stack
- good packaging for a Pi appliance
- clear compatibility with Betaflight and iNav blackbox workflows

### Current validation status

- automated tests passing: **71 tests** (Go, with race detector)
- vet and build checks passing
- cross-compilation verified: ARM6 (Pi Zero W) and ARM64 (Pi Zero 2 W)
- version: **0.3.8** (Go rewrite)

### Important scope boundary

This project is for **internal SPI flash blackbox** workflows (Betaflight and iNav).

It does **not** read blackbox logs stored on an FC-side SD card over MSP.

## Recent changelog

## v0.3.8 — SSID Renamed to LogFalcon

### Wi-Fi SSID default changed from `BF-Blackbox` to `LogFalcon`
- Updated in Go code (`internal/config/config.go` default), config template (`config/logfalcon.toml`), boot partition template (`boot/logfalcon-config.txt`), pi-gen hostapd config (`02-run-chroot.sh`), root `install.sh`, and `scripts/install.sh`
- `config_test.go` updated to match new default — all tests pass
- All docs (`README.md`, `docs/guide.html`) updated consistently
- Existing installs are unaffected — the SSID is read from the deployed config file, not the compiled binary

## v0.3.7 — Docs Polish & SSID Audit

### Documentation consistency
- **Real-time sync progress documented**: `README.md` and `docs/guide.html` now include text mockups of the live progress display (`Syncing flash… 45%  (2.1 / 4.0 MB) · 1.2 MB/s · ~18s remaining`); `docs/guide.html` §04 explains state badge colours and what to expect
- **PROJECT_STATUS.md**: stale `0.3.5` version metadata corrected
- All version strings bumped across all docs and site

### CI/CD fixes (from v0.3.6 follow-up)
- Binary artifacts now correctly attached to GitHub Releases (was broken — `go-ci.yml` never triggered on tag pushes)
- `golangci-lint` pinned to `v1.64.8` (was `latest`)
- `xz -9e` → `xz -9` in image build (saves ~5–10 min per release)
- `gomod` added to Dependabot

## v0.3.6 — SSH Docs + Headless Boot Fix + Real-Time Sync Progress

### Headless boot fix (Bookworm / Pi Zero 2 W)
- **Root cause**: Raspberry Pi OS Bookworm removed the default `pi` user; `userconfig.service` from `userconf-pi` triggered an interactive "enter username" wizard on first boot — fatal for a headless field appliance
- **Belt-and-suspenders fix**: `pi-gen/config` sets `FIRST_USER_PASS=logfalcon` (pi-gen creates the user at build time); `00-run.sh` creates `userconf.txt` in the boot partition (official Bookworm headless method); `00-run-chroot.sh` disables `userconfig.service`
- Default SSH password changed from `raspberry` → `logfalcon` (device-specific, less guessable)

### SSH access documented
- New **🔐 SSH Access** section in `README.md`
- Setup and Troubleshooting sections in `docs/guide.html` now document default credentials, `passwd`, and SSH connection strings

### Real-time sync progress on web dashboard
- `Status` struct extended with `BytesCopied`, `TotalBytes`, `SpeedBPS`, `ETASec` fields
- Flash-read loop emits real-time metrics every chunk: bytes copied, transfer speed (MB/s), and ETA
- `handleStatus` passes new fields in the JSON response
- Web UI now shows: `Syncing flash… 45%  (2.1 / 4.0 MB)` + speed + ETA below the progress bar
- State badge now distinguishes `identifying`/`querying` (gray-blue pulsing) from `syncing` (solid blue)

## v0.3.5 — One-liner Install Scripts

### Install / uninstall on existing Pi OS
- **`scripts/install.sh`**: full one-command installer for existing Raspberry Pi OS installs — detects arch (arm6/arm64), downloads the correct binary from GitHub Releases, installs hostapd/dnsmasq/avahi-daemon, sets up the Wi-Fi hotspot, installs all systemd units (sync, web, firstboot, LED), and applies boot optimizations in a single `curl | sudo bash`
- **`scripts/uninstall.sh`**: clean one-command uninstaller — stops and disables all services, removes binary, config, systemd units, udev rule, and hotspot config; optionally preserves logs
- README updated with install/uninstall commands and version pin hint
- Docs and website updated to v0.3.5

## v0.3.4 — Performance Optimization

### MSP v2 flash reads & high-speed serial
- **MSP v2 for flash reads**: 16-bit length field enables ~4 KB responses instead of 255 B with v1 (~16× per-frame throughput)
- **Baud rate 921,600**: 8× raw throughput over UART (USB CDC ignores baud — runs at USB speed)
- **Faster retry**: error recovery delay reduced from 100 ms to 10 ms
- **Chunk size aligned**: request size set to 4,096 to match Betaflight's MSP serial buffer
- **Config updated**: sample `logfalcon.toml` defaults now match code (baud=921600, chunk=4096)

### Estimated sync times (16 MB flash)
| Connection | Before (v0.3.1) | After (v0.3.4) |
|---|---|---|
| UART | ~20 min | ~2.5 min |
| USB | ~5–9 min | ~30–60 sec |

## v0.3.1 — Infrastructure Cleanup

### Python removal & Go-native infrastructure
- **Removed all legacy Python code**: `logfalcon/`, `tests/`, `pyproject.toml`
- **Dockerfile rewritten**: multi-stage Go build (golang:1.22-alpine → alpine:3.20)
- **pi-gen updated**: removed Python3/pip/venv packages, now copies pre-built Go binary
- **CI/CD cleaned**: removed old Python CI, rewrote security workflow (govulncheck + CodeQL)
- **.gitignore modernised**: replaced Python entries with Go-specific patterns
- **Pilot guide added**: `docs/guide.html` — 9-section field guide matching HUD design
- **Structured logging**: all packages migrated from log.Printf to slog

## v0.3.0 — Go Rewrite

### Complete rewrite from Python to Go
- **Single static binary**: ~6 MB replaces ~250 MB Python venv — 50× smaller
- **Instant startup**: ~10ms cold start vs 500ms–2s Python — 50–200× faster
- **Minimal memory**: ~5–10 MB idle vs 45–55 MB Python — 5–10× less
- **Zero runtime dependencies**: no Python, pip, venv, or C compiler needed on Pi
- **Race-detector tested**: all 71 tests pass with `go test -race`

### Architecture
- 8 internal packages: config, msp, fc, storage, sync, led, web, util
- 3,972 LOC Go source + 1,767 LOC tests
- 3 external dependencies (serial, TOML, syscall)
- Pure Go — no CGO, no C extension needed
- Interface-based design enables full mock testing without hardware

### New features in rewrite
- 14-state MSP frame decoder (v1/v2 protocol support)
- Thread-safe sync status via `sync.RWMutex` (goroutine-safe SSE)
- Channel-based interruptible LED sleep patterns
- Proper context cancellation throughout
- CI/CD pipeline: lint, test with race detector, multi-arch builds, GitHub Release

### Deployment simplified
- `install.sh` copies single binary (no venv setup)
- systemd services point to `/opt/logfalcon/logfalcon`
- Pi image can drop Python entirely (~150–200 MB savings)

## v0.2.1

### Ready LED Signal
- **New "Ready" LED state**: solid LED on when Pi is booted and waiting for FC — clear transition from blinking heartbeat to solid means "ready to plug in"
- After sync completes, LED returns to solid "ready" instead of turning off — Pi always shows it's alive
- New `logfalcon-ready-led.service` manages the ready LED via systemd lifecycle
- Sync service pauses ready LED during sync and restores it after (ExecStartPre/ExecStopPost)
- Added `LEDState.READY` to Python LED state machine with solid-on pattern

### Boot Speed Optimizations
- Disabled Bluetooth overlay (`dtoverlay=disable-bt`) — saves ~5s
- Disabled splash screen and boot delay in `config.txt`
- Quiet kernel boot (`quiet loglevel=3`) — reduces console output, saves ~2-3s
- Masked unused systemd services: triggerhappy, apt-daily timers, man-db
- Disabled hciuart and bluetooth services
- Pre-compiled Python bytecode (`compileall`) for faster startup
- Estimated boot time reduction: ~90s → ~60s

### Tests
- Added READY state enum and pattern tests
- Added READY solid-on execution test
- 170+ tests total

## v0.2.0

### iNav Flight Controller Support
- **First-class iNav support**: LogFalcon now detects and syncs iNav FCs alongside Betaflight — same plug-in, LED, fly-again workflow
- Variant-aware MSP parsing: handles iNav's DATAFLASH_READ response format (no length/compression header)
- Automatic compression suppression for non-Betaflight FCs
- Dynamic directory naming: iNav sessions stored as `fc_INAV_uid-*` (Betaflight remains `fc_BTFL_uid-*`)
- Skips BLACKBOX_CONFIG query for iNav (deprecated in iNav firmware)
- Renamed `FCNotBetaflight` → `FCNotSupported` exception (backwards-compatible alias kept)
- Added 7 new tests covering iNav detection, parsing, and full sync flow (159 → 166 total)
- Zero breaking changes to existing Betaflight workflows

## v0.1.3

### Critical Fixes
- **Fixed show-stopper install path mismatch**: install.sh created `/opt/bbsyncer` but systemd units expected `/opt/logfalcon` — services would fail on fresh install
- Renamed all stale bbsyncer paths across install.sh, pi-gen, firstboot, dnsmasq, avahi

### Bug Fixes
- Added threading.Lock to web server session cache (race condition)
- Replaced bare `except Exception:` with specific types in HTTP handlers
- Safe dict access in FC detector (prevents KeyError)
- Log warning on corrupted manifest JSON (was silently skipped)
- Extracted duplicate frame-flush code in MSP client (DRY)

### Refactoring
- Orchestrator: extracted 252-line `_run()` into 6 focused phase methods
- Web server: extracted 526 lines of HTML templates to `_templates.py`
- Server.py reduced from ~1100 to ~600 lines

### UX Improvements
- Styled HTML error pages (replaced plain text "404 Error")
- Idle auto-shutdown countdown banner on web UI
- Synced default SSID across all config files
- Fixed site branding and download link

### Test Coverage
- Added 70 new tests (89 → 159 total, 79% increase)
- New test files: test_msp_client.py (19), test_led_controller.py (24)
- Expanded: test_manifest.py (1→17), test_web_server.py (+11)
- CI coverage gate: --cov-fail-under=50

## v0.1.2

### Image build and distribution

- added `03-run-chroot.sh` cleanup stage: purges build-only packages
  (gcc, python3-dev) after C extension compile, removes nfs-common,
  triggerhappy, lua5.1, man-db, apt caches, docs, locale data and
  Python bytecode — reduces raw image size by ~150–250 MB
- added PiShrink step in CI before xz compression, stripping unused
  filesystem space (typically 30–50% further reduction)
- upgraded xz compression from `-9` to `-9e -T0` (extreme preset,
  all threads) for better ratio
- CI job summary now reports raw / shrunk / compressed image sizes
- images published automatically to GitHub Releases on version tags

## v0.1.1

### Security and safety improvements

- added CSRF protection to hotspot settings changes and session deletion
- restricted downloadable files to the expected session artifacts
- replaced regex-style config rewrites with safer full-line rewrite helpers
- added printable-character validation for hotspot credentials
- added better failure handling around `hostapd` restart

### Field UX and support improvements

- improved empty-state and quick-start guidance in the web UI
- added richer sync status messages for identifying, copying, verifying, erasing, and error cases
- added `/health` endpoint for support and diagnostics
- surfaced warning when the default hotspot password is still in use
- added timing data to manifests for better performance visibility
- added low-space warning surfaces and storage-pressure cleanup behavior

### Installer and boot-flow improvements

- installer now generates a unique hotspot password when one is not provided
- installer output now explains startup timing and where hotspot config is mirrored
- boot config comments improved for first-time setup

### Documentation improvements

- README rewritten to be friendlier for first-time pilots
- clarified Pi USB port usage
- clarified captive-portal fallback behavior
- clarified SPI flash requirement
- clarified realistic field sync timing expectations

## Outstanding tasks

These are the major items still open from the current engineering plan.

### 1. Launch validation matrix

Still needed:

- validate against real FC families and Betaflight versions
- validate Pi Zero W vs Zero 2 W behavior
- validate iPhone and Android captive-portal behavior in the field
- validate failure modes:
  - unplug during sync
  - low disk space
  - poor/noisy USB cable
  - repeated retry scenarios

Why it matters:

- this is the biggest remaining launch-confidence gap

## Recommended next steps

1. run the real-hardware launch validation matrix
2. gather pilot feedback from real field sessions, especially around cleanup expectations
3. decide whether the next release should focus on:
   - storage lifecycle automation
   - additional mobile/web polish
   - broader launch packaging

## Quick executive summary

This project is already a strong product for a focused FPV field workflow: copy logs safely, erase only after verification, and retrieve them later over Wi-Fi.

The main remaining work is not core architecture - it is **real-world launch validation**.
