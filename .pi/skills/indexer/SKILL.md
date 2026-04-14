---
name: indexer
description: Universal Search indexing pipeline internals. Use when working on file indexing, embedding, HNSW storage, rate limiting, or the worker pool in internal/indexer/.
---

# Indexer Pipeline

## Worker Pool

4 goroutines pull `indexJob` items from a shared `jobCh` channel. `processFolder` is a pure producer: walks the directory, pushes one `indexSingleFile` entry per eligible file onto `jobCh`. Workers consume and process.

## Two-Phase Commit

1. `UpsertFile` — called first with an empty content hash (marks file as seen)
2. `EmbedBatch` — embeds all chunks for the file in a single batched API call (up to 100 chunks; larger files use multiple sequential calls)
3. `UpdateContentHash` — written only after all chunks are successfully embedded

This prevents phantom entries after a crash: if the process dies between steps 1 and 3, the file is re-queued on next startup by `ReconcileIndex`.

## Generation Counter

`ResetStatus` increments a `generation` counter. Workers check it after each `EmbedBatch` returns and abort if the counter has advanced, discarding results without committing. This prevents stale results from a cancelled reindex from landing in the index.

## HNSW Atomic Save

Saved every 50 chunks. Write to `.graph.tmp` / `.map.tmp`, then rename to the real path. Rename is atomic on POSIX. Never write directly to the live index files.

## Rate Limiter

Sliding window, 55 req/min default. Key methods:
- `PauseUntil(t time.Time)` — sets a global pause; later time wins if called concurrently
- `PausedUntil() time.Time` — returns current pause deadline, zero if not paused
- `WaitIfPaused(ctx)` — blocks all workers until pause expires or ctx cancelled

When Gemini returns 429 with `Retry-After` / `retry_delay`, `embed()` calls `PauseUntil` on the shared limiter. `IndexStatus.QuotaPaused` = true, `IndexStatus.QuotaResumeAt` = ISO 8601 resume time.

## Force Reindex

`ReindexNow` / `ReindexFolder` bypass the content hash check and re-embed every file. Call `SetTotalFiles(n)` before submitting force-reindex jobs for accurate progress display.

## Startup Goroutines

Two background goroutines start on app launch:
- `ReconcileIndex` — re-queues files whose vector chunks are missing from the HNSW index
- `StartupRescan` — walks indexed folders to detect new/modified files; removes records for deleted files
