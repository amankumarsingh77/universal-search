---
description: Generate and run tests for a Go package
---
Package: $1

## Your workflow

1. Read all existing `*_test.go` files in `$1` to understand the testing patterns used.
2. Run `go test -v -race -count=1 $1` to see current coverage and failures.
3. For each exported function or method with missing or thin coverage, write table-driven tests following the project's existing test style.
4. Run `go test -v -race -count=1 -coverprofile=coverage.out $1` and report the coverage delta.
5. All new tests must pass with `-race`.

## Instructions for writing tests

Follow the table-driven pattern:
- Use a `tests` slice of anonymous structs with `name`, `input`, `want` fields
- Use `t.Run(tt.name, ...)` for subtests
- Name test functions `TestFunctionName`
- Use `t.Errorf` not `t.Fatalf` unless the test cannot continue
