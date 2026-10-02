# LogFalcon v0.5 beta release plan

**Created:** 27 July 2026  
**Updated:** 2 October 2026  
**Target:** `v0.5.0-beta.1`  
**Product scope:** Raspberry Pi Zero 2 W, Betaflight, SPI flash, prebuilt image.

The findings and rationale behind this plan are recorded in `LOGFALCON_REVIEW_2026-07-27.md`.

The repository remains pre-beta. The transfer core and cross-process status are not fixed. The website, guide, README and saved-log dashboard were simplified on 2 October and now describe those limitations explicitly.

## Release goal

Produce a field-testable appliance that can copy a Betaflight SPI-flash blackbox dump, persist and validate it, and leave the FC untouched whenever complete success cannot be proved.

The beta is not a claim of universal FC support. It is an instrumented release intended to establish real compatibility and demand.

## Non-negotiable invariants

1. Never erase unless an exact-length transfer, durable save and validation have all succeeded.
2. Loss of USB, serial response, storage or power must not turn into reported success.
3. Invalid configuration must prevent destructive actions.
4. The LED and dashboard must agree with the actual transfer outcome.
5. Compatibility and performance claims must come from recorded tests.

## Phase 0 — product and power decision

**Priority:** immediate  
**Estimate:** 1-2 days plus acquisition of any missing hardware

- [ ] Select Raspberry Pi Zero 2 W as the supported beta board.
- [ ] Decide whether Zero W remains build-only or is removed from beta artefacts.
- [ ] Measure at least three cold boots of the current image.
- [ ] Record time from power applied to sync-ready, FC detection, first MSP response and Wi-Fi ready.
- [ ] Measure idle, boot and active-transfer current with a representative FC attached.
- [ ] Prototype a protected 2S-6S to 5.1 V / 3 A power lead.
- [ ] Confirm that the power path does not back-power the Pi or FC.
- [ ] Decide the safe shutdown interaction: physical button, timed shutdown, or both.
- [ ] Document that FC-rail power is unsupported except for specifically validated boards.

**Exit gate**

- A written power architecture and wiring diagram exists.
- Zero 2 W reaches sync-ready in less than 20 seconds, or a documented decision is made to accept session-long power or investigate a different platform.
- The selected supply completes repeated transfers without reset or voltage instability.

## Phase 1 — repair the transfer core

**Priority:** P0  
**Estimate:** 2-3 days

### Flash transfer

- [ ] Fix the narrowing conversion in `internal/sync/orchestrator.go`.
- [ ] Keep arithmetic in a type capable of representing the full flash size.
- [ ] Reject impossible sizes and offset overflow.
- [ ] Handle zero-byte and exact-multiple-of-65,536 cases explicitly.
- [ ] Check serial short writes.
- [ ] Configure an OS-level serial read timeout.
- [ ] Make recovered panics return `ResultError`.
- [ ] Correct `MSP_BLACKBOX_CONFIG` parsing: the support flag is byte 0 and the configured device is byte 1.

### Protocol test harness

- [ ] Add a deterministic fake MSP serial peer.
- [ ] Exercise the real orchestration path rather than status helpers alone.
- [ ] Test 1 MiB and 16 MiB transfers.
- [ ] Test a non-64-KiB-aligned used size.
- [ ] Test empty flash.
- [ ] Test partial responses and short writes.
- [ ] Test CRC failure and malformed frames.
- [ ] Test a stalled FC.
- [ ] Test USB disconnect at each phase.
- [ ] Test storage-full and write errors.
- [ ] Assert that every failure path sends no erase command.
- [ ] Add permanent wire-level tests for flash, SD-card, unsupported and malformed blackbox configuration replies.

**Exit gate**

- All tests pass with the race detector.
- Core `Run`, read, save, validate and erase paths have meaningful branch coverage.
- The large-flash regression test fails on v0.4.6 and passes on the new code.

## Phase 2 — make status and safety truthful

**Priority:** P0  
**Estimate:** 2-3 days

