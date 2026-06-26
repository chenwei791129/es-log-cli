BINARY := es-log
PKG := github.com/chenwei791129/es-log-cli
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X $(PKG)/internal/cmd.version=$(VERSION)

.PHONY: build test lint fmt clean

# build produces a single static binary (CGO disabled).
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/es-log

test:
	go test ./...

# lint runs golangci-lint via the project-pinned tool dependency.
lint:
	go tool golangci-lint run

fmt:
	go fmt ./...

clean:
	rm -f $(BINARY)
