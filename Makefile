# Diane — Development Makefile
# First time? Run: make setup

SERVER_DIR := server
CMD_DIR    := $(SERVER_DIR)/cmd/diane
GO         := go
GOFLAGS    :=

.PHONY: all setup test test-all test-quick build install lint vet mod-tidy check clean

all: check

# ── Setup ──────────────────────────────────────────────────────────────────

setup:
	git config core.hooksPath .githooks
	@echo "✅ Git hooks installed at .githooks/"
	@echo ""
	@echo "Next steps:"
	@echo "  make test     — run unit tests"
	@echo "  make build    — build the binary"
	@echo "  make install  — install to ~/.diane/bin/"
	@echo "  make check    — vet + lint + test (full gate)"

# ── Testing ────────────────────────────────────────────────────────────────

## Fast unit tests (no external deps)
test:
	cd $(SERVER_DIR) && $(GO) test -count=1 $(GOFLAGS) ./internal/... ./cmd/...

## All tests including integration (needs live MP credentials)
test-all:
	cd $(SERVER_DIR) && $(GO) test -count=1 $(GOFLAGS) ./...

## Quick: run only tests matching a pattern
test-quick:
	cd $(SERVER_DIR) && $(GO) test -count=1 -run '$(filter-out $@,$(MAKECMDGOALS))' $(GOFLAGS) ./internal/... ./cmd/...

# ── Build ──────────────────────────────────────────────────────────────────

build:
	cd $(CMD_DIR) && $(GO) build $(GOFLAGS) -o ../../../diane .

build-race:
	cd $(CMD_DIR) && $(GO) build -race $(GOFLAGS) -o ../../../diane .

install: build
	@mkdir -p ~/.diane/bin
	cp diane ~/.diane/bin/diane
	@echo "✅ Installed to ~/.diane/bin/diane"
	@echo "   Ensure ~/.diane/bin is in your PATH"

# ── Linting & Static Analysis ──────────────────────────────────────────────

vet:
	cd $(SERVER_DIR) && $(GO) vet $(GOFLAGS) ./internal/... ./cmd/...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "❌ golangci-lint not installed. Install: brew install golangci-lint"; \
		exit 1; \
	}
	cd $(SERVER_DIR) && golangci-lint run ./internal/... ./cmd/...

lint-fix:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "❌ golangci-lint not installed. Install: brew install golangci-lint"; \
		exit 1; \
	}
	cd $(SERVER_DIR) && golangci-lint run --fix ./internal/... ./cmd/...

# ── Swift Companion App ──────────────────────────────────────────────────────

## Build (and test) the Swift companion app
check-swift:
	@if ! command -v xcodegen &>/dev/null; then echo "❌ xcodegen not found. Install: brew install xcodegen"; exit 1; fi
	cd server/swift/DianeCompanion && xcodegen generate --quiet
	cd server/swift/DianeCompanion && xcodebuild \
		-project Diane.xcodeproj \
		-scheme Diane \
		-configuration Debug \
		-derivedDataPath build/DerivedData \
		CODE_SIGN_IDENTITY="" \
		CODE_SIGNING_REQUIRED=NO \
		CODE_SIGNING_ALLOWED=NO \
		SWIFT_ACTIVE_COMPILATION_CONDITIONS= \
		build 2>&1 | tail -5
	@echo "✅ Swift companion app builds cleanly."

# ── Full Gate ──────────────────────────────────────────────────────────────

mod-tidy:
	cd $(SERVER_DIR) && $(GO) mod tidy
	@echo "✅ go mod tidy done"

check: mod-tidy vet lint test
	@echo ""
	@echo "✅ All checks passed!"

# ── Cleanup ────────────────────────────────────────────────────────────────

clean:
	rm -f diane
	cd $(SERVER_DIR) && $(GO) clean -cache

# ── Passthrough for test-quick args ────────────────────────────────────────
%:
	@:
