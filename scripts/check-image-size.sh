#!/usr/bin/env bash
# Separate flash-image and download budgets. The pinned pi-gen reserves 512 MiB
# for boot alone, so the 600 MiB download budget cannot apply to the raw image.
set -euo pipefail

phase=${1:-}
image=${2:-}
case "$phase" in
    raw) limit_mib=3072 ;;
    compressed) limit_mib=600 ;;
    *) echo 'Usage: check-image-size.sh raw|compressed IMAGE' >&2; exit 2 ;;
esac
[[ -f "$image" ]] || { echo "Image not found: $image" >&2; exit 1; }

# GNU stat in Linux CI, BSD stat for local fixture checks on macOS.
bytes=$(stat -c%s "$image" 2>/dev/null || stat -f%z "$image")
limit_bytes=$((limit_mib * 1024 * 1024))
echo "$phase image: $bytes bytes (limit: $limit_mib MiB / $limit_bytes bytes)"
if ((bytes > limit_bytes)); then
    echo "::error::$phase image exceeds $limit_mib MiB; inspect image contents before changing the budget." >&2
    exit 1
fi
