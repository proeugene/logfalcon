# LogFalcon - Project Status

## What this project is

FPV pilots who use Betaflight blackbox logging on internal SPI flash hit a recurring wall: the flash is small, fills up fast, and when it's full, logging stops mid-session. Before this project, the only ways to clear it were a laptop with Betaflight Configurator, or an expensive third-party USB dongle — both require extra hardware and often mean leaving the field.

LogFalcon is a pocket-sized Raspberry Pi Zero W / Zero 2 W appliance that removes that friction entirely. Plug the FC in, wait for the LED (~30–40 s), unplug, fly again. Repeat as many times as needed. All synced logs are timestamped, organised by FC, and available over the Pi's Wi-Fi hotspot from any phone when you're done flying.

Internally it:

1. detects a plugged-in Betaflight FC over USB
2. reads the blackbox flash over MSP
3. saves the raw log to the Pi SD card
4. verifies the copy with SHA-256
5. erases the FC flash only after verification passes
6. signals done via LED, makes logs available over Wi-Fi

## What problem it solves

FC onboard SPI flash is small — typically 2–4 MB. A few packs and it's full; Betaflight silently stops logging. Before this project, clearing it meant:

- connecting to a **laptop**, opening Betaflight Configurator, downloading or discarding logs manually — or —
- buying and carrying a **third-party USB dongle** that connects to a phone

Both options require leaving the field or spending money on extra hardware. This project turns a $15 Pi Zero W (already in most pilots' kit) into a dedicated sync box that works automatically at the field, with no laptop and no dongles.

## What it is capable of

### Core sync workflow

- automatic FC detection through USB CDC-ACM
- Betaflight compatibility checks over MSP
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
- optional native C extension for hot MSP paths
- zero-copy file sending with `sendfile()` when available
- timing metrics recorded in manifests for visibility

### Packaging and deployment

- install script for Raspberry Pi OS
- systemd units for web service and sync jobs
- udev trigger for automatic sync on FC plug-in
- boot-partition config file for pre-boot hotspot customization
- Docker path for web-focused local testing

## Technology summary

- **Language:** Python 3.11+
- **Runtime dependencies:** `pyserial`, conditional `RPi.GPIO`
- **Optional acceleration:** native C extension in `logfalcon/_native/_msp_fast.c`
- **Protocol:** MSP
- **Target hardware:** Raspberry Pi Zero W / Zero 2 W
- **Current version:** `0.1.3`

## Major project areas

- `logfalcon/msp/` - MSP framing, CRC, client, constants, Huffman
- `logfalcon/fc/` - FC detection and compatibility checks
- `logfalcon/sync/` - copy / verify / erase orchestration
- `logfalcon/storage/` - session layout and manifest handling
- `logfalcon/web/` - browser UI, downloads, settings, health
- `logfalcon/led/` - LED signaling
- `config/`, `system/`, `boot/` - deploy-time integration for Pi images and installs
- `tests/` - automated test coverage

## Current quality/readiness snapshot

### Strengths

- clear product purpose and strong field-use fit
- safe copy -> verify -> erase ordering
- lightweight, dependency-minimal web stack
- good packaging for a Pi appliance
- clear compatibility with Betaflight blackbox workflows

### Current validation status

- automated tests passing: **159 tests**
- lint and formatting checks passing
- version bumped to **0.1.3**
- recent hardening completed in web security, docs, and status visibility

### Important scope boundary

This project is for **internal SPI flash blackbox** workflows.

It does **not** read blackbox logs stored on an FC-side SD card over MSP.

## Recent changelog

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
