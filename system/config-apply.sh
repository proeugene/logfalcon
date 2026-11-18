#!/usr/bin/env bash
# Shared, recoverable Go transaction; no shell evaluation of boot credentials.
set -euo pipefail
exec /opt/logfalcon/logfalcon --apply-boot-config
