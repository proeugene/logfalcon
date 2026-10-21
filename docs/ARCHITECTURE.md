# How LogFalcon works

LogFalcon collects a controller's SPI-flash dump between flights. The current
evaluation target is Betaflight on a Pi Zero 2 W. iNav copying is blocked.
Build targets also exist for Pi Zero W and Linux AMD64; that is not hardware proof.

## One binary, separate processes

| Process | Responsibility | Privilege |
|---|---|---|
| `logfalcon@tty*.service` | udev starts one USB copy; exits with its outcome | `bbsyncer`, serial access |
| `logfalcon-web.service` | saved sessions, downloads, settings, status | `bbsyncer`, bind port 80 |
| `logfalcon-ready-led.service` | sole running owner of the Pi ACT LED | root, sysfs |
| config apply / diagnostics | apply boot settings; export bounded journal tails | root services |

The copy process atomically publishes boot/PID/transfer-scoped JSON in `/run`.
Web and LED processes read that file rather than separate Go globals. A missing
file means waiting, a stale active writer means interrupted, and unreadable
status means completion is unconfirmed. A failed publication aborts copying or
prevents erase and makes the process fail. Tests: [shared status](../internal/sync/status_test.go)
and [transfer failures](../internal/sync/transfer_test.go).

Packages follow the data flow: `config` validates settings, `fc` identifies the
controller, `msp` handles framing/serial requests, `sync` controls ordering,
`storage` owns files/manifests, and `web` exposes the saved results. `led` and
`diagnostics` observe outcomes. `settings` coordinates browser and boot changes.
Start at [main.go](../cmd/logfalcon/main.go) and [Orchestrator.Run](../internal/sync/orchestrator.go).

## Copy and recovery ordering

1. Validate configuration and acquire the cross-process transfer lock. Reject
   overlapping copies and copies after an idle shutdown has been scheduled.
2. Publish a new transfer identity, open USB, identify Betaflight and confirm
   SPI flash support, readiness and used size.
3. Check storage reserve. Cleanup is opt-in. Create a unique session directory
   and write `raw_flash.bbl.partial`, with bounded requests and timeouts.
4. Flush and fsync; require the exact byte count and compare the received stream
   hash with a reread of the saved file. This checks saved-copy consistency,
   not an independent controller checksum or valid blackbox structure.
5. Rename to `raw_flash.bbl`, sync directory entries, then durably write the
   manifest. Only after those operations may explicitly enabled erasure begin.
   Record erase attempted before sending it; record completion only after the
   controller reports ready and empty. Dry run always skips erasure.

USB/storage errors retain partial evidence, report failure and leave flash
untouched. A power cut may lose recent events; filesystem sync reduces exposure
but does not prove an SD card's power-loss behaviour. Recovery starts a new copy,
not a resume from a partial file. See [storage](../internal/storage),
[wire-level orchestration tests](../internal/sync/transfer_test.go) and
[physical acceptance](../DEVELOPMENT_TESTING.md).

## Configuration and trust boundaries

The appliance trusts devices on its Wi-Fi. HTTP has **no login or TLS**. Connected
clients can download/delete saved sessions and change hotspot settings. CSRF
tokens guard mutations, but do not authenticate a pilot. Use a private password;
do not expose port 80 to another network. The image's administration account also
has a documented initial password and must be changed before shared use.

Pilot titles/notes are optional `pilot-notes.json` sidecars within each saved
session. Controller nicknames live in `pilot-controller.json` within the existing
controller UID directory and apply to its recordings. They do not change the
controller, Wi-Fi, `.bbl` or manifest. Annotation reads/writes use storage-rooted
handles, bounded text and atomic publication. Missing files mean unlabelled;
unreadable files are preserved and editing is blocked until storage is repaired.
Connected clients can read/edit these annotations too. Session deletion also
deletes its notes; controller nicknames remain for subsequent copies.

The dashboard's timestamps identify collection time, not flight time. A flash
copy may contain several flights; retained controller data may appear in later
copies. Notes are pilot observations. No flight splitting, decoding or automatic
diagnosis is performed; downloads can be opened separately in Betaflight App's
Blackbox Viewer.

