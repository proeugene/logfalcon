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
