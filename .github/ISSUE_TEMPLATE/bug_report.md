---
name: Bug Report
about: Something isn't working
labels: bug
---

## What happened?

<!-- Describe what went wrong. What did you expect to happen? -->

## Steps to reproduce

1.
2.
3.

## FC & firmware

| Field | Value |
|-------|-------|
| FC board | e.g. Betaflight F7, Matek F405 |
| Firmware | e.g. Betaflight 4.5.0 |
| LogFalcon version | version + full commit |
| Pi hardware | Pi Zero W / Pi Zero 2 W |

## LED behaviour at the time of failure

<!-- What was the LED doing? Solid, blinking, off? -->

## Logs

<!-- If you can SSH in or connect a monitor: `sudo journalctl -u 'logfalcon*' -n 100` -->

```
paste logs here
```

## Reproducible evidence

- Image filename and SHA-256:
- Pi model, power supply and USB cable/adapter:
- FC board, exact firmware and SPI-flash log size:
- Erase/cleanup enabled? (keep both off during evaluation):
- Observed steps/outcome and measured timing:
- Copy transfer ID and LED/dashboard agreement:
- Download opens in Blackbox Explorer?:
- Hardware tested / not tested:

Attach only reviewed, redacted diagnostic excerpts. Remove passwords, full config,
private logs and identifying details you do not intend to publish. For sensitive
reports use SECURITY.md. Synthetic/mock results do not confirm compatibility.
