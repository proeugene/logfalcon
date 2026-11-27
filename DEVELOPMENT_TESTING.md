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


### Remaining review fixes: 21 October 2026

Corrected updater runtime/inbox access and shared-lock ownership under its
restricted capability set. The root worker repairs ownership/mode on the held
file descriptor without replacing the lock inode; symlinks, non-regular files
and hard-linked updater locks are rejected. Sync now inspects update recovery
state after acquiring that lock. Skipping optional setup requires the same
confirmed hostapd recovery as other settings changes. The guide no longer lists
the already-fixed transfer-size and blackbox-field defects as current defects.

The full macOS Go race/coverage suite passed (sync 70.4%, update 71.7%, web 80.8%),
as did vet, pinned default golangci-lint (zero issues), native and Linux
ARM6/ARM64/AMD64 builds. Linux container race tests passed for installer
integration, update, settings, web and sync. The Linux permission regression
runs the real installer with the capability set read from its service unit and
UMask 0077, with a service-owned runtime directory/inbox. Both worker-first and
service-first startup passed, including exclusive locking, unchanged lock inode,
subsequent service-account access and cleanup of a service-owned invalid request.
Additional regressions cover blocked and retryable settings recovery, checking
update state under the lock, and rejecting unsafe lock files.

These are automated process/filesystem checks. No SD image was rebuilt, no
systemd-managed appliance update was installed and no Pi/FC/power-loss acceptance
was performed in this pass. Exact-image physical acceptance remains required.

### Branch-based OS cache and Linux lint: 21 October 2026

The shared local/CI image builder now builds base OS stages 0–2 from scratch on
main and release tags (`v*`), and reuses a compatible snapshot on other named
branches. Unknown/detached refs are clean. `--clean` forces a fresh OS, and
`--cache-policy` reports the choice without Docker. Both image workflows use
this builder, with branch-scoped Actions restore/save on development branches.
Fresh snapshots are published only after complete image inspection/checksums;
the current application/export stages always run. The source snapshot now also
includes the public update-key directory used by the shared image installer.

Native and Linux-target pinned default lint passed with zero issues. The two
unchecked cleanup returns in the Linux-only update permission regression were
fixed. The full macOS Go race suite and vet passed. Linux container race tests
passed for integration and update, including policy selection, local branch/tag
selection, forced clean, warm reuse, invalidation, corrupt/incomplete archives,
failed-build preservation, link/mode preservation and fresh application/export
execution. OS-stage lifecycle tests use a small fixture, not a complete OS.
ShellCheck, bash syntax checks and actionlint passed for the changed scripts and
workflows.

The actual `bash pi-gen/build.sh --check` run passed on this checkout using
Docker on Apple Silicon: ARM chroot, loop/mount probes, all Linux race tests,
vet, govulncheck and ARM6/ARM64 binary builds. Govulncheck reported no affected
code (one unused module advisory). Its log is in ignored local output
`pi-gen/deploy/20261021T134501Z-60861/build.log`.

No full cold/warm SD image build, remote Actions execution or physical Pi test
was performed for this change. The first real development image build must
populate the cache; a second run is needed to measure the actual saving. Main
and release images still require their fresh full build and physical acceptance.

## Reliability and startup candidate — 10 November 2026

This phase targets Pi Zero W and Pi Zero 2 W equally, with offline collection
working without a phone. Source changes do not establish boot speed, Wi-Fi
readiness, controller compatibility or power-loss behaviour on either model.

Implemented in this working tree:

- Canonical polkit policy replacement and removal of the obsolete updater name.
  Signed activation journals both paths for rollback; both bootstrap installers
  also remove the obsolete policy after installing the canonical one.
- Signed activation reloads systemd/udev and refreshes running journald/LED and
  web processes. Status requests reboot for boot-only integration and clears the
  reminder only on a different boot with matching application health. An old
  worker installing its first revised release still executes its old activation
  logic; follow the transition instructions in docs/OFFLINE_UPDATES.md.
- Go CI runs on every branch push. Individual and selected-ZIP downloads renew
  bounded write deadlines, retaining range/HEAD behaviour and stall limits.
- Read-only USB/MSP readiness probes replace the fixed three-second delay.
  Unchanged, confirmed boot inputs use idempotent hostapd start; failed recovery
  still blocks startup. Copy/web no longer wait for the optional diagnostics
  lifecycle service. Debug/journal collection remains enabled and bounded.
- The existing completion panel identifies the saved recording and its recorded
  retention outcome. Known failure codes give short recovery instructions.

Verification on the candidate working tree:

- Before-fix regression failures reproduced the canonical/obsolete policy
  mismatch, missing activation commands, the individual-download cutoff and
  redundant restart of unchanged boot settings. The bootstrap fixture also
  reproduced the stale policy left by the source installer.
- Full macOS race/coverage suite passed (70.7% total statement coverage), as did
  vet and the pinned default golangci-lint 2.15.0 on native and Linux targets.
- Full Linux race suite and vet passed in the existing Docker builder, including
  the root/service-account permission regression and native installer fixtures.
  Linux ARM6, ARM64 and AMD64 builds and the native macOS build passed.
- Twenty-second bounded decoder fuzzing passed (1,684,401 executions).
  govulncheck 1.9.0 found no affected code or imported-package vulnerabilities;
  one required-module advisory was reported as uncalled.
- ShellCheck passed for the touched scripts and configuration helpers;
  actionlint passed for the changed CI workflow. Whitespace checks passed.
- Rendering/API tests cover completion context, escaped controller identity,
  exact recording links, uncertain outcomes and recognised recovery guidance.
  A synthetic-data browser preview was prepared, but interactive verification
  was not completed: sandbox socket binding was denied and the subsequent
  server-start approval was aborted. No live browser result is claimed.

These tests use fake controllers, temporary files and stubbed system commands;
they do not prove real systemd activation, USB readiness, LED patterns or power
loss behaviour. At this automated-check checkpoint, no complete new SD image or
physical Pi/controller test had been run. No boot-time saving has been measured
on either Pi model.

### Exact candidate image built — 10 November 2026

The subsequent full build completed in
`pi-gen/deploy/20261110T175342Z-27921/`. Its build log reports successful image
inspection with no obvious configuration issues and a compressed size of
497,100,748 bytes, below the 600 MiB limit. This is a clean OS build (cache
reused: 0), Raspberry Pi OS Bookworm armhf with the ARM6 application binary;
the same candidate image is to be evaluated on both Pi models.

- Image: `2026-11-10-logfalcon.img.xz`
- Image SHA-256:
  `814a95dbd66b87110aa51f1f3f5edf4afa4d16a9999c481cb628b7ea24418235`
- Base commit: `8276d66ffa88c460b2c27862a9421a93fcd39ca8`
- Version: `v.0.4.8-rc1-4-g8276d66-dirty` (includes the local candidate changes).
- Source manifest SHA-256:
  `c8cbc91bfb8b913e50e00ef60f58dc65c76516c2fd56f875be8fa88a4e0aae69`

Independent local verification passed for all four entries in `checksums.txt`,
and every entry in `source-files.sha256` matched the current checkout. Preserve
the image, `.info`, `build-info.txt`, source manifest, checksums and `build.log`
together. This records a built and inspected candidate, not physical acceptance:
neither Pi model has a recorded boot/copy result or measured timing improvement
for this image yet.

### First field report — 11 November 2026

The operator reported a short first boot (untimed), a slow waiting blink, and a
fast blink while collecting from a DARKSTAR20. Safari reached the hotspot. The
in-progress screen showed 1.0 of 8.0 MB at 62 KB/s, with 115 seconds estimated
remaining. The operator subsequently saw completion and repeated blinking.
After unplugging the controller the LED went dark and Wi-Fi became inaccessible;
the operator cycled the Pi power connection. Cause and actual transfer duration
are unknown. The pilot subsequently downloaded the saved BBL and opened it in
Blackbox Explorer. The 8,388,608-byte download contains eight DARKSTAR20 log
headers (Betaflight 2026.6.2), all with year-zero start times. The exported
previous-boot journal shows a separate failed read at offset 106496 (serial
port closed), followed about 33 seconds later by a Wi-Fi client association.
Boot LED writes to a missing led0 brightness path continued past 190 seconds
of uptime. The previous lifecycle was incomplete and both kernel sections
were unavailable. This export cannot identify the cause of the reported outage.
Phone-free completion, the precise success
pattern and retention on the FC still need confirmation.

### Exact-image baseline and comparison

1. Keep the baseline and candidate as separate artifacts/cards. The pre-change
   baseline is commit `8276d66`; record its full commit, image SHA-256 and package
   inputs. If no baseline image exists, build it in an isolated checkout without
   resetting this working tree. For the candidate, from the repository root run
   `bash pi-gen/build.sh --clean`. Keep the generated `build-info.txt`, source
   hashes, package `.info`, `checksums.txt` and build log with the image. A dirty
   candidate must be identified by those input hashes as well as its base commit.
   In the output folder, run `shasum -a 256 -c checksums.txt` before flashing.
