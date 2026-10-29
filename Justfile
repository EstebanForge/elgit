default:
    @just --list

# Ensure VERSION file matches cli.Version constant
check-version:
    make check-version

# Build the binary to bin/elgit
build:
    make build

# Build optimized release binary
build-release:
    make build-release

# Format Go code
fmt:
    make fmt

# Run go vet
vet:
    make vet

# Run golangci-lint
lint:
    make lint

# Run tests
test:
    make test

# Run full checks (fmt, vet, lint, test, build)
check:
    make check

# Local CI verification gate
ci: check-version check
