---
description: Review code changes against Universal Search best practices
agent: go-review
subtask: true
---

Review all modified Go files (from git diff or staged changes) against the Universal Search best practices. Focus on:

1. Error handling — apperr wrapping, %w vs %v, no log+return
2. Concurrency safety — goroutine lifecycle, channel sizes, mutex usage
3. Context propagation — first parameter, never in structs
4. Naming conventions — MixedCaps, initialisms, package names
5. Interface placement — consumer-side definition
6. File organization — concern-based grouping, under 400 lines
7. Missing doc comments on exported symbols
8. Test patterns — table-driven, t.Helper, t.Run
9. Performance — preallocation, strconv over fmt, strings.Builder

Report each violation with file:line, the rule broken, and a suggested fix.
