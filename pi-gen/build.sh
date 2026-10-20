#!/usr/bin/env bash
set -euo pipefail

# Build the LogFalcon SD card image using pi-gen
# Requirements: Docker

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "=== Building LogFalcon SD card image ==="
echo "This will take 30-60 minutes on first run."
echo ""

# Clone pi-gen if not present
if [[ ! -d "$SCRIPT_DIR/pi-gen-repo" ]]; then
    echo "Cloning pi-gen..."
    git init "$SCRIPT_DIR/pi-gen-repo"
    git -C "$SCRIPT_DIR/pi-gen-repo" remote add origin https://github.com/RPi-Distro/pi-gen.git
    git -C "$SCRIPT_DIR/pi-gen-repo" fetch --depth 1 origin 1c2abf50924d5bfee3527657af74ddfb1da52904
    git -C "$SCRIPT_DIR/pi-gen-repo" checkout --detach FETCH_HEAD
fi

[[ "$(git -C "$SCRIPT_DIR/pi-gen-repo" rev-parse HEAD)" == 1c2abf50924d5bfee3527657af74ddfb1da52904 ]] || {
    echo "Existing pi-gen checkout has a different commit; use a fresh build directory." >&2; exit 1;
}

# Cross-compile for ARM before copying into pi-gen
echo "Cross-compiling logfalcon for ARM..."
cd "$REPO_ROOT"
make build-pi
make build-pi2
cp bin/logfalcon-arm6 bin/logfalcon
ls -la bin/

# Copy our config and stage into pi-gen
cp "$SCRIPT_DIR/config" "$SCRIPT_DIR/pi-gen-repo/config"
rm -rf "$SCRIPT_DIR/pi-gen-repo/stage-logfalcon"
cp -r "$SCRIPT_DIR/stage-logfalcon" "$SCRIPT_DIR/pi-gen-repo/stage-logfalcon"

# Copy project source into pi-gen so it's accessible inside the Docker build
rm -rf "$SCRIPT_DIR/pi-gen-repo/logfalcon-src"
rsync -a --exclude='.git' --exclude='pi-gen' \
    "$REPO_ROOT/" "$SCRIPT_DIR/pi-gen-repo/logfalcon-src/"

# Skip stages 3, 4, 5 (safety net — STAGE_LIST already limits to 0-2 + logfalcon)
touch "$SCRIPT_DIR/pi-gen-repo/stage3/SKIP" "$SCRIPT_DIR/pi-gen-repo/stage3/SKIP_IMAGES"
touch "$SCRIPT_DIR/pi-gen-repo/stage4/SKIP" "$SCRIPT_DIR/pi-gen-repo/stage4/SKIP_IMAGES"
touch "$SCRIPT_DIR/pi-gen-repo/stage5/SKIP" "$SCRIPT_DIR/pi-gen-repo/stage5/SKIP_IMAGES"

# Build
cd "$SCRIPT_DIR/pi-gen-repo"
./build-docker.sh

echo ""
echo "=== Build complete ==="
echo "Image: $SCRIPT_DIR/pi-gen-repo/deploy/"
ls -lh "$SCRIPT_DIR/pi-gen-repo/deploy/"*.img* 2>/dev/null || echo "(check deploy/ directory)"
