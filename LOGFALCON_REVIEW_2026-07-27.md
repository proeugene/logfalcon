# LogFalcon product and technical review

**Original review:** 27 July 2026  
**Updated:** 2 October 2026  
**Repository:** <https://github.com/proeugene/logfalcon>  
**Reviewed state:** `cf3a532` on `main`, plus local UI/documentation changes from 2 October 2026  
**Purpose:** decide whether the project still solves a useful FPV problem, whether it is ready to promote, and what must change before publication.

## Decision

LogFalcon remains viable, but only with a narrower proposition:

> Automatic blackbox collection and archival for pilots managing several aircraft: connect, archive, disconnect, keep flying.

The original broad proposition is no longer credible. LogFalcon is not the only laptop-free route to blackbox logs, and a Raspberry Pi plus storage, cables and power is not inherently cheaper or simpler than every commercial alternative.

Do not promote the current v0.4.6 release. Its principal flash-copy path fails for common log sizes, the dashboard cannot receive live status from the separate sync process, and the advertised iNav support is incompatible with current iNav behaviour. The appropriate next public version is an explicitly experimental, Betaflight-only `v0.5.0-beta.1`, after the release gates in `RELEASE_PLAN.md` have passed.

**Confidence:** high on the reproduced software defects and process-status mismatch; product demand and physical reliability remain unvalidated. This is a proposed positioning, not evidence of adoption.

## Findings and completed work — 2 October 2026

The July review remains relevant: the transfer and service defects are still present at the same base revision. Today's work simplified the public explanation and saved-log browser; it did not repair the sync engine or establish field reliability.

### Implemented locally and automatically tested

- README reduced from 529 to 59 lines, with detailed instructions in the existing guide.
- Public website reduced to a clear introduction, intended workflow, compatibility and setup. Removed repeated before/after marketing, unsupported timing promises and unqualified iNav claims.
- Guide now distinguishes intended operation from current limitations, documents the destructive defaults, and corrects CLI examples: a serial transfer requires `--port`.
- Dashboard now focuses on saved logs, followed by storage and collapsed help. Download is prominent; API version, hash, manifest and deletion are under Details.
- Removed the dashboard's misleading process-local progress/completion display. It explicitly says live transfer status is unavailable. LF-002 remains open.
- Session badges report what the manifest records: no erase recorded, erase unconfirmed, or FC erased. Missing `.bbl` files do not receive download links.
- Low-space copy now reflects whether automatic cleanup is configured. Delete failures produce a visible error instead of silently succeeding.
- Added regression checks for avoiding false completion messages, distinguishing manifest erase outcomes and handling missing log files.

These are local changes, not a published release. Existing configuration defaults, LED behaviour, status endpoints and transfer logic remain unchanged.

### Evidence recorded today

| Check | Result and limit |
|---|---|
| Existing suite with race detector and coverage, before UI edits | Passed; 45.8% overall statement coverage, 1.9% in `internal/sync`, 0% in orchestration/read/verify/erase paths. Local macOS run; not directly comparable with July's platform-specific figures. |
| Focused temporary simulations | Reproduced LF-001 (1 MiB produces a zero-byte first request), LF-008 (recovered panic returns `ResultSuccess`) and LF-010 (supported=1/device=2 parsed as device=1). These probes were not added to the repository's permanent tests. |
| Full race suite after UI edits | Passed, including the new web regressions. No new coverage of the sync engine was added. |
| `go vet ./...` | Passed before and after UI edits. |
| Linux ARM6, ARM64 and AMD64 builds | Completed before UI edits; static executable formats checked. Final UI build verified natively. No target hardware execution. |
| HTML and documentation | Local links, guide anchors, unique IDs, HTML nesting and diff whitespace checks passed. |
| Browser inspection | Website, guide and sample-data dashboard inspected on desktop and at a phone-sized viewport. Disclosures worked; no horizontal overflow observed in the inspected phone views. |
| Lint / image / hardware | Linter unavailable locally; no fresh image build, Pi boot, real FC transfer, power-cut test or external pilot validation. |

