# Security

Security fixes target `main`. Older versions may lack fixes for controller
erasure or file access. See the [changelog](CHANGELOG.md) for details.

Report a security issue privately to **eprokudin@icloud.com**. Include the
affected version or commit, the impact and steps to reproduce it. Keep
credentials, private flight logs and details of an exploit out of public issues.

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
installer rejects missing/mismatched assets before installation. Choose a specific
release and inspect its source/provenance before running installer scripts.
