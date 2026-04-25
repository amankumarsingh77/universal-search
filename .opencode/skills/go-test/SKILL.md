---
name: go-test
description: Run Go tests with race detection for Universal Search
---

## Steps

1. Run `go test -race -count=1 -timeout 300s ./...`
2. If any test fails, analyze the failure:
   - Read the test file and the code under test
   - Identify whether it's a real bug or a test issue
   - Fix the issue following project conventions
3. Pay special attention to data race reports — they indicate real concurrency bugs that must be fixed.
4. Re-run tests until all pass.
5. Report summary: pass/fail count, race status, any flaky tests observed.
