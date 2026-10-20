# LogFalcon candidate status

Updated 20 October 2026. Local source candidate based on
`91847b19b5ba4427d13b490de121622f9c61f768` plus uncommitted changes. Not published.

The product is focused on collecting Betaflight SPI-flash dumps between flights,
with phone downloads over a local Pi hotspot. iNav copying is disabled; FC-side
SD cards and ArduPilot are unsupported. Pi Zero 2 W is the first physical test
target; Zero W remains a build target awaiting separate acceptance.

Implemented: non-destructive defaults, strict config, bounded MSP reads, unique
partial files, saved-copy consistency checking and durable publication before
optional erase, transfer locking, shared status, dedicated LED ownership,
bounded diagnostics, rooted downloads/deletion/publication, standard ranges,
SSE deadlines, recoverable settings transactions and upgrade preservation.
Compression has been removed. UK and debug defaults remain enabled for new
installations; existing installations retain their settings.

Automated evidence and skipped checks are recorded in
[DEVELOPMENT_TESTING.md](DEVELOPMENT_TESTING.md). There is no measured boot/transfer
speed, physical compatibility or adoption claim for this candidate. A fake MSP
peer and synthetic upstream-layout fixtures do not substitute for a real FC.

[Architecture](docs/ARCHITECTURE.md) · [Compatibility](docs/FC_COMPATIBILITY_ANALYSIS.md)
· [AI assistance](docs/AI_ASSISTED_DEVELOPMENT.md) · [Release gates](RELEASE_CHECKLIST.md)

Next: review the diff, build and inspect the exact SD image on Linux, then perform
physical acceptance with erasure/cleanup off. Record a real log opening in
Blackbox Explorer, failures and timings. After that, obtain one independent
pilot's feedback before publication. Licence-holder confirmation and repository
protection/security settings remain maintainer actions; see the testing record.
