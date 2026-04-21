.PHONY: dev build test test-unit test-integration test-e2e test-all check-file-size lint lint-go lint-frontend fmt clean

# Development mode with hot reload
dev:
	wails dev -tags webkit2_41

# Production build
build:
	wails build -tags webkit2_41

# Run all tests
test:
	go test ./...

# Unit tests — default build, under 30s wall-clock
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

# REF-021: no .go file in internal/app/ may exceed 400 lines
check-file-size:
	@bash scripts/check-file-size.sh

# Run all linters
lint: lint-go lint-frontend

# Go linting with golangci-lint
lint-go:
	golangci-lint run ./...

# Frontend linting with ESLint
lint-frontend:
	cd frontend && npx eslint src/

# Auto-fix lint issues
fmt:
	golangci-lint run --fix ./...
	cd frontend && npx eslint src/ --fix

# Run go vet
vet:
	go vet ./...

# Install all dependencies
setup:
	go mod download
	cd frontend && npm install
	npm install

# Clean build artifacts
clean:
	rm -rf build/bin frontend/dist
