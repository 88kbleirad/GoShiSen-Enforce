# GoShiSen-Enforce build and verification targets.
#
#   make build   compile ./bin/goshisen with version metadata
#   make test    run all tests
#   make vet     run go vet
#   make fmt     check formatting (gofmt -l); fails if any file is unformatted
#   make check   fmt + vet + test + build
#   make clean   remove build artifacts

GO      ?= go
GOFLAGS ?=
BIN_DIR := bin
BIN     := $(BIN_DIR)/goshisen
PKG     := github.com/88kbleirad/GoShiSen-Enforce

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w \
	-X $(PKG)/internal/cli.Version=$(VERSION) \
	-X $(PKG)/internal/app.Version=$(VERSION)

.PHONY: all build build-static verify-static test vet fmt fmt-fix check clean run tidy help test-integration fuzz \
	package package-dist verify-package sbom integration preflight-integration

.PHONY: integration
integration: build ## Run the real netns integration suite (requires root)
	@[ "$$(id -u)" = 0 ] || { echo "integration tests require root (CAP_NET_ADMIN)"; exit 1; }
	$(GO) test ./tests/integration/ -v -count=1

# preflight-integration runs the real preflight gate against this host
# (tools, privileges, kernel modules, ports). It is the separate "real
# environment" target that must not be confused with the hermetic unit
# tests (which never observe the host). Requires root only so a fully
# provisioned runner reaches READY; otherwise it reports the exact
# missing dependency instead of skipping.
.PHONY: preflight-integration
preflight-integration: ## Run the real preflight against this host (clear diagnostics when tools are missing)
	@[ "$$(id -u)" = 0 ] || { echo "preflight-integration requires root (privileged runner)"; exit 1; }
	$(GO) test -tags preflight_integration ./tests/integration/ -run '^TestPreflightReal$$' -v -count=1

fuzz: ## Fuzz the packet parser (30s)
	$(GO) test ./internal/packet/ -run='^$$' -fuzz='^FuzzParse$$' -fuzztime=30s
	$(GO) test ./internal/packet/ -run='^$$' -fuzz='^FuzzParseAttrs$$' -fuzztime=30s
all: check

# The default release build is a statically linked, reproducible binary
# (CGO off, trimmed paths, no VCS stamp): this is what ships in release
# archives and on OpenWrt (musl). See scripts/package.sh.
build: ## Build the daemon into ./bin/goshisen (static)
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false $(GOFLAGS) \
		-ldflags '-buildid= $(LDFLAGS)' -o $(BIN) ./cmd/goshisen
	@echo "built $(BIN) ($(VERSION))"

build-static: build ## Alias: the default build is already static

verify-static: ## Fail if $(BIN) is not a statically linked executable
	@[ -f "$(BIN)" ] || { echo "missing $(BIN) (run: make build)"; exit 1; }
	@file "$(BIN)" | grep -q "statically linked" || { \
		echo "ERROR: $(BIN) is not statically linked"; \
		file "$(BIN)"; exit 1; }
	@echo "static: ok"

test: ## Run tests
	$(GO) test $(GOFLAGS) ./...

vet: ## Run go vet
	$(GO) vet $(GOFLAGS) ./...

fmt: ## Verify formatting; fails on unformatted files
	@unformatted=$$(gofmt -l . | grep -v '^$$' || true); \
	if [ -n "$$unformatted" ]; then \
		echo "unformatted files (run 'make fmt-fix'):"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	@echo "formatting ok"

fmt-fix: ## Rewrite files with gofmt
	gofmt -w .

check: fmt vet test build verify-static ## Full local verification

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)

run: build ## Build and run the daemon with the sample config
	$(BIN) --config configs/goshisen.yaml

tidy: ## Tidy go.mod / go.sum
	$(GO) mod tidy

package: ## Build reproducible release archives (amd64 + arm64)
	VERSION=$(VERSION) ARCH=all bash scripts/package.sh

package-dist: ## Build only the current GOARCH archive
	VERSION=$(VERSION) bash scripts/package.sh

verify-package: package-dist ## Build and validate the archive (arch, version, checksums, SBOM)
	@bash scripts/verify-package.sh

sign-test: ## Sign dist/ with a throwaway test key (verification exercise)
	@test -d dist || { echo "run 'make package' first"; exit 1; }
	@tmp=$$(mktemp -d); \
	openssl genpkey -algorithm ed25519 -out $$tmp/k.pem 2>/dev/null; \
	openssl pkey -in $$tmp/k.pem -pubout -out $$tmp/k.pub.pem 2>/dev/null; \
	bash scripts/sign-artifacts.sh --dist dist --key $$tmp/k.pem --public-key $$tmp/k.pub.pem \
		--commit $$(git rev-parse HEAD 2>/dev/null || echo unknown) --tag dev \
		--builder local --toolchain "$$(go version)" --sign; \
	bash scripts/verify-signatures.sh --dist dist --public-key $$tmp/k.pub.pem \
		--commit $$(git rev-parse HEAD 2>/dev/null || echo unknown) --require-sign; \
	rm -rf $$tmp

verify-signatures: ## Verify dist/ with the committed production public key
	@test -f keys/goshisen-release.pub.pem || { echo "no keys/goshisen-release.pub.pem"; exit 1; }
	@bash scripts/verify-signatures.sh --dist dist --public-key keys/goshisen-release.pub.pem --require-sign

sbom: ## Generate an SPDX SBOM over the source tree
	@syft scan -o spdx-json=sbom.spdx.json . 2>/dev/null || \
		echo "ERROR: syft not installed (see scripts/package.sh)" >&2

help: ## List targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  %-10s %s\n", $$1, $$2}'
