# LogFalcon

**Collect Betaflight blackbox logs between flights, without a laptop.**

Plug the flight controller into a Raspberry Pi over USB. The Pi copies the recording from the controller’s internal flash to its microSD card. Join the Pi’s Wi-Fi on your phone to download the `.bbl`.

I leave the original on the quad by default, even if that means bringing home two copies of the same flight.

[Try it on a Pi](#try-it-on-a-pi) · [Run the copy test](#when-a-recording-grows-during-a-copy) · [Architecture](docs/ARCHITECTURE.md)

![LogFalcon dashboard showing 4 MB saved, the original retained on the controller and the download button](docs/images/dashboard-preview.png)

*Dashboard after a simulated 4 MB transfer. The second recording header is new, and the original data is still on the controller.*

## When a recording grows during a copy

An early version had a bug in the optional erase step. A simulated controller added a recording while the Pi was copying. The Pi saved every byte it had asked for, the checksum matched, and it sent an erase command. The new recording had never reached the Pi. [Audit record](DEVELOPMENT_TESTING.md#local-code-and-appliance-audit-27-november-2026)

LogFalcon now asks the controller how much flash is in use a second time, after the file and its manifest are on disk. If the size has changed or the controller can’t confirm it’s ready, the Pi keeps the copy and skips erasure. The controller can still write after that last check, so stop recording before plugging it in. [Copy and recovery design](docs/ARCHITECTURE.md#copy-and-recovery-ordering)

You can run the regression with Go 1.27 and no hardware. It checks that the saved file contains the original bytes and the controller receives zero erase commands. [Test code](internal/sync/transfer_test.go#L362-L387)

```sh
go test -v -count=1 -run '^TestEraseSkipsFlashThatChangedDuringCopy$' ./internal/sync
```

## What’s been tried in the field

On an earlier build, a pilot collected a log from a DARKSTAR20. Wi-Fi dropped after they unplugged the controller. They power-cycled the Pi, downloaded the 8 MiB file and opened it in Blackbox Explorer. The cause of the dropout is still unknown. The next field test on each Pi model is to unplug after a copy and check that the file is still downloadable over Wi-Fi. [Field report](DEVELOPMENT_TESTING.md#first-field-report--11-november-2026)

## Try it on a Pi

Use a Pi Zero W or Zero 2 W and a Betaflight controller with **internal SPI flash**. You’ll need a 16 GB or larger microSD card, a 5 V supply, a USB OTG adapter and a data cable. iNav copying is disabled. Controller-side SD cards and ArduPilot are unsupported. [Compatibility notes](docs/FC_COMPATIBILITY_ANALYSIS.md)

As of 27 November 2026, the published SD images predate the erase fix above. Build the image from this checkout and verify its checksum before flashing a spare card. Check that `erase_after_sync` and `storage_pressure_cleanup` are both `false` before connecting a controller. Fresh installs default to off. Upgrades keep your settings. [Image build instructions](DEVELOPMENT_TESTING.md#exact-image-baseline-and-comparison) · [Pilot setup guide](https://proeugene.github.io/logfalcon/guide.html#setup)

On the Pi’s Wi-Fi, open `http://log.falcon` or `http://192.168.4.1`. Choose **Download saved .bbl** when the copy finishes. A file can contain several flights. Open it in the Betaflight App’s Blackbox Viewer to inspect the traces. [Betaflight App](https://app.betaflight.com/)

## Working on the code

The copier, dashboard and status LED run as separate Linux services from the same Go binary. Restart the dashboard mid-copy and the transfer carries on. When the dashboard comes back, it picks up progress from the same file the LED service reads. [Process map](docs/ARCHITECTURE.md#one-binary-separate-processes)

I used a coding assistant on this project. One weakness in the early tests was that the fake controller built its replies with LogFalcon’s own encoder. A mistake on both sides could pass. The decoder tests now also read fixed packets based on Betaflight’s published wire format. [Protocol fixtures](internal/msp/testdata/README.md) · [AI-assisted development notes](docs/AI_ASSISTED_DEVELOPMENT.md)

Run `make build` for a native binary or `make test` for the race-enabled suite. [Contributing](CONTRIBUTING.md) · [CI](https://github.com/proeugene/logfalcon/actions/workflows/go-ci.yml) · [Signed offline updates](docs/OFFLINE_UPDATES.md)

Maintained by [Eugene Prokudin](https://github.com/proeugene). [Issues](https://github.com/proeugene/logfalcon/issues) · [MIT licence](LICENSE) · [Provenance](docs/PROVENANCE.md)
