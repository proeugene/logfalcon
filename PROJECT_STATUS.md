# Betaflight Blackbox Field Syncer - Project Status

## What this project is

Betaflight Blackbox Field Syncer is a field-ready Raspberry Pi Zero W / Zero 2 W appliance for FPV pilots who use Betaflight blackbox logging on **internal SPI flash**.

Its job is simple:

1. detect a plugged-in Betaflight flight controller over USB
2. read the blackbox flash over MSP
3. save the raw log to the Pi SD card
4. verify the copy with SHA-256
5. erase the FC flash only after verification passes
6. make the saved logs available over Wi-Fi from a phone or laptop

This lets a pilot free up blackbox storage at the field without carrying a laptop.

## What problem it solves

Pilots often fill their FC blackbox flash during a flying session. Clearing it normally means:

- connecting to a laptop
- opening Betaflight Configurator
- waiting for the FC to connect
- downloading or discarding logs manually

That workflow is slow and awkward at the field. This project turns the process into a dedicated plug-in sync box.

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
- **Optional acceleration:** native C extension in `bbsyncer/_native/_msp_fast.c`
- **Protocol:** MSP
- **Target hardware:** Raspberry Pi Zero W / Zero 2 W
- **Current version:** `0.1.1`

## Major project areas

- `bbsyncer/msp/` - MSP framing, CRC, client, constants, Huffman
- `bbsyncer/fc/` - FC detection and compatibility checks
- `bbsyncer/sync/` - copy / verify / erase orchestration
- `bbsyncer/storage/` - session layout and manifest handling
- `bbsyncer/web/` - browser UI, downloads, settings, health
- `bbsyncer/led/` - LED signaling
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

- automated tests passing: **86 tests**
- lint and formatting checks passing
- version bumped to **0.1.1**
- recent hardening completed in web security, docs, and status visibility

### Important scope boundary

This project is for **internal SPI flash blackbox** workflows.

It does **not** read blackbox logs stored on an FC-side SD card over MSP.

## Recent changelog

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
