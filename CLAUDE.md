# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Universal Search — a local-first, cross-platform desktop app that indexes files on disk using Gemini Embedding 2 and provides instant semantic search via a global keyboard shortcut. Built with Wails v2 (Go backend + React frontend).

Design spec: `docs/superpowers/specs/2026-03-27-universal-search-design.md`

## Tech Stack

- **Desktop framework:** Wails v2
- **Frontend:** React + TypeScript
- **Backend:** Go
- **Vector DB:** TFMV/hnsw (pure Go, HNSW algorithm)
- **Metadata DB:** SQLite via ncruces/go-sqlite3 (pure Go, no CGO)
- **Embeddings:** Gemini Embedding 2 Preview (`gemini-embedding-2-preview`, 768-dim via MRL)
- **Video processing:** ffmpeg (subprocess)
- **File watching:** fsnotify

## Architecture

The app has three main layers:

1. **React frontend** — Raycast-style floating search window with thumbnail-based results list (left) and smart context-aware preview panel (right). Collapsible indexing progress bar at the bottom. Filter chips (`FilterChip.tsx`) render structured query constraints extracted from natural-language input; the search bar manages chip state via `searchReducer.ts` (a finite state machine for pending/active filter chips). An offline indicator is shown when NL query understanding is disabled. `FailuresModal.tsx` surfaces per-file indexing failures grouped by error code (counts + sample paths), with a drill-down list of all individual failures; `lib/errorLabels.ts` maps stable apperr codes to human-readable UI strings (including entries for `ERR_QUERY_PARSE_FAILED` and `ERR_QUERY_RATE_LIMITED`). `ErrorBanner.tsx` renders hard query errors (e.g. rate-limited or parse-failed) returned in `ParseQueryResult.ErrorCode`; `WarningChip.tsx` renders non-fatal warnings from `ParseQueryResult.Warning`. `useSearch` threads `errorCode`, `warning`, and `retryAfterMs` from both `ParseQueryResult` and `SearchWithFiltersResult` through to these components.
2. **Go backend** — File watcher, indexing pipeline (classify → extract/chunk → embed via Gemini API → store), search engine (embed query → HNSW cosine search → join metadata), system tray management. The indexing pipeline runs 4 concurrent worker goroutines (default) that pull `indexJob` items from a shared `jobCh` channel. `processFolder` is a pure producer: it walks the directory and pushes one `jobSingleFile` entry per eligible file onto `jobCh`; workers consume and process them. Each file's chunks are embedded in a single batched Gemini API call via `EmbedBatch` (up to 100 chunks per call; larger files split into multiple sequential calls). The pipeline uses a two-phase commit: `UpsertFile` is called first (with an empty content hash to mark the file as seen), then `UpdateContentHash` is written only after all chunks are successfully embedded — preventing phantom entries after a crash. HNSW is saved to disk atomically every 50 chunks (write to `.graph.tmp`/`.map.tmp`, then rename). Normal indexing skips files whose content hash is unchanged; force-reindex (triggered by `ReindexNow`/`ReindexFolder`) bypasses the hash check and re-embeds every file. A `generation` counter in the pipeline increments on each `ResetStatus` call; workers check it after each `EmbedBatch` returns and abort if the counter has advanced, discarding results without committing. The `RateLimiter` (sliding window, 55 req/min default) supports a global `PauseUntil(t)` mechanism: when the Gemini API returns a 429 with a `Retry-After`/`retry_delay` header, `embed()` calls `PauseUntil` on the shared limiter, blocking all workers for the specified duration via `WaitIfPaused`. When a quota pause is active, `IndexStatus.QuotaPaused` is set to `true` and `IndexStatus.QuotaResumeAt` holds the ISO 8601 resume timestamp; the UI displays a "Rate limited — resuming in Xs" countdown. Transient failures from the indexing pipeline are automatically retried via a `RetryCoordinator` (in `internal/indexer/retry.go`): `ClassTransientRetry` errors use exponential backoff (1s/2s/4s, max 3 attempts), `ClassTransientWait` errors (rate-limited) block on `RateLimiter.WaitForUnpause` before re-submitting the file to `jobCh`. Terminal failures (permanent or exhausted retries) are recorded in a `FailureRegistry` (in `internal/indexer/failures.go`), a bounded in-memory FIFO (LIFO eviction at capacity) keyed by file path. The registry supports `Snapshot()` for a flat per-file list and `Groups()` for counts aggregated by error code with up to 3 sample paths per group. On startup, two background goroutines run: `ReconcileIndex` (re-queues files whose vector chunks are missing from the HNSW index) and `StartupRescan` (walks indexed folders to detect new or modified files and removes records for files deleted from disk). Search returns `SearchResultDTO` values which include a `Score` field (float32, 0–1) derived from the HNSW cosine distance as `1 - distance/2`. The **natural language query understanding** pipeline lives in `internal/query/`: `grammar.go` parses structured operator tokens (`kind:`, `ext:`, `size:`, `before:`, `after:`, `in:`, `path:`) from the raw query into a `FilterSpec`; `llmparse.go` invokes Gemini 2.5 Flash-Lite (500 ms timeout, structured output) when `trigger.go`'s `ShouldInvokeLLM` heuristic detects temporal phrases, negations, file-type terms, or queries longer than 6 tokens; `LLMParser.Parse` returns a typed `ParseResult` carrying an `Outcome` enum (`OK`, `Timeout`, `RateLimited`, `Failed`) and `RetryAfterMs int64` — transport errors (timeout, rate-limit) are no longer silently swallowed as grammar-only fallback but instead propagate their outcome to the caller; `datenorm.go` resolves relative date phrases (e.g. "yesterday", "last week") using olebedev/when and araddon/dateparse; `typo.go` provides OSA Levenshtein correction; `merge.go` merges grammar and LLM `FilterSpec` results; `cache.go` wraps the SQLite `parsed_query_cache` table to avoid redundant LLM calls. The **search engine** in `internal/search/` has three components: `planner.go` routes between pure-HNSW semantic search, brute-force cosine search (file count < 5000), and HNSW post-filter based on the cardinality of the filtered candidate set; `rerank.go` applies should-boost products and a 1.2× recency multiplier to produce a `FinalScore`; `relaxation.go` implements `RelaxationLadder`, which progressively drops Must clauses (in order: modified_at → size_bytes → path → file_type → extension) until results are found, never dropping MustNot clauses. Key App methods: `GetIgnoredFolders`, `AddIgnoredFolder`, `RemoveIgnoredFolder` (manage excluded folder name patterns); `GetFolders`, `AddFolder`, `RemoveFolder` (manage indexed folders); `ReindexNow` (force full re-index of all folders, bypassing hash skip); `ReindexFolder(path string)` (force re-index a single folder); `GetSetting`/`SetSetting` (key-value settings); `GetFilePreview(path string) (string, error)` (returns up to 8 KB of a text/code file's content, errors on binary or non-UTF-8 files); `ParseQuery(raw string) (ParseQueryResult, error)` (parses a raw query string into a `FilterSpec` with chip DTOs, using grammar + optional LLM + cache; `ParseQueryResult` now carries `ErrorCode string`, `Warning string`, and `RetryAfterMs int64` so callers can surface LLM parse failures without inspecting the Go error); `SearchWithFilters(raw, semanticQuery string, denyList []string) (SearchWithFiltersResult, error)` (full NL search pipeline: parse → plan → rerank → relax → filename fallback; on embed errors the method now returns `ERR_EMBED_FAILED` or `ERR_RATE_LIMITED` rather than falling back to filename-only search — the old `strings.Contains("network"/"embed")` substring routing is removed; `SearchWithFiltersResult` carries `RetryAfterMs int64` populated from `Embedder.PausedUntil()` when rate-limited); `GetDebugStats() map[string]any` (returns internal counters for diagnostics); `DetectMissingVectorBlobs() bool` (checks whether any chunk rows are missing their inline vector blob); `NeedsReindex() bool` (true if missing vector blobs are detected); `GetNLQueryEnabled() bool` / `SetNLQueryEnabled(enabled bool) error` (toggle NL query understanding feature flag); `GetIndexFailures() []IndexFailureDTO` (returns the full per-file failure snapshot from `FailureRegistry`). `IndexStatusDTO` now carries `PendingRetryFiles int` (count of files queued in the retry coordinator) and `FailedFileGroups []FailureGroupDTO` (failure counts grouped by error code, each with up to 3 sample paths). `FailureGroupDTO` and `IndexFailureDTO` are the wire types for these surfaces. Key Pipeline methods: `SetTotalFiles(n int)` (pre-sets total count before submitting force-reindex jobs for accurate progress display); `ResetStatus` (increments generation counter, zeroes progress counters, and calls `RetryCoordinator.DropAll` to discard pending retries). Key Embedder methods: `EmbedBatch(ctx, []ChunkInput) ([][]float32, error)` (batches up to 100 chunks per Gemini API call, preserves input order); `PausedUntil() time.Time` (returns the current rate-limit resume time, zero if not paused — added to the `Embedder` interface so callers can read remaining pause duration without depending on the concrete `*RateLimiter`). The Gemini embedder wraps 429/RESOURCE_EXHAUSTED responses with `apperr.ErrRateLimited` so callers can use `errors.Is` instead of inspecting status codes. Key RateLimiter methods: `PauseUntil(t time.Time)` (sets a global pause; later time wins if called concurrently); `PausedUntil() time.Time` (returns current pause deadline, zero if not paused); `WaitIfPaused(ctx)` (blocks until pause expires or ctx cancelled); `WaitForUnpause(ctx context.Context) error` (blocks until the active pause has expired — broadcast wakeup via channel close — used by `RetryCoordinator` for `ClassTransientWait` retries). Key apperr additions: seven indexing-failure codes (`ERR_UNSUPPORTED_FORMAT`, `ERR_EXTRACTION_FAILED`, `ERR_FILE_TOO_LARGE`, `ERR_FILE_UNREADABLE`, `ERR_EMBED_COUNT_MISMATCH`, `ERR_HNSW_ADD`, `ERR_STORE_WRITE`); two query-pipeline codes (`ERR_QUERY_PARSE_FAILED` — LLM returned an unusable result, `ERR_QUERY_RATE_LIMITED` — Gemini 429 during query parse or embed); `ERR_EMBED_FAILED` and `ERR_RATE_LIMITED` for search-time embed errors; `Classify(err error) Classification` helper that maps an `*Error` to `ClassPermanent`, `ClassTransientRetry`, or `ClassTransientWait` — unregistered or non-`*Error` values default to `ClassPermanent`.
3. **Storage** — SQLite for file metadata (paths, types, hashes, chunk timestamps, thumbnails), indexed folders, excluded patterns, and key-value settings. TFMV/hnsw for 768-dim vector embeddings. Separate concerns: SQLite is the source of truth for "what files exist", HNSW is the search index. On first launch, 20 default ignore patterns are seeded into the `excluded_patterns` table (e.g. `node_modules`, `.git`, `venv`). The `chunks` table has a `vector_blob BLOB` column (added via idempotent `ALTER TABLE`) that stores each chunk's embedding inline for brute-force search without hitting the HNSW index. The `parsed_query_cache` table (columns: `query_text_normalized`, `spec_json`, `created_at`, `last_used_at`) caches serialized `FilterSpec` JSON keyed by normalized query text. Three additional indexes exist on `files`: `idx_files_type` (file_type), `idx_files_ext` (extension), and `idx_files_modified` (modified_at). New store methods: `CountFiltered(spec FilterSpec) (int, error)` and `FilterFileIDs(spec FilterSpec) ([]int64, error)` execute `buildWhereClause`-generated SQL; `GetVectorBlobs(fileIDs []int64) (map[int64][][]float32, error)` fetches inline vectors for brute-force search; `UpsertParsedQueryCache` / `GetParsedQueryCache` / `EvictOldParsedQueryCache` manage the cache table; `SearchFilenameContains(query string) ([]FileRecord, error)` performs a LIKE-based filename search; `CountFiles() (int, error)` returns total indexed file count; `HasMissingVectorBlobs() (bool, error)` checks for NULL vector_blob rows; `VecToBlob` / `BlobToVec` are exported helpers for float32 slice ↔ little-endian byte slice conversion.

Video pipeline follows sentrysearch patterns: 30s chunks with 5s overlap, 480p/5fps preprocessing, still-frame detection, direct video-to-embedding (no captioning).

## Backend layout

The Wails-bound `App` lives in `internal/app/` and is split by concern across small files:
`app.go` (struct + constructor + core helpers), `folders.go` (indexed-folder and ignore-pattern management), `indexing.go` (reindex entrypoints), `search.go` + `search_filters.go` + `search_chips.go` (query pipeline and chip DTOs), `settings.go` (key-value settings + hotkey), `system.go` (window, tray, file preview), `stats.go` (debug stats), `lifecycle.go` (startup/shutdown), `dto.go` (shared DTOs), `snapshot.go` (embedder-state snapshot). Every file in `internal/app/` is kept under 400 lines and enforced by `scripts/check-file-size.sh`.

Supporting packages: `internal/indexer/` (pipeline, reconcile, rescan, failures registry, retry coordinator), `internal/embedder/` (Gemini + Fake + RateLimiter — these are the only embedder implementations shipped), `internal/search/` (planner, rerank, relaxation, filename fallback), `internal/query/` (grammar, LLM parse, date normalization, typo correction, merge, cache), `internal/store/` (SQLite with a schema migrator under `internal/store/migrations/`), `internal/vectorstore/` (HNSW wrapper), `internal/chunker/`, `internal/watcher/`, `internal/desktop/` (hotkey + tray), `internal/platform/` (OS-specific paths), `internal/logger/` (color + multi-handler slog), `internal/config/` (TOML loader + defaults), and `internal/apperr/` (stable error-code vocabulary that the frontend maps to user-facing messages).

## Customization

Runtime tuning lives in `config.toml` — the canonical tuning surface for indexing concurrency, embedder batch size and rate limits, HNSW parameters, search thresholds, and NL-query timeouts. The file is loaded from `~/.config/universal-search/config.toml` on Linux/macOS and `%APPDATA%\universal-search\config.toml` on Windows. Missing file and missing keys fall back to the values documented in `internal/config/defaults.toml`; consult that file for the full set of tunables and their defaults. Stable error codes surfaced by the Wails bindings are defined in `internal/apperr/` so the frontend can translate them into user-facing messages without string matching.

## Rules

- **Before writing any code, verify the correct syntax and API usage.** Use Context7 MCP (`resolve-library-id` then `query-docs`) or fetch official documentation to confirm function signatures, package APIs, and framework patterns. Do not rely on memory alone — always reference current docs.
- All Go dependencies must be **pure Go (no CGO)** unless absolutely unavoidable. This is critical for cross-platform builds.
- Embedding spaces are **incompatible across models**. Never mix embeddings from different Gemini model versions.
- Use `RETRIEVAL_DOCUMENT` task type for indexing, `RETRIEVAL_QUERY` for search queries.
- FFmpeg is called as a subprocess, not via Go bindings.
- **Do not add Claude as a co-author in commit messages.** No `Co-Authored-By` lines.
- **Never commit docs, specs, or plans.** Files in `docs/` (design specs, plans, research notes) are local-only and must not be committed to git.

## Build & Run

```bash
# Install Wails CLI (one-time)
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# Development mode (hot reload)
# -tags webkit2_41 required on Linux with webkit2gtk-4.1 (Ubuntu 22.04+)
wails dev -tags webkit2_41

# Build production binary
wails build -tags webkit2_41

# Run Go tests
go test ./...

# Run a single test
go test ./internal/indexer -run TestChunkVideo

# Frontend dev (standalone, if needed)
cd frontend && npm install && npm run dev

# Lint
cd frontend && npm run lint
```

## Git rules

- Never commit docs/ — these are local working files, not part of the deliverable.
- Never add Co-Authored-By or any AI attribution to commits.
- No emojis in commit messages.
- Write commit messages in a natural, human tone — as if the developer wrote them. Keep them concise and descriptive.