2. Flash the exact image to a spare card. Record Pi model, card, supply, cable,
   FC board/firmware, byte count and image hash. Keep erase and storage cleanup
   disabled in the effective application configuration. Use the same retained
   controller dump for baseline/candidate comparisons; do not infer speed from
   different file sizes or from builds with different OS package inputs.
3. On **each** Pi model, run at least five ordinary cold boots with the FC
   attached at power-on, then five trials plugging the FC into an already-running
   Pi. Test first boot/device identity, changed boot settings and pending recovery
   separately; do not mix those results into the ordinary-boot sample. Keep SD
   type, supply, cable and FC firmware constant, and note unavoidable differences.
4. Use a timed video or an external power timestamp for **power applied → saved
   indication**. OS logs omit early firmware/kernel startup. Record separate
   checkpoints: MSP controller ready, copy started, durable publication, completed
   LED indication, hotspot visible, phone joined and dashboard loaded. A listening
   HTTP socket is not evidence that the hotspot or phone is ready. With no phone
   connected, copying and the LED must still complete correctly.
5. On the Pi, capture the actual units (replace ttyACM0 if necessary):

   ```sh
   systemd-analyze critical-chain logfalcon@ttyACM0.service logfalcon-web.service
   sudo journalctl -b -o short-monotonic --no-pager \
     -u 'logfalcon*' -u hostapd -u systemd-journal-flush.service
   ```

   Structured application messages include `checkpoint`, `boot_id`, `uptime_sec`
   and, for copies, `transfer_id` and `elapsed_sec`. Settings checkpoints are
   `boot_settings_start`/`boot_settings_ready`; the web checkpoint is
   `http_listening`. Copy checkpoints include `started`, `controller_ready`,
   `identified`, `flash_ready`, `copy_started`, `copy_flushed`, `copy_verified`,
   `copy_published` and `finished`; optional erasure adds its own checkpoints.
   `copy_published` precedes any optional erase; use successful `finished` plus
   the observed LED for the complete collection outcome. Retain failure trials.
6. Download the exact recording linked by the completion panel, compare bytes
   and saved hash, and open the real BBL in Betaflight's Blackbox Viewer. Verify
   retained controller data, interruption/reconnect, low-space guidance, stale
   status, settings recovery, slow single/range/ZIP downloads, and update/rollback
   behaviour. Follow the full acceptance matrix above, including diagnostics
   retrieval and one independent pilot's attempt; timing alone is not acceptance.

Record median and slowest observed timings, failures/attempts and CPU/RAM where
available **separately for each model and scenario**. Do not claim a percentile
from five samples. Set the next performance target from this baseline. Removing
a three-second fixed wait, avoiding a redundant AP restart and removing optional
startup dependencies are implementation facts; their end-to-end savings remain
unmeasured. Separating configuration recovery from Wi-Fi activation entirely, or
combining first-boot changes into one restart, remains a hypothesis for a later
measured change. Neither is required for this candidate.

### LED, diagnostics and recording identification candidate — 11 November 2026

Read-only inspection of the mounted field snapshot confirmed useful service
sections and empty boot-list/kernel sections. Re-running its exact commands in
the existing Bookworm tools container (systemd 252) reproduced unsupported
`--no-legend` and `--kernel` options. The exporter already runs as root; adding
privileges would not fix those argument errors. It now uses supported arguments,
retains each useful/partial section independently and records section-specific
stderr, empty-history or two-second timeout reasons. Per-section 256 KiB,
snapshot 2 MiB, redaction and the separate low-priority export service remain.
The separately reported post-success outage still has no established cause.

The boot helper hardcoded `led0`, while the Go backend also recognised `ACT`.
Both now discover those names; missing/disappearing attributes disable writes
without repeated journal failures. The boot helper starts before `sysinit.target`;
the status service starts afterwards and synchronously stops/reaps the boot
owner before launching the status process. No startup conflicts or direct
boot/status ordering edge can discard wanted jobs or obstruct that stop job.
Boot stop and status start have three/ten-second bounds respectively; restarting
status does not restart the boot helper. Waiting remains the slow
pulse, work the fast blink, saved success the persistent double blink and error
SOS. No phone-free hardware success is claimed from these source changes.

