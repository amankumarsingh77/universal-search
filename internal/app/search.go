package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"findo/internal/apperr"
	"findo/internal/query"
)

// Search embeds the query via Gemini and returns the top search results.
func (a *App) Search(queryText string) ([]SearchResultDTO, error) {
	if a.embedder == nil {
		return nil, apperr.New(apperr.ErrEmbedFailed.Code, "embedder not initialized — set GEMINI_API_KEY")
	}

	a.logger.Info("search query", "query", queryText)

	var vec []float32
	if a.store != nil {
		cached, err := a.store.GetQueryCache(queryText)
		if err == nil && cached != nil {
			a.logger.Info("query cache hit", "query", queryText)
			vec = cached
		}
	}

	if vec == nil {
		var err error
		vec, err = a.embedder.EmbedQuery(a.ctx, queryText)
		if err != nil {
			return nil, apperr.Wrap(apperr.ErrEmbedFailed.Code, "embedding failed", err)
		}
		if a.store != nil {
			go func() {
				if err := a.store.SetQueryCache(queryText, vec); err != nil {
					a.logger.Warn("search: failed to cache query vector", "error", err)
				}
			}()
		}
	}

	if a.engine == nil {
		return nil, apperr.New(apperr.ErrInternal.Code, "search engine not initialized")
	}
	results, err := a.engine.SearchByVector(vec, 20)
	if err != nil {
		return nil, apperr.Wrap(apperr.ErrInternal.Code, "search failed", err)
	}

	dtos := make([]SearchResultDTO, len(results))
	for i, r := range results {
		dtos[i] = SearchResultDTO{
			FilePath:      r.File.Path,
			FileName:      filepath.Base(r.File.Path),
			FileType:      r.File.FileType,
			Extension:     r.File.Extension,
			SizeBytes:     r.File.SizeBytes,
			ThumbnailPath: r.File.ThumbnailPath,
			StartTime:     r.StartTime,
			EndTime:       r.EndTime,
			Score:         1 - r.Distance/2,
		}
	}

	a.logger.Info("search results", "query", queryText, "results", len(dtos))
	for i, d := range dtos {
		a.logger.Debug("search result",
			"rank", i+1,
			"file", d.FileName,
			"type", d.FileType,
			"path", d.FilePath,
		)
	}

	return dtos, nil
}

// GetFilePreview returns up to 8192 bytes of a text/code file's content.
// Returns an error if the file is binary, unreadable, or not valid UTF-8.
func (a *App) GetFilePreview(path string) (string, error) {
	const maxBytes = 8192
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer f.Close()
	buf := make([]byte, maxBytes)
	n, _ := f.Read(buf)
	if n == 0 {
		return "", fmt.Errorf("empty file")
	}
	buf = buf[:n]
	for _, b := range buf {
		if b == 0 {
			return "", fmt.Errorf("binary file")
		}
	}
	if !utf8.Valid(buf) {
		return "", fmt.Errorf("invalid UTF-8")
	}
	return string(buf), nil
}

// PreEmbedQuery speculatively embeds the query and caches the result.
// Called by the frontend while the user types. Errors are swallowed — best-effort.
func (a *App) PreEmbedQuery(queryText string) {
	if a.embedder == nil || a.store == nil {
		return
	}
	cached, err := a.store.GetQueryCache(queryText)
	if err != nil || cached != nil {
		return
	}
	vec, err := a.embedder.EmbedQuery(a.ctx, queryText)
	if err != nil {
		a.logger.Debug("pre-embed failed", "query", queryText, "error", err)
		return
	}
	if err := a.store.SetQueryCache(queryText, vec); err != nil {
		a.logger.Debug("pre-embed cache write failed", "query", queryText, "error", err)
	}
}

// isNLQueryEnabled returns true unless nl_query_enabled is explicitly set to "false".
func (a *App) isNLQueryEnabled() bool {
	if a.store == nil {
		return true
	}
	val, _ := a.store.GetSetting("nl_query_enabled", "true")
	return val != "false"
}

// ParseQuery parses a raw query string into structured filter chips.
// It runs a grammar parse always, checks the cache, and conditionally invokes
// the LLM parser if the residual query warrants it.
// When offline (no API key), LLM parse is skipped and IsOffline is set to true.
func (a *App) ParseQuery(raw string) (ParseQueryResult, error) {
	if !a.isNLQueryEnabled() {
		a.logger.Debug("parse_query: NL query disabled, skipping", "raw", raw)
		return ParseQueryResult{}, nil
	}

	// Snapshot embedder-related fields under read lock to avoid data races with
	// concurrent SetGeminiAPIKey calls.
	emb, llmParser := a.snapshotEmbedderState()
	offline := emb == nil

	a.logger.Debug("parse_query: start", "raw", raw, "offline", offline)

	// Grammar parse — always, no network.
	grammarSpec := query.Parse(raw)
	a.logger.Debug("parse_query: grammar parsed",
		"must", len(grammarSpec.Must),
		"must_not", len(grammarSpec.MustNot),
		"should", len(grammarSpec.Should),
		"semantic_query", grammarSpec.SemanticQuery,
	)

	// Check cache before LLM.
	var mergedSpec query.FilterSpec
	cacheHit := false
	if a.parsedQueryCache != nil {
		if cached, err := a.parsedQueryCache.Get(raw); err == nil && cached != nil {
			mergedSpec = *cached
			cacheHit = true
			a.logger.Debug("parse_query: cache hit",
				"normalized_key", query.NormalizeKey(raw),
				"must", len(mergedSpec.Must),
				"must_not", len(mergedSpec.MustNot),
				"should", len(mergedSpec.Should),
				"source", mergedSpec.Source,
			)
		}
	}

	if !cacheHit {
		a.logger.Debug("parse_query: cache miss")
		if a.queryStats != nil {
			a.queryStats.recordCacheMiss()
		}
		llmSpec := grammarSpec
		trigger := query.Trigger{MinTokens: a.cfg.Query.TriggerMinTokens, MaxChars: a.cfg.Query.TriggerMaxChars}
		// Skip LLM when offline.
		shouldInvoke := !offline && trigger.ShouldInvokeLLM(grammarSpec.SemanticQuery) && llmParser != nil && a.ctx != nil
		a.logger.Debug("parse_query: LLM invocation decision",
			"should_invoke", shouldInvoke,
			"offline", offline,
			"llm_available", llmParser != nil,
			"trigger_result", trigger.ShouldInvokeLLM(grammarSpec.SemanticQuery),
		)
		if shouldInvoke {
			timeoutMs := a.cfg.Query.LLMTimeoutMs
			if timeoutMs <= 0 {
				timeoutMs = 2000
			}
			ctx, cancel := context.WithTimeout(a.ctx, time.Duration(timeoutMs)*time.Millisecond)
			defer cancel()
			llmStart := time.Now()
			a.logger.Debug("parse_query: invoking LLM parser")
			parseResult, parseErr := llmParser.Parse(ctx, raw, grammarSpec)
			elapsed := time.Since(llmStart).Milliseconds()
			if a.queryStats != nil {
				a.queryStats.recordLLMCall(elapsed)
			}
			if parseErr != nil {
				a.logger.Debug("parse_query: LLM parse failed (unexpected error), returning error code", "error", parseErr, "latency_ms", elapsed)
				// parseErr is unexpected per interface contract; treat as OutcomeFailed — no cache write.
				return ParseQueryResult{ErrorCode: apperr.ErrQueryParseFailed.Code}, nil
			} else {
				switch parseResult.Outcome {
				case query.OutcomeOK:
					llmSpec = parseResult.Spec
					a.logger.Debug("parse_query: LLM parse complete",
						"latency_ms", elapsed,
						"must", len(llmSpec.Must),
						"must_not", len(llmSpec.MustNot),
						"should", len(llmSpec.Should),
					)
				case query.OutcomeTimeout:
					// Use grammar spec; set Warning; skip cache write below.
					a.logger.Debug("parse_query: LLM parse timed out, using grammar-only", "latency_ms", elapsed)
					mergedSpec = query.Merge(grammarSpec, grammarSpec, nil)
					chips := buildChipDTOs(mergedSpec)
					return ParseQueryResult{
						Chips:         chips,
						SemanticQuery: mergedSpec.SemanticQuery,
						HasFilters:    len(mergedSpec.Must)+len(mergedSpec.MustNot)+len(mergedSpec.Should) > 0,
						CacheHit:      false,
						IsOffline:     offline,
						Warning:       "query_parse_timeout",
					}, nil
				case query.OutcomeFailed:
					a.logger.Debug("parse_query: LLM parse failed (terminal), returning error code", "latency_ms", elapsed)
					return ParseQueryResult{ErrorCode: apperr.ErrQueryParseFailed.Code}, nil
				case query.OutcomeRateLimited:
					a.logger.Debug("parse_query: LLM parse rate-limited", "retry_after_ms", parseResult.RetryAfterMs, "latency_ms", elapsed)
					return ParseQueryResult{
						ErrorCode:    apperr.ErrQueryRateLimited.Code,
						RetryAfterMs: parseResult.RetryAfterMs,
					}, nil
				}
			}
		}
		mergedSpec = query.Merge(grammarSpec, llmSpec, nil)
		a.logger.Debug("parse_query: merged spec",
			"must", len(mergedSpec.Must),
			"must_not", len(mergedSpec.MustNot),
			"should", len(mergedSpec.Should),
			"semantic_query", mergedSpec.SemanticQuery,
			"source", mergedSpec.Source,
		)
		// Only write to cache on successful (OK) outcome.
		if a.parsedQueryCache != nil {
			_ = a.parsedQueryCache.Set(raw, mergedSpec)
			a.logger.Debug("parse_query: stored in cache")
		}
	} else {
		if a.queryStats != nil {
			a.queryStats.recordCacheHit()
		}
	}

	chips := buildChipDTOs(mergedSpec)
	a.logger.Debug("parse_query: complete", "chips", len(chips), "cache_hit", cacheHit)

	return ParseQueryResult{
		Chips:         chips,
		SemanticQuery: mergedSpec.SemanticQuery,
		HasFilters:    len(mergedSpec.Must)+len(mergedSpec.MustNot)+len(mergedSpec.Should) > 0,
		CacheHit:      cacheHit,
		IsOffline:     offline,
	}, nil
}
