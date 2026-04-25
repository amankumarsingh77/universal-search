---
description: Run the full quality gate before shipping changes
agent: build
---

Run these quality checks sequentially. Stop on first failure.

1. **Format & auto-fix**: `golangci-lint run --fix ./... && cd frontend && npx eslint src/ --fix && cd ..`
2. **Vet**: `go vet ./...`
3. **Lint**: `make lint`
4. **Unit tests**: `go test -race -count=1 -timeout 60s ./...`
5. **Integration tests**: `go test -count=1 -timeout 300s -tags integration ./...`
6. **Build check**: `go build ./...`
7. **File size check**: `bash scripts/check-file-size.sh`

Report each step as PASS or FAIL. If all pass, summarize: "Ready to ship."
