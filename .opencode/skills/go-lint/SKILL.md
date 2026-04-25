---
name: go-lint
description: Run Go linting and fix issues for Universal Search
---

## Steps

1. Run `golangci-lint run ./...` to check for issues.
2. If auto-fixable issues exist, run `golangci-lint run --fix ./...`.
3. For remaining issues, fix them manually following Universal Search conventions.
4. Re-run `golangci-lint run ./...` to verify clean.
5. Run `go vet ./...` as a final sanity check.
6. Report the final status: clean, number of fixed issues, or issues requiring manual intervention.
