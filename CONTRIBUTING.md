# Contributing

Keep LogFalcon focused on reliable Betaflight SPI-flash collection. iNav copying
is disabled pending separate protocol and hardware evidence. Discuss broader
scope before implementing it. See [architecture](docs/ARCHITECTURE.md).

Use Go **1.27.3** (the currently tested patch), Linux or macOS. `go.mod` requires
Go 1.27; older toolchains are not supported because storage uses `os.Root`.
The application needs no Python or frontend build. Installer integration runs
with GNU sed on Linux; the macOS fixture adapts BSD sed. The image builder below
provides Linux tools inside Docker on either host.

```sh
go mod download
go test -race -coverprofile=cover.out -covermode=atomic ./...
go vet ./...
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
golangci-lint run ./...
go test ./internal/msp -run='^$' -fuzz=FuzzFrameDecoder -fuzztime=20s -parallel=2
go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
govulncheck ./...
make build
make build-pi
make build-pi2
GOOS=linux GOARCH=amd64 go build -o bin/logfalcon-amd64 ./cmd/logfalcon
shellcheck install.sh scripts/install.sh system/config-apply.sh system/wifi-init.sh
```

Run `gofmt` on changed Go files. The linter uses its default configuration. Update
Go/CI/Docker/tool versions together and run the checks; a newer minimum version
must not silently outrun the tested linter. Dependabot covers Go, Actions and
Docker inputs. APT/image inputs still vary; see [provenance](docs/PROVENANCE.md).

For a fix, reproduce the failure and add a test for observable behaviour or an
important failure boundary. Preserve controller data on every failure path.
Keep unrelated work intact. Describe checks actually run and explicitly mark
hardware tested/not tested in the PR. Include the exact board, firmware, image
checksum and results when claiming compatibility. Never include passwords, full
config files, private controller dumps or unreviewed diagnostic bundles in an
issue. See [SECURITY.md](SECURITY.md) for sensitive reports.

To build the complete SD image on macOS (including Apple Silicon) or Linux,
install/start Docker, then run from this checkout:

```sh
bash pi-gen/build.sh
```

No host Go, Python, QEMU or Linux VM setup is required. The script prepares a
pinned Linux/Go tool image, checks ARM emulation and mounts, runs race tests,
vet and govulncheck, builds the Pi binaries, builds Raspberry Pi OS, shrinks and
inspects the image, compresses it and writes SHA-256 checksums. The image is
32-bit Bookworm with the ARM6 binary, suitable for evaluation on either Pi target.

Use `bash pi-gen/build.sh --check` to exercise the toolchain/source checks and
binary builds without the long OS-image build. Outputs are in
`pi-gen/deploy/<run>/`: `.img.xz`, package `.info`, `checksums.txt`, source hashes,
build identity and `build.log`. Each run has a new folder and temporary Docker
work volume; existing images and source changes are preserved. Tool-image layers
are cached. Temporary work/container resources are removed on exit.

The builder requires privileged Docker for ARM binfmt registration, loop devices
and filesystem mounts. It mounts this checkout read-only and writes to its own
output folder. Locked-down Docker installations may reject those operations;
`--check` detects this early. It does not flash cards, modify host networking or
publish a release. Interrupted/failed builds retain their log, without claiming
a finished image.

Image inspection and cross-compilation do not establish hardware reliability.
Use [DEVELOPMENT_TESTING.md](DEVELOPMENT_TESTING.md) for physical acceptance and
[AI-assisted development](docs/AI_ASSISTED_DEVELOPMENT.md) for how assistance is
recorded without inventing human review or validation.

Signed-update tests use isolated temporary Ed25519 keys and a small cross-compiled
Go executable to exercise real ELF/build identity checks. Historical migration
fixtures are original tag configurations, with their limits in
[OFFLINE_UPDATES.md](docs/OFFLINE_UPDATES.md). Run all package tests on Linux CI;
a macOS fixture is not native Linux installer evidence. Keep format/migrations
and cumulative trust delegations compatible with appliances that skip releases.
Never put production private signing material in this checkout or a test fixture.
