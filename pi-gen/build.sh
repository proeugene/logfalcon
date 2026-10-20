#!/usr/bin/env bash
# One-command image build on macOS (including Apple Silicon) or Linux.
# Only Docker is required on the host. Linux tools, Go and ARM emulation run inside it.
set -euo pipefail

PIGEN_COMMIT=1c2abf50924d5bfee3527657af74ddfb1da52904
PISHRINK_COMMIT=5f358d03eed4b7334657ee93867826a2b42f112a
GO_IMAGE=golang:1.27.3-bookworm@sha256:5c3e9b43cfd3bb95c7937462e29d87ada57b481a96fa23ad0eb61778d7a00110

# pi-gen assumes linux32 is supported. Apple Silicon Docker kernels can run
# armhf through QEMU while rejecting that optional personality setting.
pigen_capsh() {
    if [[ $LF_USE_LINUX32 == 1 ]]; then
        command setarch linux32 capsh "$@"
    else
        command capsh "$@"
    fi
}

prepare_pigen() {
    local common=$1/scripts/common
    [[ $(grep -c 'setarch linux32 capsh' "$common") == 2 ]] || {
        echo 'Unexpected pi-gen chroot implementation; refusing to patch it.' >&2; return 1;
    }
    sed 's/setarch linux32 capsh/pigen_capsh/g; /pigen_capsh.*BOOTSTRAP_STR/s/ || true$/ || return $?/' "$common" > "$common.tmp"
    mv "$common.tmp" "$common"
    # Some Raspbian mirrors/proxies reject pipelined package requests with HTTP
    # 400. Install conservative download settings after bootstrap, then remove
    # them before the final image is exported.
    cat >> "$1/stage0/prerun.sh" <<'APT_OPTIONS'
printf '%s\n' 'Acquire::http::Pipeline-Depth "0";' 'Acquire::Retries "3";' > "${ROOTFS_DIR}/etc/apt/apt.conf.d/99-logfalcon-build-downloads"
APT_OPTIONS
    cat >> "$1/export-image/05-finalise/01-run.sh" <<'APT_CLEANUP'
rm -f "${ROOTFS_DIR}/etc/apt/apt.conf.d/99-logfalcon-build-downloads"
APT_CLEANUP
}