Downloads, session deletion and copy publication use directory handles anchored
within storage (`os.Root`), rather than path-string prefix checks. Symlinked
sessions/files are rejected; rooted operations constrain path changes during
use. Local administrators and anyone able to modify the SD card remain trusted.
Tests: [containment](../internal/web/containment_test.go).

The web account can write `/etc/logfalcon`, restart only hostapd, and start one
fixed shutdown service through polkit. Executables and policies remain root
owned. Browser and boot updates share a lock and a durable undo journal. Failure
restores the original files and restarts hostapd; interrupted or failed recovery
is retried before another update. The boot hash is recorded only after a
confirmed restart. A failed boot apply blocks the web and copy services.
Tests: [transactions](../internal/settings/transaction_test.go).

Idle shutdown defaults off. Active copies and in-progress user requests prevent
it; completed/error results can become idle. Polls and captive probes do not
extend the timer. A transfer lock and `/run` reservation exclude a copy starting
while shutdown is queued. See [activity tests](../internal/web/activity_test.go).

Diagnostics default on, record metadata rather than payloads/passwords, and
export bounded redacted tails onto the boot partition for Mac retrieval. Review
them before sharing: controller IDs, addresses and usage times can be identifying.

## Deliberate trade-offs

A single Go binary avoids an application runtime or frontend build on a small
Pi. Standard HTTP downloads and embedded HTML keep the UI small. Compression was
removed following provenance review; uncompressed transfers reduce protocol and
licensing uncertainty. System services still provide Wi-Fi, USB, permissions and
journals, so a successful Go build is not an appliance acceptance test.

See [AI-assisted development](AI_ASSISTED_DEVELOPMENT.md),
[compatibility evidence](FC_COMPATIBILITY_ANALYSIS.md) and
[release gates](../RELEASE_CHECKLIST.md).

## Device setup and signed offline update

A fresh-install-only identity.pending marker makes the boot configuration
transaction generate and persist a random device identity and unique default SSID
on the installed appliance. The shared image contains no generated identity.
Explicit names and all upgraded installations are preserved. Optional setup/skip
is stored through the existing settings lock and undo journal, with a modest
default-password reminder. Passwords remain literal and are never rendered back
to the browser. Reconnection and the effective settings page confirm application.

The dashboard polls shared status and fetches a saved-session fragment once per
new completed transfer. New cards are inserted without replacing existing
controls/details/download anchors. Manifests explicitly record data retention
when erase was skipped; older manifests without that outcome remain unconfirmed.
Saved-file consistency remains distinct from a controller checksum.

The additional privileged process is the fixed logfalcon-update.service, driven
by logfalcon-update.path and enabled at boot for recovery. It re-verifies signed
complete payloads, excludes transfer/shutdown, journals asset backups, activates
atomically per file, checks the new application identity/health and restores the
previous assets after failure/interruption. The recovery executable stays old
until health confirmation. It never rewrites application/Wi-Fi configuration.
The web process remains unprivileged; no new login/guest model is introduced.
See [format, trust rotation and historical support](OFFLINE_UPDATES.md).

## Selected recording export

The dashboard submits up to 100 selected session identifiers to the CSRF-protected
`POST /download-selected` route. The server validates manifests, file availability
and lengths, and annotation readability before starting a ZIP. It holds rooted
file handles and streams original `.bbl` bytes, original manifests, annotation
snapshots and controller names under controller/session directories. Duplicate
selections are removed; user titles do not become archive paths. Missing optional
annotations export as empty metadata. No source files or erase settings change.

ZIPs use stored entries to avoid compression work on the Pi. They are streamed
without an archive-sized memory buffer or temporary ZIP. Active exports count as
activity for idle shutdown. A write/read failure aborts the response without
finalising a misleadingly complete archive. Long downloads renew the write
deadline, while stalled connections still time out. HTTP disconnects and disk
failures remain reasons to retry; browser download completion is not inferred
from requesting an export.
