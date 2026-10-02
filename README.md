# LogFalcon

[![CI](https://github.com/proeugene/logfalcon/actions/workflows/go-ci.yml/badge.svg)](https://github.com/proeugene/logfalcon/actions/workflows/go-ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**Automatic drone blackbox collection with a Raspberry Pi.**

LogFalcon is an open-source Go appliance for copying flight-controller blackbox logs to a Raspberry Pi over USB. It organises saved dumps by controller and session, with downloads available through a local Wi-Fi hotspot. The intended field workflow is simple: power the Pi once, connect a controller between flights, and collect logs without opening a laptop.

[Website](https://proeugene.github.io/logfalcon/) · [Pilot and developer guide](https://proeugene.github.io/logfalcon/guide.html) · [Release assets](https://github.com/proeugene/logfalcon/releases)

## Current status and compatibility

**Experimental. The current transfer workflow needs reliability fixes before field use with automatic erasure.**

- **Betaflight / SPI flash:** intended primary workflow; large-transfer and storage-device parsing defects remain unresolved.
- **iNav:** protocol support exists in the code, but flash-summary parsing needs correction. Compatibility is not confirmed.
- **FC-side SD cards and ArduPilot:** not supported.
- **Pi Zero W / Zero 2 W:** build targets exist; this checkout does not establish physical compatibility, boot time or transfer speed.
- **Dashboard:** lists saved sessions; live progress and safe-to-unplug confirmation are unavailable with the current separate services.

The code hashes received bytes and compares them with a reread of the saved file. This checks saved-copy consistency, not an independent checksum supplied by the controller or the validity of the blackbox log.

Current defaults enable **automatic controller erasure** and **deletion of old Pi sessions when storage is low**. For evaluation, set both `erase_after_sync = false` and `storage_pressure_cleanup = false` in `/etc/logfalcon/logfalcon.toml`. Check that the file is valid: malformed configuration currently falls back to defaults. Use `--dry-run` for manual transfer testing to skip controller erasure.

## What you need

- Raspberry Pi Zero W or Zero 2 W.
- microSD card, 16 GB or larger.
- Suitable 5 V USB supply or power bank.
- USB OTG adapter and a data cable for the controller.
- A Betaflight controller configured to log to internal SPI flash.

## Getting started for evaluation

1. Review the limitations above, then download a Raspberry Pi image from [Releases](https://github.com/proeugene/logfalcon/releases). Check the assets and any supplied checksums for that release.
2. Flash it with Raspberry Pi Imager or Etcher. Optionally change the hotspot settings in `logfalcon-config.txt` on the boot partition.
3. Power the Pi through **PWR IN**. Before connecting a controller, disable automatic erasure and storage cleanup in the configuration. See the [setup guide](https://proeugene.github.io/logfalcon/guide.html#setup).
4. Connect to the `LogFalcon` Wi-Fi hotspot and open **http://log.falcon** or **http://192.168.4.1**. The default Wi-Fi password is `fpvpilot`; change it before shared use.
5. Connect the controller through the Pi's **USB / OTG data port**. Keep it connected until completion has been confirmed in the service log. The current dashboard and LED do not reliably confirm the outcome.
6. Refresh the dashboard, download the `.bbl`, and check that it opens in Blackbox Explorer. Keep the original controller data during evaluation.

## Usage and development

The [guide](https://proeugene.github.io/logfalcon/guide.html) covers downloads, configuration, LED limitations, troubleshooting, SSH administration, CLI usage and local dashboard testing.

Requires **Go 1.23+**. The application is a single Go binary; the Pi image also supplies Linux services and hotspot utilities.

```sh
make build       # Native binary
make test        # Tests with the race detector
make lint        # Requires golangci-lint
make build-pi    # Linux ARM6
make build-pi2   # Linux ARM64
```

Tests run without connected hardware. Passing tests and cross-compilation do not establish field reliability. See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution instructions and [RELEASE_CHECKLIST.md](RELEASE_CHECKLIST.md) for image/release checks.

Report problems through [GitHub Issues](https://github.com/proeugene/logfalcon/issues), including the Pi model, FC board, firmware version and service log. See [CHANGELOG.md](CHANGELOG.md) for version history.

Licensed under the [MIT licence](LICENSE).
