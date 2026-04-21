package search

import (
	"context"
	"strings"

	"universal-search/internal/store"
)

// FilenameStore is the subset of store.Store used for filename matching.
type FilenameStore interface {
	SearchFilenameContains(query string) ([]store.FileRecord, error)
}

// FilenameMatch runs a SQLite LIKE search on files.path using rawQuery as
// a substring. Returns an empty slice (not nil) if rawQuery is empty.
func FilenameMatch(ctx context.Context, s FilenameStore, rawQuery string) []store.FileRecord {
	if rawQuery == "" {
		return []store.FileRecord{}
	}
	results, err := s.SearchFilenameContains(rawQuery)
	if err != nil || results == nil {
		return []store.FileRecord{}
	}
	return results
}

// MergerConfig toggles filename-match merging.
type MergerConfig struct {
	Enabled bool
}

// DefaultMergerConfig enables filename merging by default.
func DefaultMergerConfig() MergerConfig {
	return MergerConfig{Enabled: true}
}

// Merger unions semantic search results with filename matches.
type Merger struct {
	enabled bool
}

// NewMerger builds a Merger from cfg.
func NewMerger(cfg MergerConfig) *Merger {
	return &Merger{enabled: cfg.Enabled}
}

// MergeWithFilenameResults unions semantic search results with filename matches.
// When Merger.enabled is false, only the semantic results (trimmed to k) are
// returned.
//
// Merge strategy:
//  1. Filename matches whose path contains rawQuery as a substring are placed first.
//  2. Remaining semantic results follow in their original ranked order.
//  3. Deduplicates by file path — a file already in semantic results is not
//     duplicated if it also appears in filename matches.
//  4. Returns at most k results.
func (m *Merger) MergeWithFilenameResults(
	semantic []store.SearchResult,
	filename []store.FileRecord,
	rawQuery string,
	k int,
) []store.SearchResult {
	if !m.enabled {
		out := semantic
		if len(out) > k {
			out = out[:k]
		}
		return out
	}
	return mergeWithFilenameResults(semantic, filename, rawQuery, k)
}

// MergeWithFilenameResults is the package-level helper (defaults to enabled).
func MergeWithFilenameResults(
	semantic []store.SearchResult,
	filename []store.FileRecord,
	rawQuery string,
	k int,
) []store.SearchResult {
	return mergeWithFilenameResults(semantic, filename, rawQuery, k)
}

func mergeWithFilenameResults(
	semantic []store.SearchResult,
	filename []store.FileRecord,
	rawQuery string,
	k int,
) []store.SearchResult {
	// Build a set of paths already present in semantic results.
	seen := make(map[string]bool, len(semantic))
	for _, r := range semantic {
		seen[r.File.Path] = true
	}

	// Build synthetic results for filename matches not already in semantic.
	var lexicalNew []store.SearchResult
	for _, f := range filename {
		if seen[f.Path] {
			continue
		}
		if rawQuery != "" && !strings.Contains(f.Path, rawQuery) {
			continue
		}
		lexicalNew = append(lexicalNew, store.SearchResult{
			File:       f,
			Distance:   0.0, // perfect lexical match
			FinalScore: 0.0, // will be set by caller if needed
		})
		seen[f.Path] = true
	}

	// Assemble: filename-new-matches first, then semantic results.
	result := make([]store.SearchResult, 0, len(lexicalNew)+len(semantic))
	result = append(result, lexicalNew...)
	result = append(result, semantic...)

	if len(result) > k {
		result = result[:k]
	}
	return result
}