Read-only inspection of the pilot's 8,388,608-byte dump found eight DARKSTAR20
headers, all with year-zero times. The file and diagnostic snapshot remain outside
Git. New collection manifests retain optional per-header craft/start evidence
from the existing write stream: at most 256 headers, 64 KiB per header and 8 KiB
per line. Valid start strings preserve timezone offsets; byte offsets locate the
headers. The final retained header is completed across writes even after reaching
the 256-header limit; later headers are ignored. Unknown times never become
collection times. Existing recording details
show known starts and an unknown count. Individual attachment names use safe craft
text plus a stable session-derived identifier; repeated clocks and older sessions
have focused regressions. File bytes, storage layout, routes and ZIP session
folders are unchanged. Parsing supplies no input to verification/erase decisions.

Automated checks passed: full Go race/coverage suite, final changed-package race
reruns, Go vet, Linux installer/integration and changed-package tests in the
Bookworm tools container, ShellCheck for both installers and boot/Wi-Fi/LED
helpers, native macOS and Linux ARM6/ARM64/AMD64 builds, and diff whitespace
checks. Following review, targeted race/Linux regressions cover final-header
timestamps split across writes and conflict-free handover dependencies.
An isolated Bookworm systemd 252 manager running as container PID 1 also passed
`systemd-analyze verify`, simultaneous wanted boot/status/web startup through a
fresh fixture sysinit target, manual status restart and failure-driven automatic
restart after killing its main process. Original unit ordering/start semantics
were retained with fixture commands; an exclusive file lock represented LED
ownership. Events were boot started, boot stopped, then status started. Web
became active; status remained active and boot inactive after both restarts.
This checks manager ordering and process handover, not a Pi boot, its kernel
journal or ACT hardware.

Shortest exact-image acceptance, repeated separately on **Pi Zero W** and
**Pi Zero 2 W** with the same retained FC dump, erase/cleanup disabled:

1. Record candidate image checksum/source hashes, board, power supply, SD card and
   FC firmware. Cold boot without a phone or FC: slow waiting pulse. Connect FC:
   fast work blink, then persistent double blink after the saved-copy check.
2. Unplug FC and wait at least 60 seconds: double blink persists. Join Wi-Fi,
   download the BBL, verify its hash and open it in Blackbox Viewer. Reconnect the
   retained dump and download the second copy: distinct craft-labelled filenames,
   unchanged bytes, eight unknown start times for this field dump.
3. Interrupt a further copy by unplugging USB: SOS, no success indication and no
   erase. Reboot and retrieve diagnostics: boot list and current/previous kernel
   sections, or a specific reason for unavailable history; no recurring missing
   LED-path errors. Confirm boot helper is inactive while status owner is active.

These physical steps have not been run for this candidate. Copy-speed tuning is
left to Eugene after comparing the same retained dump with Betaflight Configurator.

### Field package review, 18 November 2026

Before edits, all 120 hashes in the ARM6 field package's source manifest matched
the checkout at base `8276d66`. Its signature and artifact checksums verified.
After the review, 20 listed files differed. Rebuild the field package before
testing these fixes. The original package and its public key remain together in
`bin/field-update-AWIqgogQ/`.

The fake controller reproduced deletion of the saved session during an opted-in
erase. The same regression now retains the verified bytes through erase and
permits deletion afterwards. Other regressions cover recovery blocking a new
plug-in, boot health polling competing with USB collection, damaged status,
truncated downloads, adjacent recording headers and legacy collection times.
At 390 × 844, Chromium reproduced a previous success persisting through stalled
HTTP responses. The revised dashboard showed unavailable status and recovered
when responses resumed. A stalled recording list allowed live polling to continue.

The full macOS Go 1.27.4 race suite passed with 72.1% statement coverage. The
Bookworm Go 1.27.3 race suite passed, including Linux installer and updater
permission fixtures. The final web changes also passed on Bookworm. Vet,
default lint from the v2.15.0 source, ShellCheck, 20-second decoder fuzzing,
native and Linux ARM6/ARM64/AMD64 builds passed. These checks used synthetic
data. The post-success LED and Wi-Fi outage still needs a timed unplug trial on
both Pi models as described above. No new image, Pi install or rollback was tested.

### Field-key SD image, 18 November 2026

The 32-bit Bookworm ARM6 candidate for Zero W and Zero 2 W is
`pi-gen/deploy/20261118T164945Z-5877/2026-11-18-logfalcon.img.xz`.
Image SHA-256 is `4c1c89b2970a4356bca3b77f37d196255c6683e52766ab27c2b80d9d709c4cce`.
The Go 1.27.3 build stopped on GO-2026-6859. The Go 1.27.4 build passed
race tests, vet and govulncheck (zero reachable findings). Mounted-image
inspection compared the installed field-2026.pub with the source key and
identified the 32-bit ARM executable. The host independently checked XZ
integrity, all four artifact hashes and the source manifest against the
checkout. Physical boot and Wi-Fi update acceptance remain open on both models.

