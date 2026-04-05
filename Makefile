.PHONY: dev build test lint lint-go lint-frontend fmt clean

# Development mode with hot reload
dev:
	wails dev -tags webkit2_41

# Production build
build:
	wails build -tags webkit2_41

# Run all tests
test:
	go test ./...

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
