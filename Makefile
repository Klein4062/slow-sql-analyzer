# slow-sql-analyzer — build & release helpers.
#
# Produces a single, fully-static, self-contained binary that can be copied to
# an intranet machine with ZERO runtime dependencies (no Go, no libpq, no psql).
#
# Common targets:
#   make build         # current platform binary -> dist/slow-sql-analyzer
#   make build-all     # cross-compile for all targets -> dist/
#   make test          # go test ./...
#   make vendor        # vendor deps (for air-gapped/offline builds)
#   make clean

BINARY   := slow-sql-analyzer
MODULE   := github.com/Klein4062/slow-sql-analyzer
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DIST     := dist

# Tiny, stripped, reproducible binaries. CGO_ENABLED=0 = pure-Go static build
# (pgx has no cgo), so the binary needs nothing from the target OS.
CGO      := CGO_ENABLED=0
LDFLAGS  := -trimpath -ldflags "-s -w -X $(MODULE)/internal/cli.Version=$(VERSION)"

# Cross-compile targets: os/arch pairs.
TARGETS := \
	linux/amd64 linux/arm64 \
	darwin/amd64 darwin/arm64 \
	windows/amd64 windows/arm64

.PHONY: all build build-all test vet fmt clean vendor help

all: build

build: ## Build for the current platform
	mkdir -p $(DIST)
	$(CGO) go build $(LDFLAGS) -o $(DIST)/$(BINARY) ./cmd/slow-sql-analyzer
	@echo "built $(DIST)/$(BINARY) ($$($(DIST)/$(BINARY) version))"

build-all: ## Cross-compile static binaries for every target into dist/
	mkdir -p $(DIST)
	@set -e; \
	for target in $(TARGETS); do \
		os=$${target%/*}; arch=$${target#*/}; \
		out=$(DIST)/$(BINARY)-$$os-$$arch; \
		[ $$os = windows ] && out=$$out.exe; \
		echo "==> $$os/$$arch -> $$out"; \
		$(CGO) GOOS=$$os GOARCH=$$arch go build $(LDFLAGS) -o $$out ./cmd/slow-sql-analyzer; \
	done
	@cd $(DIST) && ls -lh

test: ## Run unit tests
	go test ./...

vet: ## go vet
	go vet ./...

fmt: ## gofmt
	gofmt -w .

vendor: ## Vendor dependencies (for offline / air-gapped builds)
	go mod vendor

clean: ## Remove build artifacts
	rm -rf $(DIST)

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  %-12s %s\n",$$1,$$2}'
