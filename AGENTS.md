# LogFalcon — Agent Guide

## Go project — not Python
The active codebase is **all Go** under `cmd/logfalcon/` and `internal/`. Go module: `github.com/proeugene/logfalcon`, requires **Go 1.23+**. Do not reintroduce Python runtime/build dependencies.

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
All tests run without hardware — serial, GPIO, and filesystem are mocked via interfaces. Run `go test -race -coverprofile=cover.out -covermode=atomic ./...` for CI-accurate coverage.

## Single-binary architecture
No monorepo. No packages published. Internal packages under `internal/`:
- `config/` — TOML loader (search: `--config` flag → `/etc/logfalcon/logfalcon.toml` → `./config/logfalcon.toml` → defaults)
- `msp/` — MSP v1/v2 framing, 14-state decoder, CRC, Huffman decompression, serial client
- `fc/` — FC detection (MSP handshake), version checking (too-old = hard error, too-new = amber warning)
- `sync/` — 10-step state machine orchestrator; thread-safe `Status` shared with web server via `GetStatus()`/`SetStatus()`
- `storage/` — session dirs (`fc_VARIANT_uid-XXXXXXXX/YYYY-MM-DD_HHMMSS/`), manifest.json, stream writer
- `web/` — stdlib `net/http` server, embedded HTML templates in Go code (no JS framework), SSE for real-time progress, captive portal responses
- `led/` — 6-state blink pattern controller (`sysfs` backend for Pi ACT LED, `gpio` stub)
- `util/` — disk space helpers

## Web UI quirks
- Templates are Go string constants in `internal/web/templates.go` — not separate files.
- CSRF token generated at server start, required for DELETE and POST /settings.
- Captive portal paths respond at `/hotspot-detect.html`, `/generate_204`, `/ncsi.txt`, etc. (iOS/Android/Windows compatibility).
- Health endpoint at `GET /health` returns JSON with storage, hotspot, and sync status.
- SSE endpoint at `GET /events` streams sync status every 2 seconds.

## SD card image build
Uses [pi-gen](https://github.com/RPi-Distro/pi-gen) in Docker. Run `cd pi-gen && bash build.sh` (takes 30-60 min). Release CI builds and uploads images on `v*` tags via `.github/workflows/release.yml`.

## System integration
- **udev**: `/etc/udev/rules.d/99-betaflight-fc.rules` triggers `logfalcon@ttyACM*.service` on FC plug-in
- **systemd**: `logfalcon@.service` (oneshot sync, triggered by udev), `logfalcon-web.service` (always-running web server, binds port 80 with `CAP_NET_BIND_SERVICE`)
- **LED**: boot LED service runs during startup, ready LED service restored after sync exits (via `ExecStopPost`)

## CI pipeline
`.github/workflows/go-ci.yml`: lint → test (with race detector) → build (arm6, arm64, amd64). `.github/workflows/release.yml` handles tag releases, including binaries, SD card image artifacts, and checksums. Dependabot updates Go deps and GitHub Actions weekly.

## Config defaults (from `internal/config/config.go`)
- Baud: 921600, chunk: 4096, erase timeout: 120s
- Erase after sync: true, flash read compression: false
- LED backend: sysfs, storage: `/mnt/logfalcon-logs`, min free: 200 MB
- Hotspot: SSID `LogFalcon`, password `fpvpilot`, web port 80
