# Development diagnostics and physical acceptance

These changes are implemented in the source checkout. Automated tests use a fake
MSP controller and temporary files. They do not establish Pi Wi-Fi, LED, serial,
power-loss or image compatibility. Older release images may lack these changes.

## Development logging defaults

Debug logging and UK Wi-Fi settings are enabled by default. The SD card's boot
partition contains these settings in `logfalcon-config.txt`:

```ini
SSID=LogFalcon
PASSWORD=choose-your-password
COUNTRY_CODE=GB
DEBUG=true
```

The image also defaults to UK English, a UK keyboard and `Europe/London` time.
Change the country if the Pi operates elsewhere. Reboot to
apply changes. Keep both `erase_after_sync` and `storage_pressure_cleanup` false
in `/etc/logfalcon/logfalcon.toml` during evaluation. New installations default
both to false; upgrading preserves existing configuration, including previously enabled destructive settings.
Invalid application configuration stops copying rather than enabling defaults.

`DEBUG=true` enables additional MSP command numbers, lengths and copy progress.
It does not log passwords or protocol payloads. Normal events already record
boots, service activity, controller identity, transfer stages/outcomes, retries,
DHCP activity and meaningful web actions. Version, commit, boot ID, process ID
and time since boot identify events; sync messages also carry a transfer ID.
Client connection/disconnection evidence comes from hostapd and kernel messages;
availability/detail depends on their logging and must be checked on the image.

To reduce logging, set `DEBUG=false` and reboot. For a single manual command, use
`sudo env LOGFALCON_DEBUG=true /opt/logfalcon/logfalcon --port /dev/ttyACM0 --dry-run`
only when the automatic copy service is inactive. The transfer lock rejects overlap.

## Retrieve diagnostics on the Mac

1. Shut the Pi down cleanly (`sudo shutdown -h now`) and wait for shutdown to
   complete before disconnecting its power or removing its card.
2. Insert the card into the Mac. Open its boot volume and copy
   `LOGFALCON-DIAGNOSTICS/latest.json` to the Mac before reusing the card.
3. Analyse that file alongside the Pi/FC models, firmware and observed behaviour.

The snapshot contains recent current/previous boot service and kernel events,
generation time, boot IDs, software version and a configuration summary without
credentials. It is capped at 2 MiB and atomically replaces the previous snapshot;
there is no growing archive on the boot partition. It refreshes at boot, after
transfers, every five minutes and during clean service shutdown. An asynchronous
export runs separately from copying. It skips a missing FAT mount or a boot
partition with less than 16 MiB free. Missing journal sections are labelled.

The underlying persistent journal is limited to approximately 50 MiB/seven days,
with a free-space reserve. Exported sections are bounded tails, not the entire
journal. To obtain a larger history while the Pi runs:

```sh
sudo journalctl -u 'logfalcon*' -u hostapd -u dnsmasq -b --no-pager
sudo journalctl -k -b --no-pager
sudo systemctl start logfalcon-diagnostics-export.service
```

Abrupt power loss can lose the latest buffered events or interrupt an export.
An incomplete previous lifecycle is detected on the next boot, but its cause is
unknown. A clean lifecycle marker records that the service's shutdown handler
ran; it is not proof that the entire OS shutdown completed. Snapshot timestamps
may be wrong before the Pi has a correct clock; use boot IDs and elapsed times.

## Physical acceptance — exact image required

Record the source commit, image filename/checksum, Pi model, power supply, cable,
FC model, firmware and log sizes. Use disposable test flights and keep FC erasure
disabled throughout this first pass.

1. Build and inspect the SD image from this checkout. Flash that exact artifact.
   Cold boot with no controller connected. Confirm `LogFalcon` appears, the phone
   receives a `192.168.4.x` address, and `http://192.168.4.1` and `http://log.falcon`
   open the dashboard. No internet connection is required. Open the URL manually;
   captive-portal auto-opening is not guaranteed.
2. Change the hotspot name/password in Settings. Reconnect with the new values,
   reboot, and confirm they persist. Set a different country/debug value through
   the boot file, reboot, and verify the effective settings and service logs.
3. With Betaflight configured for SPI flash and a real log larger than 64 KiB,
   plug the FC into the OTG data port. Confirm automatic start, changing progress,
   a completed result and a saved session. LED expectations: slow pulse while waiting for the first
   transfer, solid after confirming empty flash, fast blink during work, repeating double blink after success, SOS
   after failure. Confirm those patterns on each Pi model; they are unvalidated.
4. Download the `.bbl`, check its size/hash against the manifest, and open it in
   Blackbox Explorer. Confirm the original FC flash remains unchanged. Test a
   larger dump and multiple flights within one dump; record timings rather than
   assuming performance.
5. Unplug/reconnect and repeat. Each run must create a separate session. Disconnect
   during a copy: expect interruption/error, no complete download for that run,
   retained `.partial` data and untouched FC flash. Reconnect and obtain a full copy.
6. Reboot and check service recovery, previous saved downloads and diagnostic
   snapshots. For an intentional power-cut test, use a spare card/test data and
   collect the next boot's diagnostics; do not claim power-loss safety from tests
   that have not actually been run.
