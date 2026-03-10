VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -s -w -X main.Version=$(VERSION) -X main.BuildCommit=$(COMMIT)

.PHONY: build build-pi build-pi2 test lint clean

build:
	go build -ldflags="$(LDFLAGS)" -o bin/logfalcon ./cmd/logfalcon

build-pi:
	GOOS=linux GOARCH=arm GOARM=6 go build -ldflags="$(LDFLAGS)" -o bin/logfalcon-arm6 ./cmd/logfalcon

build-pi2:
	GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/logfalcon-arm64 ./cmd/logfalcon

test:
	go test -race -v -cover ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/
