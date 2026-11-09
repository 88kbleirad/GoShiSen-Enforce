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

.PHONY: all build test vet fmt fmt-fix check clean run tidy help test-integration fuzz

.PHONY: integration
integration: build ## Run the real netns integration suite (requires root)
	@[ "$$(id -u)" = 0 ] || { echo "integration tests require root (CAP_NET_ADMIN)"; exit 1; }
	$(GO) test ./tests/integration/ -v -count=1

fuzz: ## Fuzz the packet parser (30s)
	$(GO) test ./internal/packet/ -run='^$$' -fuzz=FuzzParse -fuzztime=30s

all: check

build: ## Build the daemon into ./bin/goshisen
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/goshisen
	@echo "built $(BIN) ($(VERSION))"

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

check: fmt vet test build ## Full local verification

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)

run: build ## Build and run the daemon with the sample config
	$(BIN) --config configs/goshisen.yaml

tidy: ## Tidy go.mod / go.sum
	$(GO) mod tidy

help: ## List targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  %-10s %s\n", $$1, $$2}'