7. Shut down, insert the card into this Mac and inspect `latest.json`. Check that
   the timeline includes the boot, USB, hotspot, copy, download and shutdown
   events, with credentials absent. Test the Wi-Fi-failure path too: the SD-card
   export must remain useful when the dashboard cannot be reached.

Record pass/fail and evidence for each step. Investigate failures before enabling
automatic erasure or publishing a release. iNav automatic copying is deliberately
disabled until its current dataflash protocol has been validated; FC-side SD-card
logs and ArduPilot remain unsupported. Saved-copy hashing checks storage
consistency, not an independent controller checksum or blackbox structural validity.

## Physical result format

Copy this entry for each actual run. Leave unknowns blank and mark failures;
the template itself is not a completed result.

```text
Date / tester:
Source commit (record dirty changes if any):
Image filename / SHA-256:
Pi model / supply / cable / OTG adapter:
FC board / firmware / MSP API / storage device:
Log bytes / flights in dump:
Erase enabled? / cleanup enabled?:
Steps attempted / observed outcome / pass or fail:
Measured seconds: power→Wi-Fi / plug→copy start / copy / download:
Downloaded bytes / saved-copy hash match / Blackbox Explorer result:
Disconnect, reboot and settings recovery outcomes:
LED / dashboard / journal agreement:
Diagnostics filename / next action:
```

## Local candidate checks - 20 October 2026

Base commit: `91847b19b5ba4427d13b490de121622f9c61f768`, **plus uncommitted
changes**. Native build identifies itself as
`v0.4.7-rc1-3-g91847b1-dirty`; this is a build label, not a published version.
Host: macOS ARM64, Go **1.27.3**. No Pi or flight controller was connected.

| Check actually run | Result |
|---|---|
| `go test -race -count=1 -coverprofile=cover.out -covermode=atomic ./...` | Pass; 199 cases reported, 12 packages; 68.3% total statement coverage |
| `go vet ./...` | Pass |
| golangci-lint **2.15.0**, built with Go 1.27.3, default configuration | Pass, zero issues |
| Decoder fuzz, 20 seconds, two workers | Pass; 1,952,706 executions; synthetic inputs, not hardware evidence |
| govulncheck **1.9.0** | Pass; zero reachable vulnerabilities / zero in imported packages; one required-module advisory reported as uncalled |
| ShellCheck: both installers, boot/Wi-Fi helpers, image build and stage scripts | Pass |
| actionlint **1.7.12**: CI/security/image/release workflows | Pass; syntax/static validation only |
| Installer fixtures | Pass; two upgrades preserve credentials/country/custom settings; missing/corrupt release payloads rejected before installation. macOS uses a BSD sed adapter; native GNU sed/systemd execution awaits Linux CI |
| Settings transactions | Pass; second-write, restart, hash-write, concurrency, pending recovery and legacy-hash/restart failure paths |
| Native build and Linux ARM6, ARM64, AMD64 cross-compilation | Pass; Linux outputs statically linked and all below 10 MiB |
| Built native HTTP server on loopback, synthetic data | Pass; dashboard, shared waiting status, suffix-range bytes and two flushed SSE events |
| Local HTML asset links / `git diff --check` | Pass |

Native binary: 7,646,210 bytes. ARM6: 7,995,552 bytes; ARM64: 7,471,264;
AMD64: 8,085,664. These are build sizes, not appliance RAM or speed measurements.
Generated binaries and coverage stay outside tracked source.

**Not run:** native Linux CI, Docker build, full SD-image build/inspection,
Pi systemd/polkit ownership and startup, Wi-Fi/DHCP/client compatibility,
physical USB/controller/LED tests, power cuts, real BBL decoding, measured field
timings, interactive browser testing and independent pilot feedback. The local
Docker engine was unavailable (`docker.sock` absent). No image checksum,
hardware matrix, screenshot or field result was manufactured.

GitHub read-only inspection on this date:

| Setting | Observed result |
|---|---|
| Repository visibility | Public |
| Main protection / required checks | API says branch not protected; repository rulesets list empty |
| Dependabot alerts | API reports disabled (403); alerts could not be enumerated, so no clean-alert claim |
| Dependabot security updates | Disabled |
| Secret scanning / push protection | Enabled; no open secret alerts returned |
| Non-provider pattern scanning / validity checks | Disabled |
| Private vulnerability reporting | Disabled |

These settings were inspected, not changed. CodeQL workflow presence does not
prove a successful scan of this uncommitted candidate. The licence-holder name
and historical compression distributions also need maintainer review; see
[provenance](docs/PROVENANCE.md).

**Next external action:** review and record the candidate as a commit, then run
Linux CI and build/inspect the exact image with Docker running. Flash it to a
spare card in a **Pi Zero 2 W**. With erase/cleanup off, cold boot, join Wi-Fi,
change settings/reboot, and copy a real Betaflight SPI-flash dump larger than
64 KiB. Download/open it in Blackbox Explorer, test interruption/reconnect, and
retrieve diagnostics from the card on this Mac. Verify the browser user can
reopen the settings lock and save after a root boot apply. Record the fields
above. Then give that same image and guide to **one independent pilot** and
record their outcome and confusion. Fix observed failures before publication.

The source is ready for that image-build/physical-evaluation step. It is **not
release-ready**: native Linux/image/hardware evidence and the maintainer actions
above remain open. Further internal cosmetic work is not the next priority.
