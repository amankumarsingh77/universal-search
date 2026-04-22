package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"findo/internal/apperr"
	"findo/internal/query"
)

// SearchWithFilters runs a search using the parsed FilterSpec, applying a
// denylist to remove chips the user has dismissed.
// When offline (no API key), it falls back to filename-contains search.
// If the vector search returns a network error, it falls back to filename search for that query.
func (a *App) SearchWithFilters(raw string, semanticQuery string, denyList []string) (SearchWithFiltersResult, error) {
	start := time.Now()

	if !a.isNLQueryEnabled() {
		results, err := a.Search(raw)
		return SearchWithFiltersResult{Results: results}, err
	}

	// Snapshot embedder state under read lock to avoid races with concurrent SetGeminiAPIKey.
	emb, _ := a.snapshotEmbedderState()
	isOffline := emb == nil

	a.logger.Debug("search_with_filters: start", "raw", raw, "offline", isOffline, "deny_list_len", len(denyList))
	if isOffline {
		a.logger.Debug("search_with_filters: offline mode, using filename search only")
		// Use the most informative semantic query available; fall back to raw.
		grammarSpecOffline := query.Parse(raw)
		offlineQuery := semanticQuery
		if offlineQuery == "" {
			offlineQuery = grammarSpecOffline.SemanticQuery
		}
		if offlineQuery == "" {
			offlineQuery = raw
		}
		return a.searchFilenameOnly(offlineQuery)
	}

	// Get current FilterSpec (from cache or grammar).
	grammarSpec := query.Parse(raw)
	grammarFilterCount := len(grammarSpec.Must) + len(grammarSpec.MustNot) + len(grammarSpec.Should)
	a.logger.Debug("search_with_filters: grammar parsed",
		"filter_count", grammarFilterCount,
		"semantic_query", grammarSpec.SemanticQuery,
	)

	var mergedSpec query.FilterSpec
	cacheHit := false
	if a.parsedQueryCache != nil {
		if cached, err := a.parsedQueryCache.Get(raw); err == nil && cached != nil {
			mergedSpec = *cached
			cacheHit = true
			a.logger.Debug("search_with_filters: using cached filter spec")
		} else {
			mergedSpec = grammarSpec
		}
	} else {
		mergedSpec = grammarSpec
	}

	llmFilterCount := len(mergedSpec.Must) + len(mergedSpec.MustNot) + len(mergedSpec.Should)
	a.logger.Debug("search_with_filters: filter spec resolved",
		"grammar_filters", grammarFilterCount,
		"merged_filters", llmFilterCount,
		"cache_hit", cacheHit,
		"source", mergedSpec.Source,
	)

	// Apply denylist.
	denyClauseKeys := parseDenyList(denyList)
	if len(denyClauseKeys) > 0 {
		a.logger.Debug("search_with_filters: applying denylist", "deny_count", len(denyClauseKeys))
	}
	mergedSpec = query.Merge(mergedSpec, query.FilterSpec{}, denyClauseKeys)

	// Override semantic query if provided.
	if semanticQuery != "" {
		mergedSpec.SemanticQuery = semanticQuery
	}

	// Embed the semantic query.
	queryText := mergedSpec.SemanticQuery
	if queryText == "" {
		queryText = raw
	}
	if queryText == "" {
		return SearchWithFiltersResult{}, nil
	}

	a.logger.Debug("search_with_filters: embedding query", "query_text", queryText)
	queryVec, err := a.getQueryVector(queryText)
	if err != nil {
		a.logger.Warn("search_with_filters: embedding failed", "error", err)
		if errors.Is(err, apperr.ErrRateLimited) {
			var retryAfterMs int64
			if !emb.PausedUntil().IsZero() {
				if remaining := time.Until(emb.PausedUntil()).Milliseconds(); remaining > 0 {
					retryAfterMs = remaining
				}
			}
			return SearchWithFiltersResult{
				ErrorCode:    apperr.ErrRateLimited.Code,
				RetryAfterMs: retryAfterMs,
			}, nil
		}
		return SearchWithFiltersResult{ErrorCode: apperr.ErrEmbedFailed.Code}, nil
	}
	a.logger.Debug("search_with_filters: query embedded, running search engine",
		"must", len(mergedSpec.Must),
		"must_not", len(mergedSpec.MustNot),
		"should", len(mergedSpec.Should),
	)

	// Run SearchWithSpec; surface typed errors to the caller.
	searchResult, err := a.engine.SearchWithSpec(queryVec, mergedSpec, raw, 20)
	if err != nil {
		if errors.Is(err, apperr.ErrModelMismatch) {
			a.logger.Warn("search: model mismatch, prompting user to reindex")
			return SearchWithFiltersResult{ErrorCode: apperr.ErrModelMismatch.Code}, nil
		}
		return SearchWithFiltersResult{}, err
	}
	a.logger.Debug("search_with_filters: engine returned",
		"strategy", searchResult.Strategy,
		"planner_count", searchResult.PlannerCount,
		"results", len(searchResult.Results),
		"relaxation_banner", searchResult.RelaxationBanner,
	)

	dtos := make([]SearchResultDTO, 0, len(searchResult.Results))
	for _, r := range searchResult.Results {
		dtos = append(dtos, toSearchResultDTO(r))
	}

	a.logger.Debug("query pipeline complete",
		"raw", raw,
		"grammar_filter_count", grammarFilterCount,
		"llm_filter_count", llmFilterCount,
		"chosen_strategy", searchResult.Strategy,
		"planner_count", searchResult.PlannerCount,
		"final_result_count", len(dtos),
		"latency_ms", time.Since(start).Milliseconds(),
		"cache_hit", cacheHit,
		"offline", isOffline,
	)

	return SearchWithFiltersResult{
		Results:          dtos,
		RelaxationBanner: searchResult.RelaxationBanner,
	}, nil
}

// searchFilenameOnly runs a filename-contains search and returns results as DTOs.
func (a *App) searchFilenameOnly(queryText string) (SearchWithFiltersResult, error) {
	if a.store == nil || queryText == "" {
		return SearchWithFiltersResult{}, nil
	}
	files, err := a.store.SearchFilenameContains(queryText)
	if err != nil {
		return SearchWithFiltersResult{}, err
	}
	dtos := make([]SearchResultDTO, 0, len(files))
	for _, f := range files {
		dtos = append(dtos, SearchResultDTO{
			FilePath:      f.Path,
			FileName:      filepath.Base(f.Path),
			FileType:      f.FileType,
			Extension:     f.Extension,
			SizeBytes:     f.SizeBytes,
			ThumbnailPath: f.ThumbnailPath,
			Score:         0,
			ModifiedAt:    f.ModifiedAt.Unix(),
		})
	}
	return SearchWithFiltersResult{Results: dtos}, nil
}

// getQueryVector embeds the query and caches the result.
func (a *App) getQueryVector(queryText string) ([]float32, error) {
	if a.embedder == nil {
		return nil, fmt.Errorf("embedder not initialized — set GEMINI_API_KEY")
	}

	if a.store != nil {
		cached, err := a.store.GetQueryCache(queryText)
		if err == nil && cached != nil {
			return cached, nil
		}
	}

	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	vec, err := a.embedder.EmbedQuery(ctx, queryText)
	if err != nil {
		return nil, err
	}
	if a.store != nil {
		go func() { a.store.SetQueryCache(queryText, vec) }()
	}
	return vec, nil
}
