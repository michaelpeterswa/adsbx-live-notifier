# adsbx-live-notifier — developer task runner
#
# Recipes are written for `just` (https://github.com/casey/just). On macOS:
# `brew install just`. On Linux: `cargo install just` or download a static
# binary from GitHub releases. Run `just` with no arguments for the list.

set shell := ["bash", "-euo", "pipefail", "-c"]

set dotenv-load := true
set dotenv-required := false

binary := "adsbx-live-notifier"
cmd_path := "./cmd/adsbx-live-notifier"

# Show available recipes.
default:
    @just --list

# --- build -----------------------------------------------------------------

# Build the binary into ./bin/.
build:
    mkdir -p bin
    go build -o bin/{{binary}} {{cmd_path}}

# Run the binary using the local environment.
run:
    go run {{cmd_path}}

# Tidy module files.
tidy:
    go mod tidy

# --- test ------------------------------------------------------------------

# Unit tests.
test:
    go test ./...

# Tests with race detector and coverage.
test-race:
    go test -race -coverprofile=coverage.out ./...

# --- lint ------------------------------------------------------------------

# go vet plus golangci-lint when available.
lint:
    go vet ./...
    if command -v golangci-lint >/dev/null 2>&1; then \
        golangci-lint run ./...; \
    else \
        echo "golangci-lint not installed; skipping"; \
    fi

# --- clean -----------------------------------------------------------------

clean:
    rm -rf bin coverage.out
