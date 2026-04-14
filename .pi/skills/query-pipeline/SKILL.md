---
name: query-pipeline
description: Universal Search natural language query understanding pipeline. Use when working on query parsing, date normalization, LLM query understanding, typo correction, or the query cache in internal/query/.
---

# NL Query Pipeline

## Grammar Parser (`grammar.go`)

Parses structured operator tokens directly from the raw query string into a `FilterSpec`:
- `kind:` — file type (document, image, video, code, etc.)
- `ext:` — file extension
- `size:` — file size comparison (`>1mb`, `<500kb`)
- `before:` / `after:` — date bounds
- `in:` — directory scope
- `path:` — path substring

## LLM Trigger (`trigger.go`)

`ShouldInvokeLLM(query string) bool` returns true when:
- query contains temporal phrases ("yesterday", "last week", "recent")
- query contains negations ("not", "without", "excluding")
- query contains file-type terms ("spreadsheet", "presentation", "image")
- query has more than 6 tokens

## LLM Parser (`llmparse.go`)

Calls Gemini 2.5 Flash-Lite with a 500ms timeout and structured output. Returns a `FilterSpec` to be merged with the grammar result.

## Date Normalizer (`datenorm.go`)

Resolves relative date phrases using `olebedev/when` (English natural language dates) and `araddon/dateparse` (structured formats). Converts to absolute `time.Time` for `before:` / `after:` filter bounds.

## Typo Corrector (`typo.go`)

OSA Levenshtein distance against a corpus of known file-type terms and operator names. Applied before grammar parsing.

## Merge (`merge.go`)

Merges grammar `FilterSpec` and LLM `FilterSpec`. LLM results take precedence for ambiguous fields; grammar results take precedence for explicitly typed operators.

## Cache (`cache.go`)

Wraps the `parsed_query_cache` SQLite table. Key: normalized query text. Value: serialized `FilterSpec` JSON.
- `UpsertParsedQueryCache(key, spec)` — insert or update
- `GetParsedQueryCache(key)` — returns nil on miss
- `EvictOldParsedQueryCache()` — removes entries older than 7 days

## Feature Flag

`GetNLQueryEnabled() bool` / `SetNLQueryEnabled(enabled bool) error` — toggles the LLM parse step. When disabled, only grammar parsing runs. The frontend shows an offline indicator when this returns false.
