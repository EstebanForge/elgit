VERSION := $(shell cat VERSION)
VERSION_CONST := $(shell grep '^var Version' internal/cli/root.go | sed 's/.*"\(.*\)".*/\1/')
BINARY_NAME := elgit
BUILD_DIR := bin
BINARY_PATH := $(BUILD_DIR)/$(BINARY_NAME)
DIST_DIR := dist

GOCMD := go
GOLANGCI_LINT := golangci-lint
LINT_TIMEOUT ?= 5m

LDFLAGS := -ldflags "-s -w"
SIGN_IDENTITY ?= -

.PHONY: help check-version build build-release sign build-signed test test-ci \
	fmt vet lint lint-ci check cross-compile release version clean

.DEFAULT_GOAL := help

help: ## Show this help message
	@echo "elgit - Build System"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-22s %s\n", $$1, $$2}'

check-version: ## Ensure VERSION file matches cli.Version
	@if [ "$(VERSION)" != "$(VERSION_CONST)" ]; then \
		echo "Version mismatch: VERSION=$(VERSION) internal/cli/root.go=$(VERSION_CONST)"; \
		exit 1; \
	fi

build: ## Build the binary
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOCMD) build $(LDFLAGS) -o $(BINARY_PATH) ./cmd/elgit
	@echo "✓ Built: $(BINARY_PATH)"

build-release: ## Build optimized release binary for current platform
	@echo "Building release binary..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GOCMD) build $(LDFLAGS) -trimpath -o $(BINARY_PATH) ./cmd/elgit
	@echo "✓ Release binary built: $(BINARY_PATH)"

sign: build ## Ad-hoc sign binary on macOS
	@if [ "$$(uname)" = "Darwin" ]; then \
		codesign --force -s "$(SIGN_IDENTITY)" $(BINARY_PATH); \
		xattr -d com.apple.quarantine $(BINARY_PATH) 2>/dev/null || true; \
		echo "✓ Signed: $(BINARY_PATH)"; \
	else \
		echo "ℹ️  Skipping sign (non-macOS)"; \
	fi

build-signed: build sign ## Build, sign, and install to ~/.local/bin (local testing)
	@mkdir -p ~/.local/bin
	@cp $(BINARY_PATH) ~/.local/bin/$(BINARY_NAME)
	@if [ "$$(uname)" = "Darwin" ]; then \
		codesign --force -s "$(SIGN_IDENTITY)" ~/.local/bin/$(BINARY_NAME); \
		xattr -d com.apple.quarantine ~/.local/bin/$(BINARY_NAME) 2>/dev/null || true; \
	fi
	@echo "✓ Installed: ~/.local/bin/$(BINARY_NAME)"

test: ## Run tests (deps + verify + go test -race)
	@echo "Downloading dependencies..."
	$(GOCMD) mod download
	@echo "Verifying dependencies..."
	$(GOCMD) mod verify
	@echo "Running tests..."
	$(GOCMD) test -race ./...
	@echo "✓ Tests passed"

test-ci: ## Run tests in CI (quiet)
	@$(MAKE) --no-print-directory test

fmt: ## Format Go code
	@echo "Formatting code..."
	$(GOCMD) fmt ./...
	@echo "✓ Code formatted"

vet: ## Run go vet
	@echo "Running go vet..."
	$(GOCMD) vet ./...
	@echo "✓ Vet passed"

lint: ## Run golangci-lint
	@command -v $(GOLANGCI_LINT) >/dev/null 2>&1 || (echo "$(GOLANGCI_LINT) not installed. Install with: brew install golangci-lint" && exit 1)
	@echo "Running golangci-lint..."
	$(GOLANGCI_LINT) run --timeout=$(LINT_TIMEOUT)
	@echo "✓ Linting complete"

lint-ci: ## Run linter in CI parity mode (clears cache first)
	@$(GOLANGCI_LINT) cache clean
	@$(MAKE) --no-print-directory lint

check: ## Run full checks (fmt, vet, lint, test, build)
	@echo "==> make fmt"
	@$(MAKE) --no-print-directory fmt
	@echo "==> make vet"
	@$(MAKE) --no-print-directory vet
	@echo "==> make lint"
	@$(MAKE) --no-print-directory lint
	@echo "==> make test"
	@$(MAKE) --no-print-directory test
	@echo "==> make build"
	@$(MAKE) --no-print-directory build
	@echo "✓ Full checks complete"

cross-compile: ## Build for all platforms (pure Go, no cgo)
	@echo "Cross-compiling for all platforms..."
	@mkdir -p $(DIST_DIR)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GOCMD) build $(LDFLAGS) -trimpath -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/elgit
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GOCMD) build $(LDFLAGS) -trimpath -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/elgit
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GOCMD) build $(LDFLAGS) -trimpath -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/elgit
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GOCMD) build $(LDFLAGS) -trimpath -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/elgit
	@echo "✓ Cross-compilation complete"
	@ls -lh $(DIST_DIR)/

release: check-version clean test cross-compile ## Create release artifacts in dist/
	@echo "Packaging release $(VERSION)..."
	@cd $(DIST_DIR) && \
		tar -czf $(BINARY_NAME)-darwin-arm64-$(VERSION).tar.gz $(BINARY_NAME)-darwin-arm64 && \
		tar -czf $(BINARY_NAME)-darwin-amd64-$(VERSION).tar.gz $(BINARY_NAME)-darwin-amd64 && \
		tar -czf $(BINARY_NAME)-linux-amd64-$(VERSION).tar.gz $(BINARY_NAME)-linux-amd64 && \
		tar -czf $(BINARY_NAME)-linux-arm64-$(VERSION).tar.gz $(BINARY_NAME)-linux-arm64
	@if command -v shasum >/dev/null 2>&1; then \
		cd $(DIST_DIR) && shasum -a 256 *.tar.gz > checksums.txt; \
	else \
		cd $(DIST_DIR) && sha256sum *.tar.gz > checksums.txt; \
	fi
	@echo "✓ Release $(VERSION) ready in $(DIST_DIR)/"
	@ls -lh $(DIST_DIR)/*.tar.gz
	@cat $(DIST_DIR)/checksums.txt

version: ## Show version
	@echo "$(BINARY_NAME) version $(VERSION) (source: cli.Version=$(VERSION_CONST))"

clean: ## Clean build artifacts
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR) $(DIST_DIR)
	@echo "✓ Cleaned"
