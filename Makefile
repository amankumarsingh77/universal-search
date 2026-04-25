.PHONY: dev build test test-unit test-integration test-e2e test-all test-race check-file-size lint lint-go lint-frontend fmt vet audit ship clean setup

# Development mode with hot reload
dev:
	wails dev -tags webkit2_41

# Production build
build:
	wails build -tags webkit2_41

# Run all tests
test:
	go test ./...

# Unit tests — default build, under 60s wall-clock
test-unit:
	go test -count=1 -timeout 60s ./...

# Integration tests — real tempdir SQLite + HNSW, real pipeline goroutines
test-integration:
	go test -count=1 -timeout 300s -tags integration ./...

# End-to-end tests — deterministic fixtures + FakeEmbedder
test-e2e:
	go test -count=1 -timeout 300s -tags e2e ./...

# Run all three test layers sequentially
test-all: test-unit test-integration test-e2e

# Test with race detector enabled
test-race:
	go test -race -count=1 -timeout 300s ./...

# REF-021: no .go file in internal/app/ may exceed 400 lines
check-file-size:
	@bash scripts/check-file-size.sh

# Run all linters (go vet + staticcheck + frontend eslint)
lint: lint-go lint-frontend

# Go linting using standalone tools (golangci-lint v1.64.8 is incompatible with Go 1.26).
# go vet catches: unused vars, shadowing, printf mismatches, nil derefs, race patterns, etc.
# staticcheck catches: unused code, style issues, performance patterns, subtle bugs.
# Configuration: staticcheck.conf — adds receiver naming, error formatting, doc comments.
lint-go:
	go vet ./...
	staticcheck -tests=false -go 1.26 ./...

# Frontend linting with ESLint
lint-frontend:
	cd frontend && npx eslint src/

# Auto-fix formatting issues
fmt:
	goimports -w .
	cd frontend && npx eslint src/ --fix

# Run go vet
vet:
	go vet ./...

# Concurrency and goroutine safety audit
audit:
	@echo "=== Goroutines ==="
	@rg -n "go\s+(func|\w+\()" internal/ --type go || true
	@echo "=== Channel buffer sizes (flag > 1) ==="
	@rg -n "make\(chan.*,\s*[2-9]\|[1-9][0-9]+" internal/ --type go || true
	@echo "=== Embedded mutexes ==="
	@rg -n "\bsync\.(RWMutex|Mutex)\b" internal/ --type go || true
	@echo "=== Context stored in structs ==="
	@rg -n "\bctx\b\s+context\.Context\b" internal/ --type go || true
	@echo "=== Audit complete ==="

# Full quality gate: fmt + vet + lint + test-race + build + file-size
ship: fmt vet lint test-race build check-file-size
	@echo "=== Ship gate passed ==="

# Install all dependencies
setup:
	go mod download
	cd frontend && npm install
	npm install

# Clean build artifacts
clean:
	rm -rf build/bin frontend/dist
