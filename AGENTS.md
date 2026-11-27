# LogFalcon — Agent Guide

## Go project — not Python
The application and maintainer tools use Go under `cmd/` and `internal/`. The module `github.com/proeugene/logfalcon` requires **Go 1.27 (tested patch 1.27.4)**. The Pi image also ships shell helpers and systemd units. Do not reintroduce Python runtime/build dependencies.

## Entrypoint
The appliance runs [one executable](cmd/logfalcon/main.go) in separate processes. Use `--web` for the dashboard and `--port /dev/ttyACM0` for serial collection. Service entry points also handle diagnostics, LED status, boot configuration, upgrade validation and privileged update recovery. Check the flag definitions and the calling [systemd units](system/) before changing behaviour. Build flags supply `main.Version` and `main.BuildCommit`.

[cmd/update-package](cmd/update-package/main.go) is the maintainer/CI tool for signed offline updates. See [the update format and recovery procedure](docs/OFFLINE_UPDATES.md).

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

## Architecture
Start with [the process map and copy/recovery ordering](docs/ARCHITECTURE.md). Follow a collection from `internal/fc` and `internal/msp` through `internal/sync` to `internal/storage`. The web and LED services read the shared status file. Browser and boot changes use `internal/settings`; signed packages and activation use `internal/update`.

## Web UI quirks
- Templates and client-side behaviour live in Go files under `internal/web/`. Route registration is in [server.go](internal/web/server.go). The dashboard polls `/status`; `/events` also exposes an SSE stream.
- For stylesheet locations, logo sources and the shared screenshot, see [Dashboard and website visuals](docs/ARCHITECTURE.md#dashboard-and-website-visuals).
- Mutating browser routes require the server's CSRF token. The local hotspot is the access boundary.
- Captive portal paths respond at `/hotspot-detect.html`, `/generate_204`, `/ncsi.txt`, etc. (iOS/Android/Windows compatibility).
- Health endpoint at `GET /health` returns JSON with storage, hotspot, and shared sync status.

## SD card image build
Uses [pi-gen](https://github.com/RPi-Distro/pi-gen) in Docker. Run `bash pi-gen/build.sh` from the repository root (requires Docker on macOS or Linux). The wrapper provides Linux tools, source checks, image shrinking/inspection, compression and checksums; `--check` stops before the OS-image build. Release CI builds and uploads images on `v*` tags via `.github/workflows/release.yml`.

## System integration
- **udev**: `/etc/udev/rules.d/99-betaflight-fc.rules` triggers `logfalcon@ttyACM*.service` on FC plug-in
- **systemd**: `logfalcon@.service` (oneshot sync, triggered by udev), `logfalcon-web.service` (always-running web server, binds port 80 with `CAP_NET_BIND_SERVICE`)
- **LED**: boot LED service runs during startup, dedicated status LED service owns sysfs and distinguishes success/error; async diagnostics export runs after sync

## CI pipeline
[Go CI](.github/workflows/go-ci.yml) runs lint, tests and cross-builds as separate jobs. [Security](.github/workflows/security.yml) runs govulncheck and CodeQL. [Release](.github/workflows/release.yml) builds the tag's binaries, SD image and checksums.

## Config defaults (from `internal/config/config.go`)
- Baud: 921600, chunk: 4096, erase timeout: 120s
- Erase after sync: false; storage-pressure cleanup: false, flash read compression: false
- LED backend: sysfs, storage: `/mnt/logfalcon-logs`, min free: 200 MB
- Hotspot: SSID `LogFalcon`, password `fpvpilot`, web port 80; Wi-Fi country `GB` (UK)

## Diagnostics and acceptance
- New images default to UK English, UK keyboard and Europe/London timezone.
- Persistent journald: approximately 50 MiB/seven days. Debug metadata is enabled by default (`DEBUG=true`); use `DEBUG=false` to disable it.
- `--diagnostics start` and `stop` record lifecycle events. The export service runs `--diagnostics export` to write bounded, redacted `LOGFALCON-DIAGNOSTICS/latest.json` on the mounted FAT boot partition.
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
- The boot helper owns the ACT LED during startup. The status service stops it before taking ownership. Missing status means waiting; corrupt or stale status cannot imply completion.
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