### Shared state

- [ ] Replace package-local status with an atomically written `/run/logfalcon/status.json`, Unix socket, or single daemon.
- [ ] Include transfer ID, FC identity, phase, byte counts, result and timestamp.
- [ ] Make the web dashboard consume that shared state.
- [x] Remove misleading live progress/completion claims from the dashboard until shared state exists.
- [ ] Make health distinguish web health, appliance readiness and last-sync result.
- [ ] Add a singleton lock for transfers.

### Configuration

- [ ] Fail startup on malformed or invalid configuration.
- [ ] Default `erase_after_sync` to `false`.
- [ ] Default storage-pressure cleanup to `false`.
- [ ] Validate baud, chunk size, timeouts, storage thresholds and Wi-Fi region.
- [ ] Require an explicit first-run opt-in before automatic erasure.

### Validation

- [x] Rename the existing SHA-256 step in public documentation to describe saved-copy consistency rather than independent source verification.
- [ ] Flush and sync the saved file and parent directory before validation success.
- [ ] Add BBL structural validation or current-decoder validation before erasure.
- [ ] Store interrupted transfers with a `.partial` suffix.
- [ ] Atomically rename only complete transfers.

**Exit gate**

- The dashboard and LED show the same result as the sync process.
- Invalid configuration cannot enable erasure or cleanup.
- Every destructive action has a tested, explicit prerequisite.

## Phase 3 — harden the field appliance

**Priority:** P0 for publication  
**Estimate:** 2-4 days

### Power-loss tolerance

- [ ] Make the operating-system root read-only or use an overlay.
- [ ] Put logs and required mutable state on a separate writable partition.
- [ ] Make manifests and status files atomic.
- [ ] Recover or clearly quarantine interrupted transfers on the next boot.
- [ ] Add a physical safe-shutdown button and unambiguous “safe to remove power” state.
- [ ] Run at least 20 controlled power cuts across boot, idle, copy, validation and post-copy states.

### Boot path

- [ ] Remove networking dependencies from the sync service.
- [ ] Add `logfalcon-sync-ready.target`.
- [ ] Drive the ready LED from sync readiness rather than web readiness.
- [ ] Start Wi-Fi and the dashboard in parallel or on demand.
- [ ] Disable SSH by default.
- [ ] Remove default SSH credentials.
- [ ] Make the Wi-Fi regulatory country configurable.

### LED and services

- [ ] Provide and test actual LED sysfs permissions for the service account.
- [ ] Treat LED write errors as diagnostics rather than ignoring them.
- [ ] Keep the error state visible after service exit.
- [ ] Do not restore the ordinary ready light after a failed transfer until acknowledged or disconnected.
- [ ] Verify LED paths on Zero 2 W hardware.

**Exit gate**

- The SD image remains bootable and previous complete logs remain intact after the power-cut test.
- Sync can start without waiting for Wi-Fi.
- There are no shared default administrative credentials.

## Phase 4 — physical compatibility matrix

**Priority:** P0 for publication  
**Estimate:** 2-5 days, depending on available hardware

- [ ] Test at least two FC models from different manufacturers.
- [ ] Test current Betaflight 2026.6.
- [ ] Test every older Betaflight branch retained in the compatibility claim.
- [ ] Test at least two flash capacities.
- [ ] Test native USB VCP.
- [ ] Test every USB-UART bridge type that remains advertised.
- [ ] Test an already-empty flash.
- [ ] Test a nearly full flash.
- [ ] Test multiple logs in one flash.
- [ ] Open every produced file in current Blackbox Explorer.
- [ ] Record board, firmware, API version, flash device, used size, elapsed time and result.
- [ ] Create `HARDWARE_MATRIX.md`.
- [ ] Create `KNOWN_LIMITATIONS.md`.

**Exit gate**

- Every public compatibility claim maps to a recorded passing result.
- Unverified devices and firmware are labelled unknown, not supported.

## Phase 5 — release and documentation

