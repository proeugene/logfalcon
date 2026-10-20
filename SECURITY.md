# Security

LogFalcon is experimental. Fixes target the current candidate/main branch; no
older release has a security-maintenance guarantee or current physical sign-off.
Older binaries/images can lack important safety and containment fixes. Check
[the candidate record](PROJECT_STATUS.md) and [changelog](CHANGELOG.md).

For a sensitive report, email **eprokudin@icloud.com**, the maintainer address
recorded in the repository's current commits. Include affected version/commit,
impact and a minimal reproduction without live credentials or personal logs.
Do not post exploit details or secrets in a public issue. Private vulnerability
reporting was **disabled** when inspected on 20 October 2026; the repository does
not yet offer that route. No response deadline is guaranteed by this small
maintainer-run project.

The hotspot dashboard uses HTTP without login/TLS. Every connected client is
trusted to access/delete saved logs and change Wi-Fi settings. CSRF protection
is not authentication. Change both the default hotspot password and the image's
initial administration password before shared use. Do not expose the dashboard
outside a trusted local network. Local administrators/SD-card writers remain
trusted. See [architecture and privileges](docs/ARCHITECTURE.md).

Debug defaults on, but records command metadata rather than raw MSP payloads or
passwords. Exports are bounded and redacted; review them for IDs, IP/MAC addresses
and activity times before sharing. Redaction is not a guarantee of anonymity.

Release checksums verify download integrity relative to the GitHub-hosted checksum
file; they are not signatures or independent publisher authentication. The remote
installer rejects missing/mismatched assets before installation. Prefer a pinned
candidate and inspect its source/provenance before running installer scripts.