Relevant commands: `go test -race -coverprofile=<temporary file> -covermode=atomic ./...`, `go test -race ./...`, `go vet ./...`, and cross-builds with `CGO_ENABLED=0` for Linux ARM6/ARM64/AMD64. Go cache and preview data were placed outside the repository; no application runtime dependency was added.

## Does the problem still exist?

Field log collection is a plausible problem, but the market does not establish LogFalcon's differentiation. Official documentation checked on 2 October confirms native Android USB connectivity in the Betaflight App and mobile blackbox download/analysis in SpeedyBee Adapter 3. A laptop-free workflow is therefore not unique.

LogFalcon's proposed advantage is unattended collection: power it once, connect an FC, archive the data consistently, then retrieve it later. Whether this is preferable to a phone-led workflow requires observation of other pilots using it.

Start with a reliable Betaflight transfer and seek external feedback. Additional analytics, cloud services and custom power accessories are not prerequisites for testing that proposition.

## Current project state

The engineering foundation is substantial:

- Go MSP implementation and framing
- Raspberry Pi OS image construction
- udev and systemd integration
- web interface and Wi-Fi access point
- storage management and manifests
- ARMv6, ARM64 and AMD64 builds
- CI, release and security workflows

It is an advanced prototype, not vapourware. It is not yet a dependable appliance.

Historical observations from 27 July 2026; release status, adoption counts and GitHub metadata were not freshly audited on 2 October:

- v0.4.6 is the latest GitHub release, published on 11 March 2026.
- `v0.4.7-rc1` is tagged but has no completed GitHub release; its release workflow failed while processing the image.
- The public repository has no meaningful adoption evidence: no stars, forks, user issues or v0.4.6 asset downloads.
- The repository has no description, homepage or topics.
- GitHub Discussions are disabled although the documentation links to them.
- Releases do not contain the promised `checksums.txt`.
- `CHANGELOG.md` dates releases from November 2024 onwards, but the repository was created in February 2026 and the corresponding Git tags were produced in March 2026. The history must be corrected rather than presented as elapsed product maturity.
- `PROJECT_STATUS.md` contains conflicting current versions and self-reported progress metrics. Replace it with factual compatibility, limitations and roadmap documents.

## Release-blocking findings

### LF-001: flash sizes above 65,535 bytes are mishandled

`internal/sync/orchestrator.go` converts the remaining flash size to `uint16` before comparing it with the chunk size. The conversion wraps common flash sizes.

For a 1 MiB used size, the first request becomes zero bytes. Other values above 64 KiB can retrieve only the modulo-65,536 remainder before final size verification fails. A focused MSP simulation reproduced the zero-byte first request. The defect is present in v0.4.6.

The size check prevents erasure after this failure, so the behaviour is relatively safe, but the advertised principal function does not work for ordinary logs.

The July test suite passed with the race detector, but coverage was:

- 47.0% overall
- 2.8% for `internal/sync`
- 0% for the actual orchestration, flash-read and verification paths

Test count is therefore not evidence that the product workflow works.

### LF-002: the dashboard cannot receive live sync status

`logfalcon-web.service` and `logfalcon@.service` run separate processes. Synchronisation status is stored in a package-level in-memory variable, while the web process reads its own unrelated copy.

The sync process's progress, FC identity and error state cannot reach the web process or its health endpoint. On 2 October, the dashboard was changed to state this limitation and stop presenting misleading live progress. This is a UI mitigation, not a backend fix.

Implement cross-process status and test it using distinct processes. Until then, the web `/health` response does not establish transfer success, and idle auto-shutdown must remain disabled during evaluation because it cannot observe sync activity.

### LF-003: current iNav support is invalid

LogFalcon interprets dataflash flags using Betaflight semantics. Current iNav serialises its ready/supported state differently, causing a ready flash to appear busy. The claimed compatibility also stops several major releases behind current iNav.

