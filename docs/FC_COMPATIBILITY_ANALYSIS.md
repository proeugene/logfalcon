# Controller compatibility and evidence

| Target | Current behaviour | Evidence still needed |
|---|---|---|
| Betaflight, internal SPI flash | Intended copy target; storage device/summary checks | Exact Pi/board/firmware dump, reconnect and failure tests |
| iNav | Detected and explicitly rejected before copying | Correct firmware-specific protocol and real hardware acceptance |
| FC-side SD cards | Rejected | Outside current scope |
| ArduPilot / other firmware | Unsupported | Outside current scope |
| Pi Zero 2 W | Primary evaluation target; ARM64 build | Exact image, Wi-Fi/USB/LED/power acceptance |
| Pi Zero W | ARM6 build target | Separate physical validation; no performance claim |

See [version checks](../internal/fc/versions.go), [detection](../internal/fc/detector.go)
and [fixed synthetic protocol fixtures](../internal/msp/testdata/README.md).
Version ranges in code are protocol assumptions, not a list of tested boards.
A warning on newer Betaflight firmware does not establish compatibility.

Record every physical result in [DEVELOPMENT_TESTING.md](../DEVELOPMENT_TESTING.md),
including exact commit/image checksum and failures. No confirmed hardware matrix
has been produced for this local candidate. The [older Python analysis](archive/FC_COMPATIBILITY_ANALYSIS_PYTHON.md)
is historical and must not be used as current support evidence.
