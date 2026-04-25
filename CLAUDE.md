# CLAUDE.md

Guidance for Claude Code when working in this repository.

## Project

Universal Search — a local-first, cross-platform desktop app that indexes files on disk using Gemini Embedding 2 and provides instant semantic search via a global keyboard shortcut. Built with Wails v2 (Go backend + React frontend).

Design spec: `docs/superpowers/specs/2026-03-27-universal-search-design.md` (local-only, not committed).

## Tech Stack

- **Desktop framework:** Wails v2
- **Frontend:** React + TypeScript
- **Backend:** Go
- **Vector DB:** TFMV/hnsw (pure Go)
- **Metadata DB:** SQLite via ncruces/go-sqlite3 (pure Go, no CGO)
- **Embeddings:** Gemini Embedding 2 Preview (`gemini-embedding-2-preview`, 768-dim via MRL)
- **NL query parsing:** Gemini 2.5 Flash-Lite
- **Video processing:** ffmpeg (subprocess)
- **File watching:** fsnotify

## Architecture

Three layers:

1. **React frontend** (`frontend/`) — Raycast-style floating window: results list + preview panel, collapsible indexing progress bar, filter chips for structured query constraints, failure drill-down modal, error banner / warning chip for query-pipeline outcomes.
2. **Go backend** (`internal/`) — File watcher, indexing pipeline (classify → extract/chunk → embed → store) with concurrent workers and a shared rate limiter, NL query pipeline (grammar + optional LLM parse + cache), search engine (planner → rerank → relaxation → filename fallback), system tray.
3. **Storage** — SQLite is the source of truth for file metadata, indexed folders, excluded patterns, settings, and the parsed-query cache. HNSW holds 768-dim vectors; each chunk's vector is also mirrored inline in SQLite for brute-force fallback on small corpora.

Video pipeline follows sentrysearch patterns: 30s chunks with 5s overlap, 480p/5fps preprocessing, still-frame detection, direct video-to-embedding (no captioning).

## Backend layout

Wails-bound `App` lives in `internal/app/`, split by concern across files kept under 400 lines (enforced by `scripts/check-file-size.sh`). Supporting packages under `internal/`:

- `indexer/` — pipeline, reconcile, startup rescan, retry coordinator, failure registry
- `embedder/` — Gemini + Fake + RateLimiter (the only implementations shipped)
- `search/` — planner, rerank, relaxation, filename fallback
- `query/` — grammar, LLM parse, date normalization, typo correction, merge, cache
- `store/` — SQLite + schema migrator (`store/migrations/`)
- `vectorstore/` — HNSW wrapper
- `chunker/`, `watcher/`, `desktop/` (hotkey + tray), `platform/`, `logger/`
- `config/` — TOML loader; defaults in `config/defaults.toml`
- `apperr/` — stable error-code vocabulary the frontend maps to user-facing messages

Read the source when you need method-level detail; don't rely on this file to enumerate APIs.

## Customization

Runtime tuning lives in `config.toml` — indexing concurrency, embedder batch size and rate limits, HNSW parameters, search thresholds, NL-query timeouts. Loaded from `~/.config/universal-search/config.toml` on Linux/macOS, `%APPDATA%\universal-search\config.toml` on Windows. Missing keys fall back to `internal/config/defaults.toml` — consult that file for the full set of tunables.

Error codes surfaced across the Wails boundary are defined in `internal/apperr/`; the frontend translates them via `frontend/src/lib/errorLabels.ts`. Never pattern-match on error messages — use codes.

## Go Coding Standards

### Error Handling