Remove iNav from the first beta. Restore it only after firmware-specific parsing and physical tests on current iNav hardware.

### LF-004: serial deadlines are not reliable

The serial library defaults to blocking reads and LogFalcon does not set an OS-level read timeout. The application's time-based deadline loop cannot interrupt a blocked `Read`. The systemd ten-minute limit is an external last resort, not a sound protocol timeout.

Set a real read timeout, handle short writes, and test stalls and disconnects.

### LF-005: configuration can fail open

Malformed configuration can be skipped and replaced with defaults. The current defaults enable erasure after sync and automatic cleanup under storage pressure. A typo can therefore re-enable destructive behaviour.

For the beta:

- invalid configuration must stop synchronisation;
- `erase_after_sync` must default to `false`;
- automatic cleanup must default to `false`;
- configuration values must have explicit range validation.

### LF-006: the SHA-256 claim is overstated

The SHA-256 operation compares the in-memory received data with the saved file re-read from storage. It proves that the saved bytes can be read back unchanged; it is not an independent hash supplied by the FC.

The README and guide now describe this as saved-copy consistency. That copy correction is implemented locally; independent source/BBL validation is not. Retain the check and evaluate structural or current-decoder validation before enabling automatic erasure.

### LF-007: LED error reporting is unreliable

LED sysfs write failures are ignored. The sync runs as an unprivileged user whose actual sysfs permissions have not been demonstrated. The process exits immediately after setting success or error, and the service post-stop action restores the solid ready light even after failure.

Validate LED access on both supported Pi models and ensure that success and error remain visible until acknowledged or the FC is disconnected.

### LF-008: panic recovery can report success

`Orchestrator.Run` recovers from panic without a named result. A recovered panic can therefore return the zero-value success result even though the status side channel says error.

A temporary simulation on 2 October reproduced this: a panic in `Run` was recovered and returned `ResultSuccess` (zero), rather than `ResultError`. Return an explicit error result after recovery and add a permanent regression test. The temporary probe is evidence of the defect, not a completed fix.

### LF-009: the appliance is not hardened for field power loss

The documentation tells the pilot to unplug the appliance, but the image uses a normally writable Raspberry Pi OS root filesystem. There is no read-only root, separate data partition, power-loss transaction design or physical safe-shutdown control.

Abrupt loss during filesystem writes can corrupt the SD card or a saved log. This becomes more likely if the appliance is powered directly from a removable flight battery.

### LF-010: Betaflight blackbox configuration reads the support byte as the device

`internal/msp/client.go:GetBlackboxConfig` returns `f.Payload[0]`. Betaflight's `MSP_BLACKBOX_CONFIG` response places the support flag first and the configured device second. A supported controller therefore appears to use flash regardless of its actual device selection.

Today's simulated response `[1, 2, 1, 1, 0, 0, 0]` (supported, SD card, remaining configuration) returned device 1 rather than 2. This bypasses the intended SD-card rejection and can produce incorrect session metadata or operate on unrelated onboard flash. Verified against the Betaflight 4.5.2 source, not physical hardware.

Validate the payload length and support flag, parse the device from the correct byte, and reject unsupported/unknown device configurations before transfer. Add wire-level tests for flash, SD card, disabled logging and malformed replies. Detector mocks alone do not exercise this parser.

## Power and boot-time assessment

**October priority clarification:** the following July power-design notes are proposals, not a selected or validated hardware design. Begin evaluation with a suitable 5 V USB supply and keep it powered through the session. Custom LiPo accessories, FC-powered operation and platform replacement are deferred until basic reliability and user demand are established. Hardware-specific electrical claims below were not revalidated during today's UI/code review.

### Direct connection to a flight LiPo

Never connect a 2S-6S LiPo directly to the Pi. Raspberry Pi Zero boards require regulated 5 V.

The recommended beta power path is:

`2S-6S LiPo -> protected buck converter -> regulated 5.1 V / 3 A -> Pi PWR input`

