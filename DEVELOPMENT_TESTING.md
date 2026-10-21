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

## Image-build follow-up - 20 October 2026

[Build 39463006095](https://github.com/proeugene/logfalcon/actions/runs/39463006095)
ran against commit `56a2e7969d9d4e8ec99cd03ed378ade3dfc6e96a`. Image construction
and PiShrink completed. The job then rejected the approximately 2,159 MiB raw
image against a 600 MiB raw-size limit, before compression/upload. This updates
the earlier local record: a CI image build has now been attempted, but the
pipeline has not completed and there is no physical acceptance result.

The [pinned pi-gen partition layout](https://github.com/RPi-Distro/pi-gen/blob/1c2abf50924d5bfee3527657af74ddfb1da52904/export-image/prerun.sh#L12)
reserves 512 MiB for boot, making the old total raw budget unrealistic. Both
image workflows now use the same size checker: at most 3 GiB raw and 600 MiB
compressed. These are separate engineering budgets; the compressed size is
still unknown. No package or licence notice was removed to force this check.
Chroot size reporting now uses directory usage and installed-package sizes;
the old `df /` output described the build host's 145 GB filesystem.

Local verification of this follow-up: integration suite passed (14 cases),
including sparse fixtures matching the reported failure size and exact
raw/compressed boundaries; ShellCheck, actionlint, integration-package lint and
diff whitespace checks passed. Sparse fixtures test the limits only; they are
not real images and do not establish compression ratio or bootability.

Next: commit/push this patch, then start a **new** image workflow on that commit.
Re-running the old run uses its original code. Check the compressed size and
image contents before flashing and carrying out physical acceptance above.
The complete image pipeline has not been rerun for this local patch.

## One-command local Docker builder - 20 October 2026

Run `bash pi-gen/build.sh` from this repository on macOS or Linux. The previous
Linux-host wrapper has been replaced: Go, QEMU, pi-gen, filesystem tools and
inspection now run inside a pinned Linux Docker image. Docker is the only
additional host dependency. `--check` runs the Docker capability probes, source
checks and Pi binary builds without creating the OS image.

The full command snapshots the current build inputs, checks source, builds the
32-bit Bookworm image with the ARM6 binary, runs pinned PiShrink, checks the raw
budget, inspects the image, compresses/tests it and checks the download budget.
Each output folder under `pi-gen/deploy/` contains the final `.img.xz`, package
`.info`, `checksums.txt`, source hashes, build identity and log. There is no
flashing or publishing step. The privileged container uses a temporary Docker
work volume; source is mounted read-only. Per-run ARM registrations and loop
devices are cleaned up, including a separate cleanup pass after interruption.
Inspection redacts hotspot passwords before they reach the build log.

Actual local evidence: Docker Desktop 29.9.1 on Apple Silicon completed
`bash pi-gen/build.sh --check`. A real armhf binary ran within a chroot, a loop
filesystem was created/mounted/unmounted, the complete Go race suite and vet
passed on Linux, govulncheck found no reachable vulnerabilities (one uncalled
required-module advisory), and ARM6/ARM64 binary builds passed. The final check
log is `pi-gen/deploy/20261020T195623Z-64881/build.log` (local/generated, ignored).
The integration suite includes 17 passing cases, with new checks for help,
native-inspection access and credential redaction. ShellCheck, integration lint,
actionlint and whitespace checks passed.

At this initial preflight checkpoint, the full local image sequence had not
been executed. Later full-build results are recorded below. A Docker capability
probe is not proof of a bootable image; physical acceptance remains separate.

### Apple Silicon bootstrap correction - 20 October 2026

The first full local runs failed before creating the base root filesystem:
`setarch linux32` was rejected by Docker's aarch64 kernel. The earlier probe
used a direct chroot and therefore missed pi-gen's personality/capsh path.
Invoking upstream with `bash ./build.sh` also discarded its shebang's `-e`,
allowing subsequent stages to run against a missing root filesystem.

The wrapper now probes the actual capsh/chroot path, preserves linux32 where
supported and bypasses it only on an aarch64 kernel with working ARM emulation.
A guarded adjustment to the pinned pi-gen copy uses that same route and
propagates bootstrap failures. Upstream is invoked with `bash -e` so stage
errors stop the build. These adjustments affect the temporary pi-gen copy;
the upstream pin remains unchanged.

Verification: all 22 integration cases and integration lint pass; ShellCheck
and whitespace checks pass. Regression cases cover both personality routes and
failure propagation. Docker `--check` passed with the revised chroot probes,
Linux race suite/vet/vulnerability scan and ARM6/ARM64 builds
(`pi-gen/deploy/20261020T201051Z-69477/build.log`). The subsequent full run
(`pi-gen/deploy/20261020T201128Z-70007/build.log`) passed bootstrap but stopped
correctly on a Raspbian mirror HTTP 400 during firmware downloads; apt also
reported a pipelining/server-proxy response problem. The builder now disables
HTTP pipelining and sets three retries while installing packages, then removes
that temporary apt configuration before export. A regression case checks its
installation and removal.

The retry completed successfully (exit 0), recorded in
`pi-gen/deploy/20261020T201821Z-72027/build.log`. OS creation, application
installation, PiShrink, offline image inspection, compression, `xz -t` and all
four output checksum checks passed. The shrunk raw image is 2,263,908,864 bytes
(within the 3 GiB budget); the compressed image is 494,095,852 bytes (within
600 MiB). Inspection identified the statically linked 32-bit ARM application
and reported no configuration issues.

Artifact: `pi-gen/deploy/20261020T201821Z-72027/2026-10-20-logfalcon.img.xz`

SHA-256: `fc91e7d325c08709cffc42b23452cdd68f809a747d57f24ffbe6657601515178`

This is a local image from the modified `test` working tree at base commit
`56a2e7969d9d4e8ec99cd03ed378ade3dfc6e96a`, with exact input hashes beside it.
The additional temporary-download-options regression case was added after the
build snapshot and passed locally; it changes no application/image code.
Next: flash this exact artifact and carry out physical acceptance above.
Boot, Wi-Fi, USB/controller copying and power-cycle behaviour remain untested.

## Onboarding/dashboard/offline update candidate, 21 October 2026

Based on the clean test branch at 66b21ff, with the local implementation diff.
No commit, push, release, secret setup or physical acceptance is implied.

Implemented: first-installed-device identity (never generated in the shared
image), optional setup/skip persistence through settings transactions, literal
password handling, country and reconnect warnings, automatic completed-session
insertion, explicit retained/erase outcomes, version/update UI, signed complete
release packages, cumulative key delegation, fixed privileged updater with
durable rollback, release package CI and the documented bootstrap boundary.
Betaflight 2026.6.2/API 1.48 has tagged-source and synthetic-fixture coverage.
iNav — Experimental; copying currently disabled.

Automatic evidence in this session:

- Go 1.27.3 race/coverage suite and vet passed on macOS.
- Pinned golangci-lint 2.15.0 (built with Go 1.27.3) passed using defaults.
- Twenty-second decoder fuzzing with two workers passed, approximately 2.01
  million executions.
- ShellCheck passed for both installers, settings/Wi-Fi scripts, image inspection
  and the changed image installation script. release.yml passed actionlint.
- Native and Linux ARM6, ARM64 and AMD64 application builds passed. Host builds
  used a temporary GOCACHE and -buildvcs=false because the sandbox cannot write
  Go's normal cache; these are compilation checks, not signed release artifacts.
- All three ABI package-builder/verification commands also passed with an isolated all-zero test seed in a temporary directory. These smoke artifacts use an artificial version and a dirty checkout, so they are not release artifacts.
- Temporary-file/isolated-key tests cover signature/hash tampering, wrong
  architecture and executable identity, unsafe/duplicate/symlink archives,
  upload/expanded limits, key delegation without an intermediate package,
  insufficient space, copy-lock contention, interrupted activation, failed
  startup/restart, rollback, actual-tag config fixtures and incomplete config.
- HTTP upload/review/queue tests use an isolated signed ELF fixture and exercise invalid files, stale CSRF, changed-review rejection, queued-state reporting and safe abandoned-upload cleanup.
- HTTP handler/template checks cover setup/skip persistence, existing-name
  prefill, no current-password rendering, blank/literal passwords, stale CSRF
  guidance and the completed-list refresh contract. These do not execute a real
  mobile browser or simulate a Wi-Fi reconnection.
- A pre-existing storage test compared a UTC-created session date with the
  local date around midnight; its expectation now uses UTC.

Blocked/unverified: govulncheck 1.9.0 built with Go 1.27.3 could not reach
vuln.go.dev (DNS/network restriction), so there is no actionable current scan.
Native Linux installer integration/image build was not run: Docker socket
access is denied in this session. macOS installer fixtures passed separately.
The local browser preview was attempted, but the sandbox forbade its listening
socket and automatic approval review could not run because of an account usage
limit. No rendered phone-size browser, physical phone/Wi-Fi, FC, image boot,
systemd activation or physical rollback result is claimed.

### Next external acceptance: exact spare card and one pilot

1. Provision signing as documented in docs/OFFLINE_UPDATES.md. Complete the
   blocked vulnerability scan and native Linux CI, then build one exact image
   and ABI-specific package from one recorded commit. Record SHA-256 for both.
2. Flash a spare card; leave erase and cleanup disabled. Boot two fresh cards
   independently: confirm different SSIDs, correct temporary password, no internet
   expected, setup offered and Skip persists. Copy a real FC without a phone.
3. On a real small phone, change name/password/country; observe the warning and
   disconnection, rejoin and confirm effective settings. Reboot and upgrade:
   verify identity, credentials, country, debug and all app values are retained.
   Exercise blank-password preservation and boot-file recovery after a bad edit.
4. Copy a real Betaflight controller (record board and full firmware version).
   Confirm waiting/copying/checking/completed states, live new-log insertion
   without closing an existing details/download action, retained-data label and
   successful Blackbox Explorer opening. Record interruptions separately.
5. Upload the exact signed package, review version/ABI, install, reconnect and
   require version/commit/healthy confirmation. Attempt during copying: verify
   activation waits and no new copy starts during activation. Reject a corrupted,
   wrong-ABI, unsigned and older package before activation.
6. On the disposable spare card, exercise failed new-app startup and controlled
   interruption during activation. Reboot and confirm previous application
   restoration, retryable recovery and unchanged config/logs. Capture bounded
   diagnostics; retain a separate known-good card throughout.
7. For each supported historical-image row, bootstrap and direct-jump to the
   latest package without an intermediate release. Original config fixtures
   alone do not accept that row physically; record the exact old image/layout.
8. Ask one independent pilot to attempt setup, a real copy/download and an
   offline update. Record confusion/failure before another internal polish cycle.

### Dashboard redesign: 21 October 2026

Implemented a shared offline visual shell for dashboard, Settings, update upload,
update review and request errors. Transfer stages use the published status;
missing/unknown status does not imply readiness or completion. Compatibility
warnings remain visible. Session retention/erase badges come from manifest facts.

Local checks after the redesign: `go test -race -cover ./...`, `go vet ./...`,
and pinned golangci-lint passed (web coverage 79.1%; lint reported zero issues).
Regression checks include status/HTML escaping, compatibility warnings, bounded
progress, newest available download selection and preservation of the session
cache order. Existing settings and signed-update handler tests passed.

Browser checks used a separate localhost:8086 render harness with clearly labelled
sample sessions and simulated transfer states. Desktop and 390px phone viewports
were inspected; dashboard, Settings and Software update had no horizontal
page overflow at 390px. Waiting/copying/completed displays, the Wi-Fi review
modal, and refresh preserving an expanded session were checked. No browser
warnings/errors were reported during these checks. No credentials were changed
and no update was installed in this preview. The temporary harness is not shipped.

This is local browser evidence, not physical phone, captive portal, Wi-Fi restart,
controller or installation acceptance. The previously built image predates this
redesign. Rebuild the exact candidate image, then test on the Pi and a real phone
before treating these screens as appliance-validated.

### Pilot recording context: 21 October 2026

Added optional recording titles (120 characters), notes (2,000 characters), and
controller nicknames (80 characters). Notes are session sidecars; nicknames are
stored in the existing controller UID directory. Neither edits transfer evidence
nor gates automatic collection. The UI distinguishes collection time from flight
time and explains overlapping flash copies and the separate Betaflight Viewer
workflow. Each editor section saves separately and warns about unsaved edits.

After this change, the full Go race/coverage suite, vet and pinned default lint
passed (web 79.6%, storage 63.9%; lint zero issues). Tests cover save/reopen/clear,
shared nicknames, old recordings without annotations, escaping, bounded input,
stale CSRF, path traversal, symlinks, corrupt/oversized metadata, failed saves,
and preservation of `.bbl` and manifest bytes.

A localhost:8087 preview used the actual HTTP handlers and disposable sample
recordings (not decodable flight logs). Browser checks saved/reopened titles and
multiline notes, saved a nickname visible on both sample recordings, and retained
annotations across a preview process restart. Desktop and 390px phone layouts
were inspected; dashboard/editor had no horizontal overflow. The unsaved-other-
section confirmation was triggered. No browser warnings/errors were reported.
No real controller, installed appliance, Wi-Fi change or software update was used.

The existing SD image still predates these changes. Next acceptance remains a
rebuilt exact image on the Pi, followed by one pilot collecting, naming, finding
and downloading a real recording for analysis.

### Multi-select ZIP and simpler UI: 21 October 2026

Added selected recording export (maximum 100 per ZIP), including `.bbl`, manifest,
notes and controller-name metadata with controller/session paths. Selection is
preserved on refresh; new rows stay unselected and removed rows disappear.
Removed promotional headings/taglines, decorative status artwork, the duplicate
latest-copy card and repeated explanations. Dashboard, editor, Settings and
Software update now share a quieter layout; guidance remains in collapsed help.

Full Go race/coverage tests passed (web 80.5%); subsequent final web changes also
passed `go test -race ./internal/web`. Vet, pinned default lint and native build
passed; lint reported zero issues. Export tests cover multiple sessions, duplicate
selection, source bytes, notes/nicknames, invalid counts/CSRF/paths, missing files,
symlinks, corrupt notes, manifest size mismatch and aborted response writes.

The actual localhost:8088 handlers downloaded a ZIP containing both disposable
sample recordings. `unzip -t` checked all seven entries successfully. Browser
checks covered select-all, partial selection, preserving selection/expanded
details on refresh, new unselected rows, removed rows and visible export errors
when a selected file was temporarily unavailable. The fixture was restored.
Dashboard, editor, Settings and Software update were inspected at 390px width;
no horizontal overflow was observed. No JavaScript warnings/errors were reported
during the normal flow. These are local sample-data checks, not real log decoding,
physical phone/Pi/Wi-Fi acceptance or sustained large-archive measurements.

Rebuild the SD image and validate the actual pilot collection/download workflow
on the Pi before release. SD-card partition access has not changed.
