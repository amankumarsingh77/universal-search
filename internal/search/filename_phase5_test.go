package search

import (
	"testing"

	"universal-search/internal/store"
)

// REF-043: when Merger.Enabled=false, filename results are NOT merged.
func TestMerger_DisabledReturnsSemanticOnly(t *testing.T) {
	semantic := []store.SearchResult{
		{File: store.FileRecord{Path: "/docs/a.txt"}, Distance: 0.1},
	}
	filename := []store.FileRecord{{Path: "/docs/last-week-report.pdf"}}

	m := NewMerger(MergerConfig{Enabled: false})
	out := m.MergeWithFilenameResults(semantic, filename, "last-week-report", 10)
	if len(out) != 1 || out[0].File.Path != "/docs/a.txt" {
		t.Fatalf("expected only the semantic result, got %+v", out)
	}
}

// Enabled preserves the historical behaviour (filename-first).
func TestMerger_EnabledMerges(t *testing.T) {
	semantic := []store.SearchResult{
		{File: store.FileRecord{Path: "/docs/a.txt"}, Distance: 0.1},
	}
	filename := []store.FileRecord{{Path: "/docs/last-week-report.pdf"}}

	m := NewMerger(MergerConfig{Enabled: true})
	out := m.MergeWithFilenameResults(semantic, filename, "last-week-report", 10)
	if len(out) < 2 {
		t.Fatalf("expected 2 results, got %+v", out)
	}
	if out[0].File.Path != "/docs/last-week-report.pdf" {
		t.Fatalf("filename match should be first, got %s", out[0].File.Path)
	}
}
