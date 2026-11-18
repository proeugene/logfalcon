# Offline application updates

This is an implemented local candidate, not physical acceptance or release
readiness. It updates the Go application and the fixed integration asset list
in internal/update/package.go. It does not upgrade Raspberry Pi OS, add internet
access, or enable iNav. iNav — Experimental; copying currently disabled.

## Pilot journey

Download the architecture-specific .lfu file while online. Join the appliance's
unique LogFalcon Wi-Fi, open http://192.168.4.1, then Software update (also linked
from Settings). Upload, review the authenticated target version and install.
Keep power connected, reconnect if necessary and wait for the installed version
and confirmed success. A dropped connection or successful upload is not proof
of installation. Reload an old cached form after a restart/CSRF rejection.

Packages use the installed application/OS ABI: arm6 for a 32-bit image, including
a Zero 2 W running that image; arm64 for a 64-bit OS; amd64 for Linux AMD64.
The binary's ELF machine, Go build ABI, version and source commit must agree
with signed metadata. Wrong architectures, downgrades and ambiguous prerelease
ordering are rejected. Within the same numeric version, only prerelease to final
is accepted; moving between prereleases requires a higher numeric version.

## Package and privilege boundary

Format 1 is a complete ZIP release, not a delta. Limits: 32 MiB upload, 64 MiB
expanded data, 64 entries and 64 KiB manifest. The manifest signs version, full
commit, architecture, format, base contract, migration number, file lengths and
SHA-256 hashes. An Ed25519 signature authenticates the manifest. Bundled hashes
alone are not trust. Unexpected paths/files, duplicate ZIP entries, symlinks,
invalid keys and incomplete payloads are rejected without activation.

The web process remains bbsyncer. It writes only the bounded inbox; root-owned
public keys live in /usr/share/logfalcon/update-keys. A fixed systemd path/service
runs /opt/logfalcon/updater --update-worker as a separately constrained process.
It snapshots and re-verifies web-owned input before using it. It never executes
an uploaded script during validation or accepts arbitrary installation paths.
Release-owned integration scripts are installed only after signature verification.

Activation holds the existing cross-process transfer/shutdown lock. A queued update waits while a copy owns that lock; lock contention never overwrites a recovery journal. The durable
root-owned state and previous asset copies live under
/var/lib/logfalcon-update/state. Previous assets are backed up before publishing
the activation journal. Each replacement is fsynced and atomically renamed.
The old recovery executable is retained until the new web application is healthy.
A restart reads pending activation state and restores previous assets before
accepting another update; copies reject pending recovery. Health confirmation is
bounded and must match version/commit and application readiness. Failed recovery
remains pending and retryable. A successful rollback reports restoration, not
new-release success. Configuration and saved logs are never replaced by a package.

Only one previous application is retained. These filesystem/process tests cannot
establish SD-card power-loss behaviour. On a damaged filesystem, restore the spare
card or recover the root-owned previous assets offline. Do not delete a pending
journal to force activation. Run the updater service again after correcting the
underlying storage/service failure. Journal exports already include logfalcon*
services; errors contain metadata, never credentials. Review diagnostics before sharing.

## Direct-upgrade evidence and limits

Inventory was made from local tags through v.0.4.8-rc1 and test baseline 66b21ff.
Tags v0.1.0–v0.2.1 are Python-era; Go starts at v0.3.0. The early v0.1.0–v0.1.2
layout used /opt/bbsyncer, /etc/bbsyncer and /mnt/bbsyncer-logs; later layouts use
logfalcon paths. Current releases before this candidate have no dashboard uploader.

| Historical installation | Implemented route | Evidence / remaining limitation |
|---|---|---|
| Complete Go/TOML, standard Bookworm/systemd layout, v0.3.0 onwards | One-time verified source installer bootstrap, then latest signed package | Actual tag config fixtures for v0.3.0, v0.3.8, v0.4.0, v0.4.6, v0.4.7-rc1 and v.0.4.8-rc1; local installer upgrade fixture; no exact old-image acceptance |
| Python-era v0.2.0 | Config fixture only; runtime migration unsupported automatically | Original TOML parser preservation is tested; Python services/venv and rollback are not validated |
| Earlier Python/bbsyncer layouts, missing/corrupt config, divergent service layout, custom status_path or non-Bookworm base | Stop and recover/migrate explicitly | No automatic path claim; preserve logs/config offline and use a spare current image if needed |
| This candidate with uploader and a provisioned trust anchor | Direct latest package, including cumulative key delegation | Signature, real executable metadata, migration/config preservation and failure fixtures; physical systemd/image upgrade still unverified |

Migration contract 1 retains configuration bytes. Every supported prior Go
configuration loads into the current schema without rewriting its values. In
particular, old explicit erase_after_sync=true and storage_pressure_cleanup=true
remain true. This is preservation, not an endorsement of destructive evaluation.
Set both false deliberately before hardware evaluation if that is your intent.
erase_after_sync, storage_pressure_cleanup, flash_read_compression, credentials
and storage_path must be explicit. Missing values, malformed TOML, inconsistent
hostapd credentials or enabled unsupported compression block activation. Resolve
them as an operator decision; the updater never substitutes launch defaults.

Retain migration contract 1 and its original-tag fixtures cumulatively in future
releases. Add a new migration number and retained fixture only for an actual schema
change, and extend the fixed recovery implementation before claiming it supported.
No universal upgrade compatibility is established by these fixtures.

