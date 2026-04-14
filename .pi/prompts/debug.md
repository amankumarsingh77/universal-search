---
description: Debug an issue — reproduce, diagnose, fix, verify with tests
---
Issue: $@

## Your workflow

1. **Read** the failing code and any relevant tests.
2. **Reproduce** — run `go test -run <TestName> ./...` or the exact command that triggers the bug.
3. **Diagnose** — explain the root cause before touching any code.
4. **Fix** — implement the minimal change. Do not refactor surrounding code.
5. **Verify** — run `go test -race -count=1 ./...`. All tests must pass.
6. **Do not create a PR** — run `/review` first, then create the PR manually.

## Project rules (non-negotiable)
- All Go dependencies must be pure Go, no CGO.
- Never add Co-Authored-By or AI attribution to commits.
- Never commit anything under `docs/`.
- Commit messages: natural human tone, concise, no emojis.
