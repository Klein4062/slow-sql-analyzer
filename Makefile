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

test: ## Run unit tests (count=1 to bypass cache)
	go test -count=1 ./...

cover: ## Run tests with coverage; print per-package + total (target 90% on changed packages)
	go test -count=1 -coverprofile=coverage.out ./...
	@echo "=== 总覆盖率 ==="
	@go tool cover -func=coverage.out | tail -1
	@echo "（按包见上方各 ok 行；HTML: go tool cover -html=coverage.out）"

test-integration: ## Run live integration tests against PostgreSQL (set SSA_TEST_ADMIN_DSN)
	go test -tags=integration -cover ./internal/source/

cover-html: cover ## Open HTML coverage report
	go tool cover -html=coverage.out

ci: ## Run the same gates as GitHub Actions CI (gofmt + vet + build + test)
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt 需格式化:"; echo "$$out"; exit 1; fi
	go vet ./...
	go build ./...
	go test -count=1 -cover ./...

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
