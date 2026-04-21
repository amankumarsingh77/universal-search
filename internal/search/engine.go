package search

import (
	"context"
	"log/slog"
	"time"

	"universal-search/internal/apperr"
	"universal-search/internal/query"
	"universal-search/internal/store"
	"universal-search/internal/vectorstore"
)

// SearchWithSpecResult is the return value of Engine.SearchWithSpec.
type SearchWithSpecResult struct {
	Results          []store.SearchResult
	Strategy         string
	PlannerCount     int
	RelaxationBanner string // non-empty if a filter was dropped during relaxation
}

// Engine performs semantic search by combining vector similarity search
// with SQLite metadata lookups.
type Engine struct {
	store    *store.Store
	index    *vectorstore.Index
	logger   *slog.Logger
	planner  *Planner
	reranker *Reranker
	ladder   *Ladder
	merger   *Merger
	modelID  string // current embedder model; empty disables the gate
}

// EngineConfig bundles tunables for the search engine and its collaborators.
type EngineConfig struct {
	Planner  PlannerConfig
	Reranker RerankerConfig
	Ladder   LadderConfig
	Merger   MergerConfig
}

// DefaultEngineConfig returns the historical defaults for all search knobs.
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		Planner:  DefaultPlannerConfig(),
		Reranker: DefaultRerankerConfig(),
		Ladder:   DefaultLadderConfig(),
		Merger:   DefaultMergerConfig(),
	}
}

// New creates a new search engine backed by the given store and vector index.
func New(s *store.Store, idx *vectorstore.Index, logger *slog.Logger, cfg EngineConfig) *Engine {
	planner := NewPlannerWithLogger(s, idx, cfg.Planner, logger.WithGroup("planner"))
	return &Engine{
		store:    s,
		index:    idx,
		logger:   logger.WithGroup("search"),
		planner:  planner,
		reranker: NewReranker(cfg.Reranker),
		ladder:   NewLadder(cfg.Ladder),
		merger:   NewMerger(cfg.Merger),
	}
}

// NewWithConfig creates a new Engine with an explicit Planner plus explicit
// reranker/ladder/merger configs. Used by app.startup to wire Config-derived
// values through all collaborators.
func NewWithConfig(s *store.Store, idx *vectorstore.Index, logger *slog.Logger, p *Planner, cfg EngineConfig) *Engine {
	return &Engine{
		store:    s,
		index:    idx,
		logger:   logger.WithGroup("search"),
		planner:  p,
		reranker: NewReranker(cfg.Reranker),
		ladder:   NewLadder(cfg.Ladder),
		merger:   NewMerger(cfg.Merger),
	}
}

// NewWithModel creates an Engine that gates results by the given embedding
// model id. Chunks whose embedding_model does not match modelID are dropped
// from search results. An empty modelID disables the gate.
func NewWithModel(s *store.Store, idx *vectorstore.Index, logger *slog.Logger, p *Planner, cfg EngineConfig, modelID string) *Engine {
	e := NewWithConfig(s, idx, logger, p, cfg)
	e.modelID = modelID
	return e
}

// NewWithPlanner creates a new Engine with an explicit Planner (used in tests).
func NewWithPlanner(s *store.Store, idx *vectorstore.Index, logger *slog.Logger, p *Planner) *Engine {
	return &Engine{
		store:    s,
		index:    idx,
		logger:   logger.WithGroup("search"),
		planner:  p,
		reranker: NewReranker(DefaultRerankerConfig()),
		ladder:   NewLadder(DefaultLadderConfig()),
		merger:   NewMerger(DefaultMergerConfig()),
	}
}

