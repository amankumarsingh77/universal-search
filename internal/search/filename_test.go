package search

import (
	"context"
	"testing"

	"universal-search/internal/store"
)

// mockFilenameStore implements FilenameStore for tests.
type mockFilenameStore struct {
	records []store.FileRecord
}

func (m *mockFilenameStore) SearchFilenameContains(query string) ([]store.FileRecord, error) {
	var out []store.FileRecord
	for _, r := range m.records {
		if containsSubstring(r.Path, query) {
			out = append(out, r)
		}
	}
	return out, nil
}

// containsSubstring is a simple helper (case-sensitive) used only in tests.
func containsSubstring(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && findStr(s, sub)
}

func findStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestFilenameMatch_FindsSubstring verifies that FilenameMatch returns records
// whose path contains the raw query as a substring.
func TestFilenameMatch_FindsSubstring(t *testing.T) {
	ms := &mockFilenameStore{
		records: []store.FileRecord{
			{ID: 1, Path: "/docs/last-week-report.pdf"},
			{ID: 2, Path: "/docs/annual-summary.pdf"},
			{ID: 3, Path: "/tmp/notes.txt"},
		},
	}

	results := FilenameMatch(context.Background(), ms, "last-week-report")

	if len(results) != 1 {
		t.Fatalf("expected 1 match, got %d", len(results))
	}
	if results[0].Path != "/docs/last-week-report.pdf" {
		t.Errorf("expected /docs/last-week-report.pdf, got %s", results[0].Path)
	}
}

// TestFilenameMatch_EmptyQueryReturnsNothing verifies no results for empty query.
func TestFilenameMatch_EmptyQueryReturnsNothing(t *testing.T) {
	ms := &mockFilenameStore{
		records: []store.FileRecord{
			{ID: 1, Path: "/docs/report.pdf"},
		},
	}

	results := FilenameMatch(context.Background(), ms, "")

	if len(results) != 0 {
		t.Fatalf("expected 0 matches for empty query, got %d", len(results))
	}
}

// TestMergeWithFilenameResults_LexicalAtTop verifies that filename matches
// appear before semantic results in the merged output.
func TestMergeWithFilenameResults_LexicalAtTop(t *testing.T) {
	semantic := []store.SearchResult{
		{File: store.FileRecord{Path: "/docs/unrelated.txt"}, Distance: 0.1},
		{File: store.FileRecord{Path: "/docs/something.txt"}, Distance: 0.2},
	}
	filename := []store.FileRecord{
		{Path: "/docs/last-week-report.pdf"},
	}

	merged := MergeWithFilenameResults(semantic, filename, "last-week-report", 10)

	if len(merged) < 3 {
		t.Fatalf("expected at least 3 results, got %d", len(merged))
	}
	// Filename match should be first
	if merged[0].File.Path != "/docs/last-week-report.pdf" {
		t.Errorf("expected filename match at top, got %s", merged[0].File.Path)
	}
}

// TestMergeWithFilenameResults_Deduplicates verifies that a file appearing in
// both semantic and filename results is returned only once.
func TestMergeWithFilenameResults_Deduplicates(t *testing.T) {
	sharedPath := "/docs/shared-report.pdf"

	semantic := []store.SearchResult{
		{File: store.FileRecord{Path: sharedPath}, Distance: 0.1},
		{File: store.FileRecord{Path: "/docs/other.txt"}, Distance: 0.3},
	}
	filename := []store.FileRecord{
		{Path: sharedPath}, // same file
	}

	merged := MergeWithFilenameResults(semantic, filename, "shared-report", 10)

	// Count occurrences of the shared path
	count := 0
	for _, r := range merged {
		if r.File.Path == sharedPath {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected shared file to appear exactly once, got %d times", count)
	}
}

// TestMergeWithFilenameResults_RespectsK verifies the result is capped at k.
func TestMergeWithFilenameResults_RespectsK(t *testing.T) {
	semantic := []store.SearchResult{
		{File: store.FileRecord{Path: "/a.txt"}, Distance: 0.1},
		{File: store.FileRecord{Path: "/b.txt"}, Distance: 0.2},
		{File: store.FileRecord{Path: "/c.txt"}, Distance: 0.3},
	}
	filename := []store.FileRecord{
		{Path: "/d.pdf"},
		{Path: "/e.pdf"},
	}

	merged := MergeWithFilenameResults(semantic, filename, "query", 4)

	if len(merged) > 4 {
		t.Errorf("expected at most 4 results, got %d", len(merged))
	}
}