container_main() {
    local mode=$1
    arm_handler="logfalcon-arm-$LF_RUN_ID"
    arm_handler_created=0
    probe_loop=""
    probe_mounted=0
    # shellcheck disable=SC2329 # Invoked by the EXIT trap.
    container_cleanup() {
        local result=$?
        trap - EXIT
        if [[ $probe_mounted -eq 1 ]]; then umount /work/probe-mount || true; fi
        if [[ -n "$probe_loop" ]]; then losetup -d "$probe_loop" || true; fi
        if [[ $arm_handler_created -eq 1 && -f "/proc/sys/fs/binfmt_misc/$arm_handler" ]]; then
            echo -1 > "/proc/sys/fs/binfmt_misc/$arm_handler" || true
        fi
        chown -R "$LF_OUTPUT_UID:$LF_OUTPUT_GID" /output || true
        exit "$result"
    }
    trap container_cleanup EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM

    for tool in go govulncheck systemctl sfdisk parted resize2fs qemu-arm-static; do
        command -v "$tool" >/dev/null || { echo "Missing container tool: $tool" >&2; exit 1; }
    done
    [[ $(getconf PAGESIZE) == 4096 ]] || {
        echo 'The Docker Linux kernel must use 4 KiB pages for this armhf image.' >&2; exit 1;
    }
    mkdir -p /proc/sys/fs/binfmt_misc
    if [[ ! -f /proc/sys/fs/binfmt_misc/register ]]; then
        mount -t binfmt_misc binfmt_misc /proc/sys/fs/binfmt_misc
    fi
    # A private registration with F (fix-binary) also works across chroot on
    # Apple Silicon, whose CPU cannot execute armhf directly. Remove it on exit.
    local registration=':M::\x7fELF\x01\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x02\x00\x28\x00:\xff\xff\xff\xff\xff\xff\xff\x00\xff\xff\xff\xff\xff\xff\xff\xff\xfe\xff\xff\xff:/usr/bin/qemu-arm-static:F'
    printf ':%s%s\n' "$arm_handler" "$registration" > /proc/sys/fs/binfmt_misc/register
    arm_handler_created=1
    # Keep linux32 wherever supported; bypass it only on an aarch64 kernel
    # with working ARM emulation. Exercise the same capsh path as pi-gen.
    LF_USE_LINUX32=1
    if ! setarch linux32 true; then
        [[ $(uname -m) == aarch64 ]] || {
            echo 'Docker cannot provide the linux32 personality required by pi-gen.' >&2; exit 1;
        }
        LF_USE_LINUX32=0
        echo 'Using ARM emulation without linux32 personality on this aarch64 kernel.'
    fi
    export LF_USE_LINUX32
    export -f pigen_capsh
    arch-test armhf
    mkdir -p /work/arm-probe
    cp /usr/lib/arch-test/armhf /work/arm-probe/test
    pigen_capsh -- -e -c 'chroot /work/arm-probe /test'
    pigen_capsh --chroot=/work/arm-probe --shell=/test -- -c probe

    # Test a real loop device and mount before spending time building an image.
    [[ -c /dev/loop-control ]] || mknod /dev/loop-control c 10 237
    local next_loop
    next_loop=$(losetup -f)
    [[ -b "$next_loop" ]] || mknod "$next_loop" b 7 "${next_loop##*loop}"
    truncate -s 32M /work/probe.img
    probe_loop=$(losetup --find --show /work/probe.img)
    mkfs.ext4 -q "$probe_loop"
    mkdir -p /work/probe-mount
    mount "$probe_loop" /work/probe-mount
    probe_mounted=1
    touch /work/probe-mount/test
    umount /work/probe-mount
    probe_mounted=0
    losetup -d "$probe_loop"
    probe_loop=""
    rm -rf /work/probe.img /work/probe-mount /work/arm-probe
    echo 'Docker checks passed: ARM chroot, loop device and filesystem mount.'

    local commit version state snapshot image filename
    commit=$(git -c safe.directory=/source -C /source rev-parse HEAD)
    version=$(git -c safe.directory=/source -C /source describe --tags --always --dirty)
    state=$(git -c safe.directory=/source -C /source status --porcelain --untracked-files=normal)
    if [[ -n "$state" && "$version" != *-dirty ]]; then version="${version}-dirty"; fi

    # Snapshot only build inputs, including current uncommitted code. No .git,
    # credentials, local logs, prior images or Docker work directories are copied.
    mkdir -p /work/src
    tar -C /source -cf - cmd internal config system boot scripts \
        pi-gen/config pi-gen/build.sh pi-gen/stage-logfalcon go.mod go.sum Makefile install.sh LICENSE |
        tar -C /work/src -xf -
    cd /work/src
    find . -type f -print0 | sort -z | xargs -0 sha256sum > /output/source-files.sha256
    snapshot=$(sha256sum /output/source-files.sha256 | cut -d' ' -f1)
    {
        printf 'Application commit: %s\nVersion: %s\nSource snapshot SHA-256: %s\n' "$commit" "$version" "$snapshot"
        printf 'Working tree: %s\n' "$([[ -n "$state" ]] && echo modified || echo clean)"
        printf 'pi-gen: %s\nPiShrink: %s\nBuilder: %s\n' "$PIGEN_COMMIT" "$PISHRINK_COMMIT" "$LF_BUILDER_IMAGE"
        printf 'OS image: Raspberry Pi OS Bookworm armhf (32-bit, ARM6 binary)\n'
        go version
    } > /output/build-info.txt

    echo 'Running source checks...'
    go test -race -count=1 ./...
    go vet ./...
    govulncheck ./...
    make VERSION="$version" COMMIT="$commit" build-pi build-pi2
    [[ $(stat -c%s bin/logfalcon-arm6) -le $((10 * 1024 * 1024)) ]] || {
        echo 'ARM6 binary exceeds 10 MiB.' >&2; exit 1;
    }

    if [[ $mode == check ]]; then
        echo 'Source checks and Pi binary builds passed; no OS image was built.'
        return
    fi

    echo 'Building the Raspberry Pi OS image...'
    cp -a /opt/pi-gen /work/pi-gen
    prepare_pigen /work/pi-gen
    cp pi-gen/config /work/pi-gen/config
    cp -a pi-gen/stage-logfalcon /work/pi-gen/
    mkdir -p /work/pi-gen/logfalcon-src
    cp -a /work/src/. /work/pi-gen/logfalcon-src/
    cp bin/logfalcon-arm6 /work/pi-gen/logfalcon-src/bin/logfalcon
    touch /work/pi-gen/stage2/SKIP_IMAGES
    cd /work/pi-gen
    # Keep the Linux filesystem on a Docker volume, not a macOS bind mount.
    # Disable upstream's interactive progress and force the pinned builder ID.
    # Explicit -e preserves upstream's #!/bin/bash -e when invoking with bash.
    GIT_HASH="$PIGEN_COMMIT" CLEAN=1 USE_QEMU=1 bash -e ./build.sh
    shopt -s nullglob
    local images=(/work/pi-gen/deploy/*.img)
    [[ ${#images[@]} -eq 1 ]] || { echo 'Expected exactly one generated .img.' >&2; exit 1; }
    image=${images[0]}
    filename=$(basename "$image")

    echo 'Shrinking and inspecting the image...'
    bash /opt/pishrink.sh "$image"
    bash /work/src/scripts/check-image-size.sh raw "$image"
    LOGFALCON_IMAGE_CONTAINER=1 bash /work/src/scripts/inspect-image.sh --native "$image"
    echo 'Compressing the image...'
    xz -9 -T2 "$image"
    bash /work/src/scripts/check-image-size.sh compressed "${image}.xz"
    xz -t "${image}.xz"
    cp "${image}.xz" /output/
    cd /output
    cp "/work/pi-gen/deploy/${filename%.img}.info" /output/
    sha256sum "${filename}.xz" "${filename%.img}.info" build-info.txt source-files.sha256 > checksums.txt
    sha256sum --check checksums.txt
    echo "SUCCESS: ${filename}.xz is ready for physical evaluation."
}

if [[ ${1:-} == --container ]]; then
    [[ ${LOGFALCON_IMAGE_CONTAINER:-} == 1 && $(uname -s) == Linux && $EUID -eq 0 ]] || exit 2
    container_main "${2:-build}"
    exit 0
fi

host_main() {
mode=build
case "${1:-}" in
    --help|-h)
        echo 'Usage: bash pi-gen/build.sh [--check]'
        echo 'Default: build, shrink, inspect, compress and checksum the current checkout.'
        echo '--check: validate Docker, ARM emulation, source checks and Pi binaries without building an OS image.'
        echo 'Output: pi-gen/deploy/<run>/ (image, checksums, source identity and build.log).'
        exit 0 ;;
    --check) mode=check ;;
    '') ;;
    *) echo 'Usage: bash pi-gen/build.sh [--check]' >&2; exit 2 ;;
esac
[[ $# -le 1 ]] || { echo 'Too many arguments.' >&2; exit 2; }
command -v docker >/dev/null || { echo 'Install Docker Desktop (Mac) or Docker Engine (Linux) first.' >&2; exit 1; }
if ! docker info >/dev/null 2>&1; then
    if [[ $(uname -s) == Darwin ]]; then
        echo 'Starting Docker Desktop...'
        open -a Docker
        for ((attempt=0; attempt<60; attempt++)); do
            if docker info >/dev/null 2>&1; then break; fi
            sleep 2
        done
    fi
    docker info >/dev/null 2>&1 || { echo 'Docker is not ready. Start it and retry.' >&2; exit 1; }
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
output="$SCRIPT_DIR/deploy/$run_id"
container="logfalcon-image-$run_id"
volume="$container"
builder=logfalcon-image-builder:go1.27.3
mkdir -p "$output"
tooling=$(mktemp -d)
volume_created=0
builder_id=""
cp "$SCRIPT_DIR/build.sh" "$tooling/entry.sh"
cleanup() {
    local result=$?
    trap - EXIT
    docker rm -f "$container" >/dev/null 2>&1 || true
    if [[ $volume_created -eq 1 ]]; then
        # A forced interruption can skip the worker's trap. A separate namespace
        # releases only this run's binfmt handler and loops backed by its volume.
        docker run --rm --privileged --mount "type=volume,source=$volume,target=/work" \
            -e "LF_ARM_HANDLER=logfalcon-arm-$run_id" "$builder_id" bash -eu -c '
            if [[ ! -f /proc/sys/fs/binfmt_misc/register ]]; then
                mount -t binfmt_misc binfmt_misc /proc/sys/fs/binfmt_misc
            fi
            if [[ -f /proc/sys/fs/binfmt_misc/$LF_ARM_HANDLER ]]; then
                echo -1 > "/proc/sys/fs/binfmt_misc/$LF_ARM_HANDLER"
            fi
            findmnt -rn -o TARGET | sort -r | while IFS= read -r target; do
                case "$target" in /work/*) umount "$target" || true ;; esac
            done
            find /work -maxdepth 6 -type f -name "*.img" -print0 | while IFS= read -r -d "" image; do
                losetup -n -O NAME -j "$image" | while IFS= read -r device; do losetup -d "$device"; done
            done
            ' >> "$output/build.log" 2>&1 || echo "Docker cleanup incomplete; work volume: $volume" >&2
        docker volume rm "$volume" >/dev/null 2>&1 || echo "Could not remove Docker work volume: $volume" >&2
    fi
    rm -rf "$tooling"
    echo "Build log: $output/build.log"
    if [[ $result -ne 0 ]]; then echo 'Image build did not complete; see the log above.' >&2; fi
    exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cat > "$tooling/Dockerfile" <<DOCKERFILE
FROM $GO_IMAGE
ENV DEBIAN_FRONTEND=noninteractive GOTOOLCHAIN=local
RUN apt-get update && apt-get install -y --no-install-recommends \\
    quilt parted coreutils qemu-user-static debootstrap zerofree zip dosfstools \\
    e2fsprogs libcap2-bin rsync grep xz-utils curl xxd file kmod bc gpg pigz \\
    arch-test binfmt-support ca-certificates fdisk udev systemd python3 make git \\
    sudo wget openssl && rm -rf /var/lib/apt/lists/*
RUN git init /opt/pi-gen && git -C /opt/pi-gen remote add origin https://github.com/RPi-Distro/pi-gen.git \\
    && git -C /opt/pi-gen fetch --depth 1 origin $PIGEN_COMMIT \\
    && git -C /opt/pi-gen checkout --detach FETCH_HEAD
RUN curl --fail --location --retry 3 \\
    https://raw.githubusercontent.com/Drewsif/PiShrink/$PISHRINK_COMMIT/pishrink.sh \\
    -o /opt/pishrink.sh
RUN GOBIN=/usr/local/bin go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
DOCKERFILE

echo "Preparing Docker build tools; output: $output"
docker build -t "$builder" "$tooling" 2>&1 | tee "$output/build.log"
builder_id=$(docker image inspect "$builder" --format '{{.Id}}')
docker volume create "$volume" >/dev/null
volume_created=1
# Privilege is needed for ARM binfmt, loop devices and mounts. No host Docker
# socket or host devices are bind-mounted; the source checkout is read-only.
docker run --rm --init --privileged --name "$container" \
    --mount "type=bind,source=$REPO_ROOT,target=/source,readonly" \
    --mount "type=volume,source=$volume,target=/work" \
    --mount "type=bind,source=$output,target=/output" \
    --mount "type=bind,source=$tooling/entry.sh,target=/run/logfalcon-build.sh,readonly" \
    -e LOGFALCON_IMAGE_CONTAINER=1 -e "LF_OUTPUT_UID=$(id -u)" -e "LF_OUTPUT_GID=$(id -g)" \
    -e "LF_BUILDER_IMAGE=$builder_id" -e "LF_RUN_ID=$run_id" "$builder_id" \
    bash /run/logfalcon-build.sh --container "$mode" 2>&1 | tee -a "$output/build.log"
if [[ $mode == build ]]; then
    echo "Finished. Flash the .img.xz in: $output"
else
    echo 'Preflight passed. Run bash pi-gen/build.sh to build the full image.'
fi
}

host_main "$@"
