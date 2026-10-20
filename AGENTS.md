# LogFalcon — Agent Guide

## Go project — not Python
The active codebase is **all Go** under `cmd/logfalcon/` and `internal/`. Go module: `github.com/proeugene/logfalcon`, requires **Go 1.27 (tested patch 1.27.3)**. Do not reintroduce Python runtime/build dependencies.

## Entrypoint
`cmd/logfalcon/main.go` — two modes: `--web` (HTTP server with embedded templates) or `--port /dev/ttyACM0` (sync over serial). Version/commit injected via ldflags at build time (`main.Version`, `main.BuildCommit`).

## Commands
```
make test       # go test -race -v -cover ./...
make lint       # golangci-lint run ./...  (no local .golangci.yml — uses default config)
make build      # go build -o bin/logfalcon ./cmd/logfalcon
make build-pi   # cross-compile ARM6 (Pi Zero W)
make build-pi2  # cross-compile ARM64 (Pi Zero 2 W)
make clean      # rm -rf bin/
```

## Tests
Tests run without hardware, using serial/GPIO fakes and real temporary files. Linux-native installer integration is required in Linux CI; the macOS fixture adapts sed syntax. Run `go test -race -coverprofile=cover.out -covermode=atomic ./...` for CI-accurate coverage.

## Single-binary architecture
No monorepo. No packages published. Internal packages under `internal/`:
- `config/` — TOML loader (search: `--config` flag → `/etc/logfalcon/logfalcon.toml` → `./config/logfalcon.toml` → defaults)
- `msp/` — MSP v1/v2 framing, 14-state decoder, CRC, uncompressed serial client
- `fc/` — FC detection (MSP handshake), version checking (too-old = hard error, too-new = amber warning)
- `sync/` — 10-step state machine orchestrator; thread-safe status published atomically to `/run/logfalcon/status.json`; the separate web and LED services read it
- `storage/` — session dirs (`fc_VARIANT_uid-XXXXXXXX/YYYY-MM-DD_HHMMSS/`), manifest.json, stream writer
- `web/` — stdlib `net/http` server, embedded HTML templates in Go code (no JS framework), SSE for real-time progress, captive portal responses
- `led/` — 6-state blink pattern controller (`sysfs` backend for Pi ACT LED, `gpio` stub)
- `util/` — disk space helpers

## Web UI quirks
- Templates are Go string constants in `internal/web/templates.go` — not separate files.
- CSRF token generated at server start, required for DELETE and POST /settings.
- Captive portal paths respond at `/hotspot-detect.html`, `/generate_204`, `/ncsi.txt`, etc. (iOS/Android/Windows compatibility).
- Health endpoint at `GET /health` returns JSON with storage, hotspot, and shared sync status.
- SSE endpoint at `GET /events` streams sync status every 2 seconds.

## SD card image build
Uses [pi-gen](https://github.com/RPi-Distro/pi-gen) in Docker. Run `bash pi-gen/build.sh` from the repository root (requires Docker on macOS or Linux). The wrapper provides Linux tools, source checks, image shrinking/inspection, compression and checksums; `--check` stops before the OS-image build. Release CI builds and uploads images on `v*` tags via `.github/workflows/release.yml`.

## System integration
- **udev**: `/etc/udev/rules.d/99-betaflight-fc.rules` triggers `logfalcon@ttyACM*.service` on FC plug-in
- **systemd**: `logfalcon@.service` (oneshot sync, triggered by udev), `logfalcon-web.service` (always-running web server, binds port 80 with `CAP_NET_BIND_SERVICE`)
- **LED**: boot LED service runs during startup, dedicated status LED service owns sysfs and distinguishes success/error; async diagnostics export runs after sync

## CI pipeline
`.github/workflows/go-ci.yml`: lint → test (with race detector) → build (arm6, arm64, amd64). `.github/workflows/release.yml` handles tag releases, including binaries, SD card image artifacts, and checksums. Dependabot updates Go deps and GitHub Actions weekly.

## Config defaults (from `internal/config/config.go`)
- Baud: 921600, chunk: 4096, erase timeout: 120s
- Erase after sync: false; storage-pressure cleanup: false, flash read compression: false
- LED backend: sysfs, storage: `/mnt/logfalcon-logs`, min free: 200 MB
- Hotspot: SSID `LogFalcon`, password `fpvpilot`, web port 80; Wi-Fi country `GB` (UK)

## Diagnostics and acceptance
- New images default to UK English, UK keyboard and Europe/London timezone.
- Persistent journald: approximately 50 MiB/seven days. Debug metadata is enabled by default (`DEBUG=true`); use `DEBUG=false` to disable it.
- `--diagnostics start|export|stop` emits a bounded, redacted `LOGFALCON-DIAGNOSTICS/latest.json` on the mounted FAT boot partition.
- See `DEVELOPMENT_TESTING.md`; automated tests are not physical image/Wi-Fi/FC acceptance.
- iNav automatic copying is blocked pending validation of its current dataflash wire layout.

## Safety invariants and review
- Preserve unrelated working-tree changes. Do not publish or claim physical validation from a build.
- Default erase and storage cleanup off. Reject invalid settings; never fall back after an explicit config error.
- Erase requires exact-length transfer, successful saved-copy consistency check, durable file/manifest publication, healthy status publication and explicit opt-in. Dry run always prevents erase.
- Retain partial evidence on failure; never turn panic, timeout, disconnect or status-write failure into success.
- iNav copying remains blocked. Compression is unsupported; do not restore the removed upstream table.
- Use storage-rooted file handles for untrusted session paths. Never use string-prefix containment checks.
- Browser and boot settings share the settings lock/undo journal. Record boot applied hash only after confirmed restart; failed recovery must block changes and remain retryable.
- Preserve credentials, country, debug and application settings during upgrades. Do not reset missing/incomplete upgrade configuration to launch defaults.
- Only the dedicated service drives sysfs LEDs. Missing status means waiting, not confirmed readiness; corrupt/stale status cannot imply completion.
- Debug may log operation metadata, never passwords or MSP payloads. Keep journal/export bounds and review diagnostics before sharing.
- Keep binaries, integration assets, source identity and checksums on one release commit. Validate downloads before machine changes. Do not fetch fallback assets from main.

Required before proposing a release: Go race/coverage suite, vet, pinned default lint,
bounded decoder fuzzing, actionable vulnerability scan, ShellCheck, Linux installer
integration, native and Linux ARM6/ARM64/AMD64 builds. See CONTRIBUTING.md for exact
commands. Hardware/image results belong in DEVELOPMENT_TESTING.md. After at most
two internal refinement passes, move to exact-image physical acceptance and one
independent pilot's feedback; avoid further cosmetic expansion.

Read docs/ARCHITECTURE.md, docs/PROVENANCE.md and docs/AI_ASSISTED_DEVELOPMENT.md
before changing safety, compatibility or provenance claims. Explain evidence and
uncertainty; do not invent maintainer reasoning, review history or field adoption.
