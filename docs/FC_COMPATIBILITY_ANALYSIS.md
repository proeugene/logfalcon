# Controller compatibility and evidence

| Target | Current behaviour | Evidence still needed |
|---|---|---|
| Betaflight, internal SPI flash | Intended copy target; storage device/summary checks | Exact Pi/board/firmware dump, reconnect and failure tests |
| iNav — Experimental; copying currently disabled | Rejected before copying | Correct firmware-specific protocol and real hardware acceptance |
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

## Tagged protocol review, 21 October 2026

The latest stable release listed upstream at implementation time was Betaflight
[2026.6.2](https://github.com/betaflight/betaflight/releases/tag/2026.6.2), commit
e0b7bb01b17b21351057e9ead2d1ab39dd44fa16. Tagged msp_protocol.h identifies API
1.48. Tagged msp.c retains the SPI blackbox device, flash summary and plain-read
layouts used by LogFalcon. Modern FC_VERSION replies append a length-prefixed
firmware string; LogFalcon now checks its bounds and uses it, retaining older
three-byte replies and calendar-year decoding.

The protocol-reviewed Betaflight range is API 1.41–1.48. This is source/fixture
evidence, not physical firmware validation. The independently authored bf2026
fixtures have their exact provenance in internal/msp/testdata/README.md. No new
board or firmware has been physically accepted by this change.
