---
description: Pre-PR code review — Go standards, pure-Go constraint, test coverage
---
Review all changes in the current branch against main.

## Checklist

Run `git diff main...HEAD` to see all changes, then check each modified Go file:

**Correctness**
- [ ] No ignored errors (every `err` is checked or explicitly discarded with `_` and a comment)
- [ ] No naked goroutines — every `go func()` has a corresponding `WaitGroup.Add` or cancel signal
- [ ] Context is propagated correctly — no `context.Background()` inside request paths
- [ ] No data races — concurrent access to shared state uses mutex or channels

**Style**
- [ ] Error strings are lowercase, no punctuation at end (Go convention)
- [ ] Errors are wrapped with `fmt.Errorf("...: %w", err)` not `fmt.Errorf("...: %v", err)`
- [ ] No `log.Printf` left in production paths — use `slog` at debug level

**Constraints**
- [ ] No CGO imports anywhere
- [ ] No new dependency added without verifying it is pure Go
- [ ] No files under `docs/` staged for commit

**Tests**
- [ ] Every new exported function has at least one test
- [ ] Tests are table-driven where there are multiple cases
- [ ] Run `go test -race -count=1 ./...` — all pass

Report findings as a numbered list. Mark each as CRITICAL (blocks PR), MAJOR (should fix), or MINOR (suggestion). If any CRITICAL issues exist, do not proceed to PR creation.
