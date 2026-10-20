# Contributing

Keep LogFalcon focused on reliable Betaflight SPI-flash collection. iNav copying
is disabled pending separate protocol and hardware evidence. Discuss broader
scope before implementing it. See [architecture](docs/ARCHITECTURE.md).

Use Go **1.27.3** (the currently tested patch), Linux or macOS. `go.mod` requires
Go 1.27; older toolchains are not supported because storage uses `os.Root`.
The application needs no Python or frontend build. Linux is required for native installer integration and the image build. The
local installer fixture uses a BSD sed adapter on macOS; Linux CI runs GNU sed.

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

Linux + Docker can build the image with `cd pi-gen && bash build.sh`. Image
inspection and cross-compilation do not establish hardware reliability. Use
[DEVELOPMENT_TESTING.md](DEVELOPMENT_TESTING.md) for physical acceptance and
[AI-assisted development](docs/AI_ASSISTED_DEVELOPMENT.md) for how assistance is
recorded without inventing human review or validation.
