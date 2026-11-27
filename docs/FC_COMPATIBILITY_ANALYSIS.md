# Controller compatibility and evidence

| Hardware or firmware | What LogFalcon does |
|---|---|
| Betaflight with internal SPI flash | Checks the storage device and flash usage before copying. Blocks MSP API below 1.41 and warns above 1.48. |
| iNav | Stops before reading flash. |
| Controller-side SD card or ArduPilot | Copying is unsupported. |
| Pi Zero W and Zero 2 W | Both use the 32-bit ARM6 image. A separate ARM64 binary is available for 64-bit installations on a Zero 2 W. |

The [detector](../internal/fc/detector.go), [version checks](../internal/fc/versions.go)
and [protocol fixtures](../internal/msp/testdata/README.md) describe the wire formats the code handles.
For results on individual boards, see the [field test record](../DEVELOPMENT_TESTING.md).
The [roadmap](https://proeugene.github.io/logfalcon/roadmap.html) lists the next hardware checks.
The [Python compatibility analysis](archive/FC_COMPATIBILITY_ANALYSIS_PYTHON.md) covers an earlier implementation.

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