### Blackbox capture check, 25 November 2026

The local Go 1.27.4 race suite passed with 73.7% statement coverage. Native
Go vet passed. Default golangci-lint v2.15.0 passed on macOS and for Linux AMD64.
Both Pi builds passed, with Linux ARMv6 and ARM64 confirmed in the Go build
metadata. The copy tests cover retained references, a failed intervening copy,
exact repeats, a verified append, changed padding, an unconfirmed erase and
an unavailable capture-check file during both retention and opted-in erase.

The retained 8,388,608-byte DARKSTAR20 BBL was replayed twice on macOS through
the simulated MSP controller. All eight recording headers declared filtered
and unfiltered gyro on X, Y and Z. A separate read of their field-name lines
agreed. The second copy identified the first as its identical reference.
This used the pilot’s original file as read-only input and temporary storage.

Chrome at a 390 × 844 viewport showed the check on initial load, removed the
previous result during identification and displayed the next result through
live polling. A synthetic appended header declared filtered gyro with unfiltered
gyro omitted. Its details and reference links opened the intended sessions,
and the original download matched the fixture bytes.

Physical acceptance remains open. On an exact candidate image, keep erase and
cleanup off and collect the retained DARKSTAR20 flash twice. Then use a test
controller with spare flash, collect a baseline and add one recording with
unfiltered gyro logging omitted. Have a pilot identify the added recording on
the phone and cross-check its fields in Betaflight’s Viewer. Record the image,
board and what the pilot decided to do before the next pack.

### Local code and appliance audit, 27 November 2026

The checkout was `test` at `8276d66ffa88c460b2c27862a9421a93fcd39ca8` with
uncommitted changes. The SHA-256 of the 148-entry source manifest for the
builder's input paths was
`097ded241e0d8cdb09066dc66be7834d104e56ec8461842905ad7b92938d25f0`.
The four checksums from the 18 November image still verified. That image
predates these fixes and has a different source manifest.

Two regressions failed against the incoming source. A simulated controller
that appended data during copying received an erase command. An opted-in
storage cleanup removed files outside completed recording directories. The
repaired candidate kept the saved copy and sent zero erase commands when flash
use grew. Cleanup retained unrelated files, partial copies and recordings with
a length mismatch. The installer fixtures also cover an existing appliance
whose settings have disappeared and an upgrade with shutdown already queued.

| Check | Observed result |
|---|---|
| macOS ARM64, Go 1.27.4 | Fresh race/coverage suite passed, 74.0% of statements. Vet passed. |
| Bookworm Linux ARM64, Go 1.27.4 | Race tests for integration, settings, update, storage and sync passed on a fixed source snapshot. The compiled updater permissions test passed as root for both worker-first and service-first lock acquisition. |
| Default golangci-lint 2.15.0 | Zero issues on native macOS and Linux AMD64 target. |
| govulncheck 1.9.0, 24 November database | Zero reachable findings on macOS ARM64 and Linux ARMv6. The indirect `x/sys` version had a Windows-only advisory, GO-2026-5024. |
| Protocol and build | Final 20-second decoder fuzz run passed. Native macOS, Linux ARM6, ARM64 and AMD64 application binaries and the native update-package tool built. |
| Scripts and workflows | ShellCheck passed on all 16 shell scripts and actionlint passed. The revised pull-request permissions step was exercised locally in its compiled-fixture form. |
| Dashboard | Headless Chrome at a 390 × 844 window rendered a fixture recording link. Requesting that link returned the three original synthetic bytes as an attachment. |

Physical acceptance remains open. On a spare card, record the **new image
checksum**, Pi model and Betaflight version. With erasure and cleanup off,
collect a retained recording, unplug, check that Wi-Fi stays available and
open the downloaded file in Blackbox Viewer. Record the outcome on each Pi
model for which compatibility will be claimed and obtain one independent
pilot's result.

### Field Kit browser checks, 27 November 2026

The design in `b1b0b07` was checked in headless Chromium at 320, 390 and 1040 CSS
pixels wide. The homepage, pilot guide and four dashboard pages (Recordings,
recording details, Updates and Settings) rendered at all three widths. Each
surface loaded the Peregrine logo and favicon.

On the local Go server, the Recordings page updated through waiting, copying,
interrupted, failed and completed states. The downloaded 4 MiB sample matched
the fixture’s SHA-256. The [dashboard screenshot](docs/images/dashboard-preview.png)
shows that simulated transfer. Physical Pi and flight-controller acceptance remains open.
