package search

import (
	"testing"
	"time"

	"universal-search/internal/query"
	"universal-search/internal/store"
)

// makeResult creates a SearchResult with the given distance and file.
func makeResult(path string, distance float32) store.SearchResult {
	return store.SearchResult{
		File:     store.FileRecord{Path: path},
		VectorID: path + "-vec",
		Distance: distance,
	}
}

// makeResultWithFile creates a SearchResult with a fully-populated FileRecord.
func makeResultWithFile(f store.FileRecord, distance float32) store.SearchResult {
	return store.SearchResult{
		File:     f,
		VectorID: f.Path + "-vec",
		Distance: distance,
	}
}

// TestRerank_ShouldBoostApplied verifies that a Should clause with Boost=1.5
// multiplies into the final score.
// cos_sim = 1 - Distance/2 = 1 - 0.4/2 = 0.8
// boost product = 1.5
// recency_mult = 1.0 (no date Must clause)
// final_score = 0.8 * 1.5 * 1.0 = 1.2
func TestRerank_ShouldBoostApplied(t *testing.T) {
	results := []store.SearchResult{
		{
			File:     store.FileRecord{Path: "/tmp/doc.pdf", FileType: "document", Extension: ".pdf"},
			VectorID: "vec-1",
			Distance: 0.4,
		},
	}

	spec := query.FilterSpec{
		Should: []query.Clause{
			{Field: query.FieldFileType, Op: query.OpEq, Value: "document", Boost: 1.5},
		},
	}

	ranked := Rerank(results, spec)

	if len(ranked) != 1 {
		t.Fatalf("expected 1 result, got %d", len(ranked))
	}

	const want = float32(1.2)
	const epsilon = float32(0.001)
	got := ranked[0].FinalScore
	if got < want-epsilon || got > want+epsilon {
		t.Errorf("expected FinalScore ~1.2, got %v (distance=%v)", got, ranked[0].Distance)
	}
}

// TestRerank_RecencyMultiplier verifies that a date-range Must clause with the
// file's ModifiedAt within range produces recency_mult=1.2.
func TestRerank_RecencyMultiplier(t *testing.T) {
	now := time.Now()
	modTime := now.Add(-24 * time.Hour) // yesterday

	results := []store.SearchResult{
		{
			File: store.FileRecord{
				Path:       "/tmp/recent.txt",
				FileType:   "text",
				Extension:  ".txt",
				ModifiedAt: modTime,
			},
			VectorID: "vec-recent",
			Distance: 0.4, // cos_sim = 0.8
		},
	}

	// Must clause: modified_at >= 2 days ago (file is within range)
	spec := query.FilterSpec{
		Must: []query.Clause{
			{
				Field: query.FieldModifiedAt,
				Op:    query.OpGte,
				Value: now.Add(-48 * time.Hour),
			},
		},
	}

	ranked := Rerank(results, spec)

	if len(ranked) != 1 {
		t.Fatalf("expected 1 result, got %d", len(ranked))
	}

	// cos_sim=0.8, no should boost, recency_mult=1.2 → 0.8 * 1.2 = 0.96
	const want = float32(0.96)
	const epsilon = float32(0.001)
	got := ranked[0].FinalScore
	if got < want-epsilon || got > want+epsilon {
		t.Errorf("expected FinalScore ~0.96, got %v", got)
	}
}

// TestRerank_NoRecencyOnPureSemantic verifies that a spec with no date Must clauses
// produces recency_mult=1.0 (no boost).
func TestRerank_NoRecencyOnPureSemantic(t *testing.T) {
	modTime := time.Now().Add(-24 * time.Hour)

	results := []store.SearchResult{
		{
			File:     store.FileRecord{Path: "/tmp/old.txt", FileType: "text", ModifiedAt: modTime},
			VectorID: "vec-old",
			Distance: 0.4, // cos_sim = 0.8
		},
	}

	// No date Must clause — pure semantic
	spec := query.FilterSpec{}

	ranked := Rerank(results, spec)

	if len(ranked) != 1 {
		t.Fatalf("expected 1 result, got %d", len(ranked))
	}

	// cos_sim=0.8, no boosts → 0.8
	const want = float32(0.8)
	const epsilon = float32(0.001)
	got := ranked[0].FinalScore
	if got < want-epsilon || got > want+epsilon {
		t.Errorf("expected FinalScore ~0.8, got %v", got)
	}
}

// TestRerank_SortsDescending verifies results are sorted by FinalScore descending.
func TestRerank_SortsDescending(t *testing.T) {
	results := []store.SearchResult{
		{
			File:     store.FileRecord{Path: "/tmp/far.txt", FileType: "text"},
			VectorID: "vec-far",
			Distance: 1.0, // cos_sim = 0.5 → lower
		},
		{
			File:     store.FileRecord{Path: "/tmp/close.txt", FileType: "text"},
			VectorID: "vec-close",
			Distance: 0.2, // cos_sim = 0.9 → higher
		},
		{
			File:     store.FileRecord{Path: "/tmp/mid.txt", FileType: "text"},
			VectorID: "vec-mid",
			Distance: 0.6, // cos_sim = 0.7 → middle
		},
	}

	spec := query.FilterSpec{} // no boosts

	ranked := Rerank(results, spec)

	if len(ranked) != 3 {
		t.Fatalf("expected 3 results, got %d", len(ranked))
	}

	// Verify descending order
	for i := 0; i < len(ranked)-1; i++ {
		if ranked[i].FinalScore < ranked[i+1].FinalScore {
			t.Errorf("results not sorted descending at index %d: %v < %v",
				i, ranked[i].FinalScore, ranked[i+1].FinalScore)
		}
	}

	// Verify the top result is the closest
	if ranked[0].File.Path != "/tmp/close.txt" {
		t.Errorf("expected /tmp/close.txt as top result, got %s", ranked[0].File.Path)
	}
}
