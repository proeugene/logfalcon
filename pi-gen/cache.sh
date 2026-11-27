#!/usr/bin/env bash
# Shared by the local wrapper and CI. Cache only the pristine stage2 rootfs.

os_cache_policy() {
    case "$1" in
        refs/heads/main|refs/tags/v*) echo clean ;;
        refs/heads/*) echo cached ;;
        *) echo clean ;; # Unknown/detached refs are never a release-cache shortcut.
    esac
}

os_cache_key() (
    cd "$1" || exit 1
    # Include the complete build recipe/config, but no application code. Bump the
    # schema if the snapshot format changes. Paths are relative for portability.
    {
        printf 'logfalcon-os-cache-v1-armhf\n'
        sha256sum pi-gen/config pi-gen/build.sh pi-gen/cache.sh
    } | sha256sum | cut -d' ' -f1
)

os_cache_restore() (
    local cache=$1 key=$2 root=$3 entry
    [[ -n $cache && $key =~ ^[0-9a-f]{64}$ ]] || exit 1
    entry="$cache/$key"
    mkdir -p "$cache" || exit 1
    exec 9>"$cache/cache.lock"
    flock 9 || exit 1
    [[ -f "$entry/rootfs.tar" && -f "$entry/rootfs.sha256" && -s "$entry/info" && ! -e "$root" ]] || exit 1
    (cd "$entry" && sha256sum --check --status rootfs.sha256) || exit 1
    mkdir -p "$root" || exit 1
    if ! tar --acls --xattrs --numeric-owner -xpf "$entry/rootfs.tar" -C "$root"; then
        rm -rf "$root"
        exit 1
    fi
    # A base snapshot must never contain installed LogFalcon/device state.
    if [[ -e "$root/opt/logfalcon" || -e "$root/etc/logfalcon" ]]; then
        rm -rf "$root"
        exit 1
    fi
    cat "$entry/info"
)

os_cache_save() (
    local cache=$1 key=$2 root=$3 provenance=$4 pending
    umask 022
    [[ -n $cache && $key =~ ^[0-9a-f]{64}$ ]] || exit 1
    [[ -d "$root/etc" && ! -e "$root/opt/logfalcon" && ! -e "$root/etc/logfalcon" ]] || exit 1
    mkdir -p "$cache" || exit 1
    exec 9>"$cache/cache.lock"
    flock 9 || exit 1
    pending=$(mktemp -d "$cache/.pending.XXXXXX") || exit 1
    trap 'rm -rf "$pending"' EXIT
    tar --acls --xattrs --numeric-owner --sparse -cpf "$pending/rootfs.tar" -C "$root" . || exit 1
    (cd "$pending" && sha256sum rootfs.tar > rootfs.sha256) || exit 1
    {
        printf 'OS base created: %s\n' "$(date -u +%FT%TZ)"
        printf 'OS base archive SHA-256: %s\n' "$(cut -d' ' -f1 "$pending/rootfs.sha256")"
        printf 'OS base builder: %s\n' "$provenance"
    } > "$pending/info"
    chmod 0755 "$pending" || exit 1
    # Called only after the entire image passes inspection/checksums. Readers
    # share this lock, so they cannot observe a partial replacement.
    rm -rf "${cache:?}/$key" || exit 1
    mv "$pending" "$cache/$key"
)

run_pigen() {
    local pigen=$1 cache=$2 key=$3 policy=$4 info=$5 root reused=0
    root="$pigen/work/logfalcon/stage2/rootfs"
    if [[ $policy == cached ]] && os_cache_restore "$cache" "$key" "$root" > "$pigen/os-base.info"; then
        reused=1
        touch "$pigen/stage0/SKIP" "$pigen/stage1/SKIP" "$pigen/stage2/SKIP"
        echo 'OS base: using compatible cached stages 0–2.'
    else
        echo "OS base: building stages 0–2 from scratch (policy: $policy)."
    fi
    {
        printf 'OS cache policy: %s\nOS cache key: %s\nOS cache reused: %s\n' "$policy" "$key" "$reused"
        if [[ $reused == 1 ]]; then cat "$pigen/os-base.info"; fi
    } >> "$info"
    # Upstream still runs its stage loop, assigning PREV_ROOTFS_DIR to stage2
    # even when skipped. The application and export stages always run fresh.
    (cd "$pigen" && GIT_HASH="$PIGEN_COMMIT" CLEAN=1 USE_QEMU=1 WORK_DIR="$pigen/work/logfalcon" bash -e ./build.sh)
}