### Published asset inventory

The GitHub releases API was read on 21 October 2026, separately from the local
tag inventory. [Release collection](https://github.com/proeugene/logfalcon/releases).

| Published release group | Assets observed |
|---|---|
| v.0.4.8-rc1 | Prerelease entry, no uploaded assets |
| v0.3.7–v0.4.6 | Dated logfalcon.img.xz image and Linux arm6/arm64/amd64 binaries |
| v0.3.4–v0.3.6 | Dated logfalcon.img.xz image only |
| v0.1.0–v0.1.2 | Dated bbsyncer.img.xz image only |

Other local tags in this range were not entries in that API response. These
published releases had no signed update files or complete current integration
asset/checksum/bootstrap bundle. They cannot be treated as a source for fallback
assets. For example, v0.4.6 lists 2026-03-11-logfalcon.img.xz (466,467,332 bytes,
GitHub digest sha256:feda15861f43727d4f4a68f4f10b23a57c137e6840343cb30856ff10620179fd).
No historical image was downloaded, mounted or booted in this session: installed
layout claims are based on tagged installer/image source, pending exact-image
confirmation. Source tags, an uploaded image name and physical validation are
separate evidence.

## One-time bootstrap
 without reflashing (standard Go installation)

Perform this on a spare card first. The source installer bootstrap is an
administrator operation, separate from transactional dashboard package activation.
It is not a power-cut-safe migration of arbitrary legacy installations.

1. Obtain the chosen release's source archive, matching installed-ABI binary,
   checksums and public keys over HTTPS. Confirm the archive/binary SHA-256 and
   public-key fingerprint through an independently trusted maintainer channel.
   A checksum downloaded beside an artifact is only transport integrity.
2. Verify checksums as the ordinary user before extracting or running anything.
   Extract the verified source archive into a new directory, place the verified
   binary at bin/logfalcon-arm6, bin/logfalcon-arm64 or bin/logfalcon-amd64, and
   confirm the public keys in update-keys belong to that same reviewed release.
3. Preserve a copy of /etc/logfalcon, the boot config and logs. Stop active use;
   unplug the controller. Run the verified binary's --version and
   --check-upgrade-config. The source installer repeats config validation and
   takes the transfer lock before system changes.
4. Run sudo bash install.sh from that verified release directory, then reboot.
   It preserves existing names/passwords/country/debug/application config; it
   creates no identity.pending marker for an upgrade. Do not use a script or
   integration fallback from main.
5. Confirm the existing Wi-Fi, saved logs and installed version. The uploader is
   then available for later direct packages. Retain the spare card and backup.

The HTTPS release installer follows the same preflight/preservation rules and
verifies downloaded assets against release checksums before machine changes.
For first trust, pin the independently verified source/bootstrap route above.
Both installers reject missing Go executables instead of guessing a Python migration.

## Maintainer signing setup (not performed by the coding assistant)

Use a secure maintainer machine. Generate and retain a secret outside the repo:

~~~sh
umask 077
openssl rand -hex 32 > /secure/location/logfalcon-release.seed
UPDATE_SIGNING_SEED="$(cat /secure/location/logfalcon-release.seed)"   go run ./cmd/update-package -public-key update-keys/release-2026.pub
~~~

Commit only release-2026.pub and review its fingerprint. Put the 64-character
seed in the repository Actions secret UPDATE_SIGNING_SEED and set the repository
Actions variable UPDATE_SIGNING_KEY_ID to release-2026. Restrict tag/release
workflow execution to trusted maintainers. Never expose the secret to PR jobs,
enable shell tracing, print it, or include it in an artifact. No external secrets
or repository settings were changed by this implementation.

Tag releases build/sign/self-verify one package per ABI from the same commit as
the binaries and integration assets. Missing seed, key ID, public key, package
or image fails the workflow; unsigned packages are not published. The image and
both installer paths provision public keys. There are deliberately no production
keys committed by this task, so release signing is inactive until setup is done.

For a local maintainer build (replace the version/commit with the exact release):

~~~sh
UPDATE_SIGNING_SEED="$(cat /secure/location/logfalcon-release.seed)"  go run ./cmd/update-package -binary bin/logfalcon-arm6  -version v0.5.0 -commit FULL_40_CHARACTER_COMMIT -arch arm6  -key-id release-2026 -out logfalcon-v0.5.0-arm6.lfu
go run ./cmd/update-package -verify logfalcon-v0.5.0-arm6.lfu -arch arm6
~~~

### Rotation without intermediate installations

Generate a new seed and public key outside the repo as above. While the old key
is still trusted, create a public delegation certificate with the old seed:

~~~sh
UPDATE_SIGNING_SEED="$(cat /secure/location/OLD.seed)"  go run ./cmd/update-package -certify update-keys/release-2027.pub  -key-id release-2027 -signed-by release-2026 > /tmp/delegation.json
~~~

Append that public object to the JSON array update-keys/rotation.json, preserving
all earlier certificates, and commit the new public key/cumulative array. Switch
the CI secret and key ID to the new seed/ID. The latest package carries the
cumulative signed chain, so an original-trust appliance can skip the rotation
release. Tests exercise that direct jump and reject forged delegation.

This is planned key rotation, not automatic revocation of compromised trust
anchors. Remove a compromised anchor through an independently authenticated
administrator recovery/update of the trust store, and reassess the supported
upgrade matrix. Do not claim that a certificate signed by a compromised key
revokes that key.