**Priority:** P1  
**Estimate:** 1-2 days

- [ ] Remove iNav from code paths, README, website and release claims for this beta.
- [x] Remove unmeasured boot and transfer timings from the README, website and guide.
- [ ] Remove unsupported “only option”, “vast majority” and price claims.
- [ ] Correct `CHANGELOG.md` to the actual development dates.
- [ ] Replace `PROJECT_STATUS.md` with the release plan, roadmap and limitations documents.
- [ ] Make the prebuilt image the primary and initially recommended installation route.
- [ ] Remove or clearly de-emphasise the invasive `curl | sudo bash` installer.
- [ ] If the installer remains, verify release checksums and bring its udev behaviour into parity with the image.
- [ ] Add a GitHub description, homepage and topics.
- [ ] Enable Discussions or remove all Discussions links.
- [ ] Remove Ko-fi until there are users.
- [ ] Produce `checksums.txt` and verify it in the release workflow.
- [ ] Complete an image smoke test from the exact release artefact.
- [ ] Record a 60-90 second unedited field demonstration.
- [ ] Publish `v0.5.0-beta.1` with explicit experimental wording.

### Documentation work completed locally on 2 October

- [x] Reduce the README to a short product and evaluation entry point.
- [x] Simplify the public website to product, workflow, compatibility and setup.
- [x] Rewrite the guide around evaluation, limitations and troubleshooting.
- [x] Make saved-log downloads prominent and put metadata/actions under Details.
- [x] Label unconfirmed erase outcomes and hide download actions for missing log files.

These edits are local and uncommitted as a release. They do not satisfy the transfer, hardware or beta acceptance gates.

**Exit gate**

- The release pipeline is green from tag to downloadable image.
- The downloaded image checksum matches and a fresh flash passes the smoke test.
- README claims match the hardware matrix and known limitations.

## Phase 6 — external beta

**Priority:** after the release candidate passes internal gates

- [ ] Recruit three pilots outside the project.
- [ ] Give each pilot a short test script and failure-report form.
- [ ] Record successful and failed transfers, hardware and firmware.
- [ ] Reach approximately 20-50 successful field transfers.
- [ ] Resolve any data-loss, false-success, power or repeated-compatibility failure before wider promotion.
- [ ] Decide whether evidence supports a broader FPV community announcement.

## Later product improvements

Do these only after the external beta establishes that the basic workflow is wanted:

- [ ] A small protected LogFalcon LiPo power board and enclosure.
- [ ] Individual-flight indexing inside a flash dump.
- [ ] Aircraft aliases, duplicate detection and fleet/session views.
- [ ] Bulk session export for Blackbox Explorer.
- [ ] Optional home-network upload or backup.
- [ ] Re-evaluate iNav using current protocol semantics and real hardware.
- [ ] Investigate ESP32-S3 plus microSD only if Pi power or cold-start remains unacceptable.

## Immediate next actions

1. Do not announce or recommend v0.4.6.
2. Obtain one Zero 2 W, a USB power meter or oscilloscope, two representative FCs and a protected adjustable buck supply.
3. Measure the existing image before optimising it.
4. Decide and document the beta power architecture.
5. Fix LF-001 and land the end-to-end MSP test harness.
6. Fix LF-010 and add permanent parser tests.
7. Make erasure and cleanup opt-in and configuration fail closed.
8. Implement cross-process status.
9. Build the power-loss-safe storage layout.
10. Run the physical matrix and correct all documentation from its evidence.
11. Cut `v0.5.0-beta.1`, then recruit external beta pilots.

## Effort and uncertainty

Expected effort is approximately 10-20 focused working days, rather than the earlier 7-14-day software estimate, because power design, cold-boot measurement and power-loss hardening are now explicit release work.

The largest uncertainties are:

- access to representative FC and flash hardware;
- actual cold-start performance of the built image;
- safe behaviour of USB VBUS across different FC power topologies;
- whether pilots value unattended archival enough to carry a dedicated appliance.
