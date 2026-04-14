---
name: search-engine
description: Universal Search search engine internals. Use when working on query execution, result ranking, search planning, or the relaxation ladder in internal/search/.
---

# Search Engine

## Planner (`planner.go`)

Routes between three execution strategies based on candidate set size:
- **Pure HNSW** — default; fast approximate nearest-neighbour search
- **Brute-force cosine** — used when total file count < 5000; scans all inline `vector_blob` rows in SQLite
- **HNSW post-filter** — used when the filtered candidate set is large enough to justify it

## Scoring

`SearchResultDTO.Score` = `1 - distance/2` where distance is the HNSW cosine distance (range 0–2). Score range is 0–1; higher is better.

## Reranker (`rerank.go`)

Applied after initial retrieval:
- Should-boost products: scores for matching filter conditions
- 1.2× recency multiplier for recently modified files
- Produces `FinalScore`

## Relaxation Ladder (`relaxation.go`)

`RelaxationLadder` progressively drops Must clauses until results are found. Drop order:
1. `modified_at`
2. `size_bytes`
3. `path`
4. `file_type`
5. `extension`

MustNot clauses are **never** dropped.

## Full Search Pipeline (`SearchWithFilters`)

`raw` query → parse (`ParseQuery`) → plan → rerank → relax → filename fallback (`SearchFilenameContains`)

`SearchWithFiltersResult` includes both the results and the `ParseQueryResult` used (for chip rendering in the UI).
