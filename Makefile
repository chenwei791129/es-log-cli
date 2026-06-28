BINARY := es-log
PKG := github.com/chenwei791129/es-log-cli
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X $(PKG)/internal/cmd.version=$(VERSION)

.PHONY: help build test lint fmt clean

.DEFAULT_GOAL := help

# help prints the available targets and their descriptions.
help:
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-8s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: ## Build a single static binary (CGO disabled)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/es-log

test: ## Run all tests
	go test ./...

lint: ## Run golangci-lint via the project-pinned tool dependency
	go tool golangci-lint run

fmt: ## Format all Go source files
	go fmt ./...

clean: ## Remove the built binary
	rm -f $(BINARY)