The design should provide:

- at least 2 A continuous output, with 3 A design headroom;
- reverse-polarity protection;
- input fuse or resettable PTC;
- transient suppression;
- current limiting and thermal protection;
- a power-good indication;
- an enclosure and strain-relieved XT30/XT60 lead.

This should be a documented reference build or small LogFalcon power accessory. The safest field workflow is to power LogFalcon from a spare or disconnected flight pack, not from an armed-capable aircraft.

### Powering from the flight controller

Do not make this the default or claim universal support.

The Pi is the USB host and normally supplies USB VBUS to the FC. Attempting to power the host back through the same USB connection is non-standard and risks back-powering. Powering the Pi separately from an FC 5 V pad requires a custom soldered lead and a LiPo connected to the aircraft.

Betaflight's FC design guidance recommends only 1-2 A for the complete battery-powered 5 V peripheral rail. A Zero 2 W is specified around a 2 A supply and must also power the connected FC over USB. Existing receivers, GPS, LEDs and cameras may already consume much of the rail. FC designs also differ in rail topology and USB/LiPo isolation.

An FC-powered mode could be documented later for individually verified boards with measured spare capacity and a protected, non-backfeeding interface. It is unsuitable as the general product workflow.

### Recommended board support

Make Raspberry Pi Zero 2 W the sole supported target for the first beta. Treat the original Zero W as community-supported until cold-boot and transfer measurements justify maintaining it.

This reduces:

- cold-start delay;
- ARMv6 release and test work;
- performance variability;
- the temptation to publish unsupported timing claims.

### Boot time

The July README gave conflicting approximately 30-second workflow and approximately 90-second boot guidance. Those promises have now been removed from the README, website and guide. No repeatable physical timing measurements were established today; measure the exact image before publishing timings.

The current ready LED is coupled to `logfalcon-web.service`, which waits for hostapd. Wi-Fi and the web interface are not prerequisites for copying a log, so the appliance may be technically sync-capable before it declares itself ready.

Short-term changes:

1. Measure three cold boots on each board from power applied to:
   - local filesystems ready;
   - sync service ready;
   - FC detected;
   - first MSP response;
   - Wi-Fi/dashboard ready.
2. Remove networking dependencies from the sync path.
3. Add a separate `logfalcon-sync-ready.target` and drive the ready LED from it.
4. Start Wi-Fi, DNS, mDNS and the dashboard in parallel or on demand.
5. Disable SSH by default.
6. Record boot timing in diagnostics and the hardware matrix.

Do not rewrite the image around Buildroot or another distribution until measurements show that service-level optimisation is insufficient. A reasonable product gate is under 20 seconds from power-on to sync-ready on Zero 2 W; this is a target, not a current claim.

If that target cannot be reached and the device must cold-boot for every transfer, reassess the Pi platform. An ESP32-S3 plus microSD design would boot faster and use less power, but it would be a substantial rewrite and would overlap with Betaflight Bridge. The Pi remains reasonable if it is powered once at the beginning of a session and survives safe field power cycling.

## Proposed first-beta scope

This remains a recommendation, not a completed support decision. Both Pi build targets and iNav code paths remain in the checkout; today's copy labels them appropriately.

Ship:

- Raspberry Pi Zero 2 W only
- Betaflight only
- SPI flash only
- prebuilt image only
- manual opt-in erasure
- documented, suitable 5 V USB supply; custom LiPo power design deferred
- local Wi-Fi log browser
- explicit compatibility and limitations matrix

Do not ship or claim:

- iNav support
- original Pi Zero W performance guarantees
- universal FC-powered operation
- automatic erasure or cleanup by default
- “only laptop-free option”, “expensive dongle”, “vast majority” or “already in most pilots' kit”
- unmeasured transfer or boot times
- CP2102/CH340 compatibility without physical tests
- the invasive one-line installer as the primary path

## Product improvements after external feedback