- Use `apperr.Error` for all application errors. Never `errors.New()` for user-facing errors.
- Wrap with `apperr.Wrap(code, message, cause)`. Frontend maps code to user-facing message via `frontend/src/lib/errorLabels.ts`.
- Use `%w` in `fmt.Errorf` when callers should inspect the cause with `errors.Is`/`errors.As`. Use `%v` to obfuscate.
- Never log AND return the same error. Choose one: log and degrade, or wrap and return.
- Handle errors first (early return). Normal code stays at minimal indentation. No `else` after error returns.
- Error strings lowercase, no trailing punctuation.
- Never pattern-match on error messages — use `appErr.Code`, `errors.Is`, or `errors.As`.

### Concurrency

- Every goroutine must have a predictable stop mechanism: `context.Context` cancellation, `errgroup.Group`, or done channel.
- Channel buffer size: 0 (unbuffered) or 1. Larger needs a comment explaining why.
- Use `chan struct{}` for signaling, not `chan bool`.
- Mutexes: named fields (`mu sync.RWMutex`), never embedded in structs.
- Copy slices/maps at goroutine boundaries to prevent data races.
- Zero-value mutexes are valid: `var mu sync.Mutex`. Don't use `new()`.

### Context

- `context.Context` as first parameter: `func F(ctx context.Context, ...)`.
- Never store `context.Context` in struct fields. Pass as parameter to every method.
- Exception: methods implementing standard library interfaces (e.g., `io.Reader`).

### Naming

- MixedCaps: `MaxLength` (exported), `maxLength` (unexported). Never underscores.
- Initialisms: `URL`, `HTTP`, `ID` uppercase throughout (`ServeHTTP`, `userID`, `ModelID`).
- Getters: `Owner()` not `GetOwner()`.
- Interfaces: `-er` suffix for single-method: `Reader`, `Writer`, `Embedder`.
- Receiver names: 1-2 letters, consistent. Never `this`, `self`, `me`.
- Package names: lowercase, single word. Never `util`, `common`, `helpers`.

### Structure

- Interfaces belong in the consuming package. Producers return concrete types.
- One file per concern, not one file per type.
- Production `.go` files in `internal/app/` must stay under 400 lines.
- Doc comments on ALL exported names: `// Name does the thing.`
- Package comment immediately above `package` clause, no blank line.

### Testing

- Table-driven tests with `t.Run(name, ...)`. Anonymous structs with `name`, `want`, `wantErr`.
- `t.Helper()` in test helpers. `t.Setenv()` for test isolation.
- Run with `-race`: `go test -race ./...` or `make test-race`.
- Prefer `require` for critical assertions, `assert` for non-critical.

### Performance

- `strconv` over `fmt.Sprintf` for primitives. `strings.Builder` for loop concatenation.
- Preallocate capacity: `make([]T, 0, expectedSize)`.
- Profile before optimizing. Don't guess.

## Project Rules

- **Verify APIs before coding.** Use Context7 MCP (`resolve-library-id` then `query-docs`) or fetch official docs to confirm signatures — don't rely on memory.
- **Pure Go, no CGO** unless truly unavoidable. Critical for cross-platform builds.
- **Embedding spaces are incompatible across models.** Never mix embeddings from different Gemini model versions.
- Use `RETRIEVAL_DOCUMENT` task type for indexing, `RETRIEVAL_QUERY` for search queries.
- FFmpeg is invoked as a subprocess, not via Go bindings.
- Never commit `docs/` — design specs, plans, and research notes are local-only.
- Committed images live in top-level `assets/`, never under `docs/`.

## Build & Run

```bash
# Install Wails CLI (one-time)
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# Development mode (hot reload).
# -tags webkit2_41 required on Linux with webkit2gtk-4.1 (Ubuntu 22.04+)
wails dev -tags webkit2_41

# Production build
wails build -tags webkit2_41

# Go tests
go test ./...
go test ./internal/indexer -run TestChunkVideo

# Frontend (standalone)
cd frontend && npm install && npm run dev
cd frontend && npm run lint
```

## Git rules

- Never commit `docs/`.
- No `Co-Authored-By` lines or other AI attribution.
- No emojis in commit messages.
- Write commits in a natural, human tone — concise and descriptive.
