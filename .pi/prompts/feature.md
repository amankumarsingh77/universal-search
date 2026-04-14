---
description: Full feature development workflow — research, design, spec, plan, implement, test, PR
---
Feature request: $@

## Your workflow

1. **Research** — read the relevant parts of the codebase first. Use context7 MCP or fetch official docs if you need to verify any library API. Do not rely on memory for Go APIs or Wails bindings.

2. **Propose approaches** — present 2–3 options with trade-offs. Wait for approval before proceeding.

3. **Write spec** — on approval, save to `docs/superpowers/specs/YYYY-MM-DD-<name>-design.md`. The spec must include a phases section. Each phase must declare:
   - `name`: short label
   - `description`: what it implements
   - `files`: glob patterns for files this phase owns (e.g. `internal/indexer/*.go`)
   - `dependsOn`: phase names that must complete first (empty array if none)

4. **Write plan** — break the spec into a step-by-step implementation plan saved to `docs/superpowers/plans/YYYY-MM-DD-<name>.md`.

5. **Implement** — when the plan is approved, type `/implement` to start the automated phase orchestrator.

## Project rules (non-negotiable)
- All Go dependencies must be pure Go, no CGO.
- Never add Co-Authored-By or AI attribution to commits.
- Never commit anything under `docs/` — specs and plans are local-only.
- Commit messages: natural human tone, concise, no emojis.
- Use `RETRIEVAL_DOCUMENT` task type when indexing, `RETRIEVAL_QUERY` for search queries.
- Never mix embeddings from different Gemini model versions.
- FFmpeg must be called as a subprocess, never via Go bindings.
- Before writing any Go code, verify API usage via context7 or official documentation.