Choose additions based on observed friction: bulk export if pilots struggle to retrieve collections; aircraft aliases if controller IDs confuse multi-aircraft users; enclosure or power changes if handling is the main barrier. These are options, not a commitment to build all three.

Avoid additional features until the basic workflow is reliable and other pilots choose to use it again.

## Publication and portfolio threshold

The July suggested thresholds below remain proposed beta acceptance criteria, not achieved results or guarantees of demand. Before a broad FPV announcement:

- all release-blocking issues above closed;
- green `v0.5.0-beta.1` image build with real checksums;
- physical matrix covering at least two FCs, current Betaflight and every older version claimed;
- power interruption tested before, during and after transfer;
- every retained BBL opens in the current Blackbox Explorer;
- three external beta pilots;
- approximately 20-50 successful real transfers with failures recorded rather than discarded.

Until then, describe LogFalcon as an experimental appliance or advanced prototype. For Go/Linux/platform interviews it already offers concrete discussion of protocol handling, streaming storage, system integration and release packaging. Pair it with Eugene's larger delivery/leadership experience rather than treating project polish as a reason to delay applications.

The strongest next portfolio evidence is one reliable end-to-end physical demonstration, then measured compatibility and failure results. Be able to explain the implementation and trade-offs personally, including how AI-generated work was reviewed. Today's existing tests missed central workflow defects; closing that gap is a useful engineering story once actually completed.

The current career CV's claim of safe Betaflight and iNav field retrieval should be qualified until the relevant fixes and hardware validation are recorded. No career document was edited in this session.

Current defensible framing:

> Built LogFalcon, an open-source Go and Raspberry Pi appliance for collecting drone blackbox logs over USB, with controller/session storage, manifests and a local browser interface. The current focus is transfer reliability and validation before destructive operations.

After validation it can support a stronger claim covering dependable storage and physical compatibility.

Suggested eventual wording:

> Built and released LogFalcon, a Go and Raspberry Pi appliance that automatically archives Betaflight blackbox logs over MSP in the field. Implemented protocol framing, fail-safe copy, validation and erase handling, embedded Linux packaging and multi-architecture CI; validated across X flight controllers, Y firmware versions and Z field transfers.

Replace X, Y and Z only with recorded evidence.

## Immediate recommendation — 2 October

Fix LF-001 and LF-010 first, add permanent tests that exercise the actual transfer/parser paths, then address fail-open configuration, serial timeouts, panic outcomes and cross-process status. Prove one Betaflight copy produces a usable saved log before expanding the product or investing in another design cycle. After that proof and basic safety checks, obtain external pilot feedback.

## External references

The Betaflight App, SpeedyBee product page and firmware source links below were checked during today's review. Other links are retained from July as background, not evidence of freshly verified behaviour.

- Betaflight blackbox logging: <https://betaflight.com/docs/wiki/guides/current/Black-Box-logging-and-usage>
- Betaflight App: <https://betaflight.com/docs/wiki/app>
- Betaflight 2026.6 release notes: <https://betaflight.com/docs/wiki/release/Betaflight-2026-6-Release-Notes>
- Betaflight Bridge: <https://github.com/betaflight/bridge>
- Betaflight FC manufacturer power guidance: <https://betaflight.com/docs/development/manufacturer/manufacturer-design-guidelines>
- Betaflight mass-storage support: <https://betaflight.com/docs/wiki/guides/current/Mass-Storage-Device-Support>
- Raspberry Pi hardware and power documentation: <https://www.raspberrypi.com/documentation/computers/raspberry-pi.html>
- SpeedyBee Adapter 3: <https://www.speedybee.com/speedybee-adapter-3/>
- Current iNav releases: <https://github.com/iNavFlight/inav/releases>

- Betaflight 4.5.2 blackbox response layout: <https://github.com/betaflight/betaflight/blob/4.5.2/src/main/msp/msp.c#L1624>
- Current iNav flash-summary encoding: <https://github.com/iNavFlight/inav/blob/master/src/main/fc/fc_msp.c#L326>
