package search

import (
	"testing"
	"time"

	"universal-search/internal/query"
	"universal-search/internal/store"
)

// REF-043: Reranker honours RecencyBoostMultiplier from config. A larger
// multiplier produces a higher FinalScore for a matching recent file.
func TestReranker_RecencyMultiplierFromConfig(t *testing.T) {
	now := time.Now()
	file := store.FileRecord{
		Path:       "/tmp/recent.txt",
		FileType:   "text",
		ModifiedAt: now.Add(-24 * time.Hour),
	}
	results := []store.SearchResult{{File: file, VectorID: "v1", Distance: 0.4}}
	spec := query.FilterSpec{
		Must: []query.Clause{{Field: query.FieldModifiedAt, Op: query.OpGte, Value: now.Add(-48 * time.Hour)}},
	}

	low := NewReranker(RerankerConfig{RecencyBoostMultiplier: 1.0}).Rerank(results, spec)
	high := NewReranker(RerankerConfig{RecencyBoostMultiplier: 2.0}).Rerank(results, spec)

	if !(high[0].FinalScore > low[0].FinalScore) {
		t.Fatalf("expected 2.0 multiplier to outscore 1.0; low=%v high=%v", low[0].FinalScore, high[0].FinalScore)
	}
}