// SearchByVector searches the HNSW index for the nearest neighbors to
// queryVec. It over-fetches 5x candidates from the vector index to ensure
// cross-cluster diversity (text, image, video embeddings live in different
// regions), then deduplicates by file path and trims to k results.
func (e *Engine) SearchByVector(queryVec []float32, k int) ([]store.SearchResult, error) {
	start := time.Now()

	// Over-fetch to improve diversity across embedding clusters.
	fetchK := k * 5
	vecResults, err := e.index.Search(queryVec, fetchK)
	if err != nil {
		e.logger.Error("vector search failed", "error", err)
		return nil, err
	}

	if len(vecResults) == 0 {
		e.logger.Debug("no vector results")
		return nil, nil
	}

	vectorIDs := make([]string, len(vecResults))
	for i, vr := range vecResults {
		vectorIDs[i] = vr.ID
	}

	results, err := e.store.GetChunksByVectorIDs(vectorIDs)
	if err != nil {
		return nil, err
	}

	// Build distance map from vectorID to cosine distance.
	distMap := make(map[string]float32, len(vecResults))
	for _, r := range vecResults {
		distMap[r.ID] = r.Distance
	}
	for i := range results {
		results[i].Distance = distMap[results[i].VectorID]
	}

	// Build a lookup from vectorID to result for fast access
	resultByVecID := make(map[string]store.SearchResult, len(results))
	for _, r := range results {
		resultByVecID[r.VectorID] = r
	}

	// Re-order by HNSW ranking and deduplicate by file path (keep lowest distance)
	best := make(map[string]store.SearchResult)
	for _, id := range vectorIDs {
		r, ok := resultByVecID[id]
		if !ok {
			continue
		}
		existing, seen := best[r.File.Path]
		if !seen || r.Distance < existing.Distance {
			best[r.File.Path] = r
		}
	}

	// Re-traverse HNSW order to emit in rank order, capped at k.
	emitted := make(map[string]bool)
	var deduped []store.SearchResult
	for _, id := range vectorIDs {
		if len(deduped) >= k {
			break
		}
		r, ok := resultByVecID[id]
		if !ok {
			continue
		}
		if emitted[r.File.Path] {
			continue
		}
		// Only emit the best chunk for this file.
		if kept := best[r.File.Path]; kept.VectorID == r.VectorID {
			emitted[r.File.Path] = true
			deduped = append(deduped, r)
		}
	}

	e.logger.Info("search completed", "results", len(deduped), "candidates", len(vecResults), "duration", time.Since(start))
	return deduped, nil
}

// SearchWithSpec is the NL-query entry point. It routes through the Planner,
// applies zero-result relaxation if needed, reranks, and merges with filename
// matches. Filename matching runs after the planner on the same connection to
// avoid SQLite in-memory isolation issues across connections.
func (e *Engine) SearchWithSpec(queryVec []float32, spec query.FilterSpec, rawQuery string, k int) (SearchWithSpecResult, error) {
	start := time.Now()

	// 1. Run planner.
	results, strategy, plannerCount, err := e.planner.Plan(queryVec, spec, k)
	if err != nil {
		e.logger.Error("planner failed", "error", err, "strategy", strategy)
		return SearchWithSpecResult{Strategy: strategy, PlannerCount: plannerCount}, err
	}

	// Model gate: drop chunks whose embedding_model != engine.modelID. If the
	// gate strips every candidate and there was at least one before filtering,
	// signal ErrModelMismatch so the UI can prompt for reindex.
	if e.modelID != "" {
		preCount := len(results)
		filtered := results[:0]
		for _, r := range results {
			if r.EmbeddingModel == "" || r.EmbeddingModel == e.modelID {
				filtered = append(filtered, r)
			}
		}
		results = filtered
		if preCount > 0 && len(results) == 0 {
			e.logger.Warn("search: all candidates filtered by model gate", "engine_model", e.modelID)
			return SearchWithSpecResult{Strategy: strategy, PlannerCount: plannerCount}, apperr.ErrModelMismatch
		}
	}

	// 2. If zero results and has filters, try relaxation.
	var droppedDesc string
	if len(results) == 0 && len(spec.Must) > 0 {
		results, droppedDesc, err = e.ladder.RelaxationLadder(context.Background(), e.planner, queryVec, spec, k)
		if err != nil {
			return SearchWithSpecResult{Strategy: strategy, PlannerCount: plannerCount}, err
		}
	}

	// 3. Rerank.
	results = e.reranker.Rerank(results, spec)

	// 4. Run filename match and merge.
	filenameResults := FilenameMatch(context.Background(), e.store, rawQuery)
	results = e.merger.MergeWithFilenameResults(results, filenameResults, rawQuery, k)

	e.logger.Info("SearchWithSpec completed",
		"strategy", strategy,
		"plannerCount", plannerCount,
		"results", len(results),
		"rawQuery", rawQuery,
		"relaxationBanner", droppedDesc,
		"duration", time.Since(start),
	)

	return SearchWithSpecResult{
		Results:          results,
		Strategy:         strategy,
		PlannerCount:     plannerCount,
		RelaxationBanner: droppedDesc,
	}, nil
}
