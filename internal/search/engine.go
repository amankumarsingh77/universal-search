package search

import (
	"universal-search/internal/store"
	"universal-search/internal/vectorstore"
)

// Engine performs semantic search by combining vector similarity search
// with SQLite metadata lookups.
type Engine struct {
	store *store.Store
	index *vectorstore.Index
}

// New creates a new search engine backed by the given store and vector index.
func New(s *store.Store, idx *vectorstore.Index) *Engine {
	return &Engine{store: s, index: idx}
}

// SearchByVector searches the HNSW index for the top-k nearest neighbors
// to queryVec, joins results with SQLite metadata, and deduplicates by
// file path (keeping the best-ranked match per file).
func (e *Engine) SearchByVector(queryVec []float32, k int) ([]store.SearchResult, error) {
	vecResults, err := e.index.Search(queryVec, k)
	if err != nil {
		return nil, err
	}

	if len(vecResults) == 0 {
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

	// Build a lookup from vectorID to result for fast access
	resultByVecID := make(map[string]store.SearchResult, len(results))
	for _, r := range results {
		resultByVecID[r.VectorID] = r
	}

	// Re-order by HNSW ranking and deduplicate by file path (keep best rank)
	seen := make(map[string]bool)
	var deduped []store.SearchResult
	for _, id := range vectorIDs {
		r, ok := resultByVecID[id]
		if !ok {
			continue
		}
		if seen[r.File.Path] {
			continue
		}
		seen[r.File.Path] = true
		deduped = append(deduped, r)
	}

	return deduped, nil
}
