# LogFalcon

**A Raspberry Pi flight-log collector for Betaflight pilots.**

I’m Eugene Prokudin. I built LogFalcon so I could collect blackbox recordings between flights without taking a laptop to the field. Plug the flight controller into the Pi over USB, then download the logs to a phone over the Pi’s own Wi-Fi. I chose to keep the original recording on the controller by default.

[How it protects a recording](#when-a-recording-grows-during-a-copy) · [Architecture](docs/ARCHITECTURE.md) · [Pilot guide](https://proeugene.github.io/logfalcon/guide.html) · [My GitHub profile](https://github.com/proeugene)

![LogFalcon dashboard showing 4 MB saved, the original retained on the controller and a button to download the recording](docs/images/dashboard-preview.png)

*The dashboard running locally with a simulated transfer and sample recording.*

## When a recording grows during a copy

Is it safe to erase a controller after a verified copy? The Pi can save every byte the controller reported and still miss a recording that started during the transfer.

If a pilot enables erasure, LogFalcon checks the byte count and compares the SHA-256 of the incoming bytes with a reread of the saved file. Once the recording and its manifest are safely on disk, it asks the controller how much flash is in use. If the size changed or the controller can’t confirm it is ready, LogFalcon keeps the copy and skips erasure. Stop recording before collection, since the controller can still write after that last check. [Copy and recovery design](docs/ARCHITECTURE.md#copy-and-recovery-ordering)

You can run the regression without a Pi. The simulated controller appends data during the first read. The test passes only if the original snapshot is still readable, the manifest records no erase attempt and the controller receives **zero erase commands**. [Regression test](internal/sync/transfer_test.go#L362-L387)

```sh
go test -count=1 -run '^TestEraseSkipsFlashThatChangedDuringCopy$' ./internal/sync
```

## What the pilot sees

- **Transfer progress and saved recordings.** Each copy is grouped by controller and collection time. One flash dump can contain several flights.
- **What changed since last time.** The dashboard can identify a repeat dump or a newly appended recording header when the previous bytes match. **Channel details** shows which filtered and unfiltered gyro axes the header declares. Open the file in Betaflight’s Viewer to inspect the samples.
- **Downloads and notes.** Download a single `.bbl` or select up to 100 recordings for a ZIP with manifests and notes. Add a controller nickname or a note about a flight while it is still fresh.

## Current status and compatibility

**Experimental.** On an earlier build, a pilot downloaded an 8 MiB recording and opened it in Blackbox Explorer. The pilot also reported losing Wi-Fi after unplugging the controller. The next field check is to repeat that sequence on an image built from this revision. [Field report](DEVELOPMENT_TESTING.md#first-field-report--11-november-2026) · [Software audit](DEVELOPMENT_TESTING.md#local-code-and-appliance-audit-27-november-2026)

This revision targets Betaflight controllers with internal SPI flash and the Pi Zero W or Zero 2 W. Each Pi model needs its own physical acceptance test. iNav copying is disabled. Controller-side SD cards and ArduPilot are unsupported. [Compatibility evidence](docs/FC_COMPATIBILITY_ANALYSIS.md)

## Engineering and development

One Go binary runs as separate Linux services. The USB service publishes its progress to a shared file that the dashboard and the Pi’s status LED both read. The dashboard can restart while a copy is in progress and pick up the same transfer. [Process map](docs/ARCHITECTURE.md#one-binary-separate-processes) · [Signed offline updates and rollback](docs/OFFLINE_UPDATES.md)

I used a coding assistant to help implement and test LogFalcon. Early tests used LogFalcon’s own encoder to simulate a controller, so both sides could agree on the same protocol mistake. Fixed test packets tied to a specific Betaflight revision give the decoder a separate reference. [AI-assisted development retrospective](docs/AI_ASSISTED_DEVELOPMENT.md) · [Protocol fixtures](internal/msp/testdata/README.md)

With **Go 1.27**, run `make build` for a native binary or `make test` for the race-enabled suite. The contributor guide covers linting, Pi cross-builds and SD images. [Contributing](CONTRIBUTING.md) · [Diagnostics and testing](DEVELOPMENT_TESTING.md) · [Report an issue](https://github.com/proeugene/logfalcon/issues)

## Getting started for evaluation

You’ll need a Pi Zero W or Zero 2 W, a microSD card of at least 16 GB, a 5 V supply, a USB OTG adapter and a controller data cable.

1. Choose an image whose release notes cover the safeguards you intend to test. Verify its checksum, flash it and set your Wi-Fi country in `logfalcon-config.txt` on the boot partition. [Release assets](https://github.com/proeugene/logfalcon/releases)
2. Power the Pi through **PWR IN**. Before connecting the controller, confirm `erase_after_sync = false` and `storage_pressure_cleanup = false`. Both default to off on fresh installs. Upgrades preserve existing settings, including previously enabled erasure. [Setup guide](https://proeugene.github.io/logfalcon/guide.html#setup)
3. Join the Pi’s hotspot and open **http://log.falcon** or **http://192.168.4.1**. A fresh install uses `LogFalcon-XXXXXXXXXXXX` and the temporary password `fpvpilot`. Upgrades keep the configured network name.
4. Stop recording, connect the controller to the Pi’s **USB / OTG data port** and leave it connected until the dashboard shows completion. Download the `.bbl` and open it in Blackbox Explorer.

[Project website](https://proeugene.github.io/logfalcon/) · [CI runs](https://github.com/proeugene/logfalcon/actions/workflows/go-ci.yml) · [Changelog](CHANGELOG.md) · [Release checklist](RELEASE_CHECKLIST.md) · [MIT licence](LICENSE) · [Provenance](docs/PROVENANCE.md)
