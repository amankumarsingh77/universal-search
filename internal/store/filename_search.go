// Package store provides SQLite-backed persistence for file and chunk metadata.
package store

import (
	"database/sql"
	"fmt"
	"strings"

	"findo/internal/apperr"
)

// HighlightRange is a byte-offset range within a string marking a matched
// substring. Start is inclusive, End is exclusive (UTF-8 byte offsets).
type HighlightRange struct {
	Start int
	End   int
}

// FilenameMatch is a single result returned by FilenameSearch.
type FilenameMatch struct {
	File       FileRecord
	Score      float64          // normalised [0, 1]; higher is better
	MatchKind  string           // "exact" | "substring" | "fuzzy"
	Highlights []HighlightRange // byte offsets within File.Basename
}

// highlightSentinelStart and highlightSentinelEnd are single-byte non-printing
// characters used as sentinel markers with FTS5 highlight(). They are stripped
// from the returned Basename string after offset extraction.
const (
	highlightSentinelStart = "\x02"
	highlightSentinelEnd   = "\x03"
)

// FilenameSearch queries the FTS5 index for filenames matching q.
//
// Result ordering:
//  1. Exact basename matches (basename = q) with Score 1.0 and MatchKind "exact".
//  2. FTS5 MATCH results ordered by bm25 with column weights
//     (basename:10, parent:3, stem:5, ext:2, path:0).
//
// BM25 raw scores from SQLite are negative (more negative = better rank).
// They are normalised to [0, 1] via 1 / (1 + max(0, -raw)).
//
// highlight() with sentinel bytes is used on the basename column to compute
// byte-offset highlight ranges. The sentinels are stripped before the basename
// is stored in FilenameMatch.File.Basename.
//
// An empty query returns an empty slice and nil error.
// If FTS5 is unavailable (FilenameFTSAvailable() == false) this method returns
// an empty slice and nil error — callers should use SearchFilenameContains.
func (s *Store) FilenameSearch(query string, limit int) ([]FilenameMatch, error) {
	if query == "" {
		return nil, nil
	}
	if !s.filenameFTSReady {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}

	// Phase 1 — exact basename match.
	exact, err := s.exactBasenameSearch(query, limit)
	if err != nil {
		return nil, apperr.Wrap("ERR_FILENAME_SEARCH_FAILED", "filename exact search failed", err)
	}

	seen := make(map[int64]bool, len(exact))
	results := make([]FilenameMatch, 0, limit)
	for _, m := range exact {
		seen[m.File.ID] = true
		results = append(results, m)
	}

	remaining := limit - len(results)
	if remaining <= 0 {
		return results, nil
	}

	// Phase 2 — FTS5 MATCH.
	fts, err := s.ftsSearch(query, remaining+len(exact)) // fetch extra to allow dedup
	if err != nil {
		return nil, apperr.Wrap("ERR_FILENAME_SEARCH_FAILED", "filename FTS search failed", err)
	}

	for _, m := range fts {
		if seen[m.File.ID] {
			continue
		}
		if len(results) >= limit {
			break
		}
		seen[m.File.ID] = true
		results = append(results, m)
	}

	return results, nil
}

// exactBasenameSearch returns files whose basename exactly equals query (case-insensitive).
func (s *Store) exactBasenameSearch(query string, limit int) ([]FilenameMatch, error) {
	rows, err := s.db.Query(`
		SELECT f.id, f.path, f.file_type, f.extension, f.size_bytes,
		       f.modified_at, f.indexed_at, f.content_hash, f.thumbnail_path,
		       f.basename, f.parent, f.stem
		FROM files f
		WHERE lower(f.basename) = lower(?)
		LIMIT ?
	`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("exact basename query: %w", err)
	}
	defer rows.Close()

	var out []FilenameMatch
	for rows.Next() {
		var f FileRecord
		if err := rows.Scan(
			&f.ID, &f.Path, &f.FileType, &f.Extension, &f.SizeBytes,
			&f.ModifiedAt, &f.IndexedAt, &f.ContentHash, &f.ThumbnailPath,
			&f.Basename, &f.Parent, &f.Stem,
		); err != nil {
			return nil, fmt.Errorf("scan exact row: %w", err)
		}
		// Highlight the full basename as a single range.
		out = append(out, FilenameMatch{
			File:      f,
			Score:     1.0,
			MatchKind: "exact",
			Highlights: []HighlightRange{
				{Start: 0, End: len(f.Basename)},
			},
		})
	}
	return out, rows.Err()
}

// ftsSearch queries filename_search using FTS5 MATCH and returns ranked results.
func (s *Store) ftsSearch(query string, limit int) ([]FilenameMatch, error) {
	escaped := escapeFTS5(query)

	// Column weights: basename=10, parent=3, stem=5, ext=2, path=0.
	// highlight() is applied to column 0 (basename) with sentinel markers.
	rows, err := s.db.Query(`
		SELECT
		    f.id, f.path, f.file_type, f.extension, f.size_bytes,
		    f.modified_at, f.indexed_at, f.content_hash, f.thumbnail_path,
		    f.parent, f.stem,
		    bm25(filename_search, 10.0, 3.0, 5.0, 2.0, 0.0, 0.0) AS rank,
		    highlight(filename_search, 0, ?, ?)                    AS hl_basename
		FROM filename_search
		JOIN files f ON f.id = filename_search.file_id
		WHERE filename_search MATCH ?
		ORDER BY rank
		LIMIT ?
	`, highlightSentinelStart, highlightSentinelEnd, escaped, limit)
	if err != nil {
		if isFTSNoMatch(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("fts query: %w", err)
	}
	defer rows.Close()

	var out []FilenameMatch
	for rows.Next() {
		var f FileRecord
		var rawRank float64
		var hlBasename string

		if err := rows.Scan(
			&f.ID, &f.Path, &f.FileType, &f.Extension, &f.SizeBytes,
			&f.ModifiedAt, &f.IndexedAt, &f.ContentHash, &f.ThumbnailPath,
			&f.Parent, &f.Stem,
			&rawRank, &hlBasename,
		); err != nil {
			return nil, fmt.Errorf("scan fts row: %w", err)
		}

		// Normalise BM25: raw is negative (more negative = better).
		// score = 1 / (1 + max(0, -raw))
		negRaw := -rawRank
		if negRaw < 0 {
			negRaw = 0
		}
		score := 1.0 / (1.0 + negRaw)

		// Extract highlight ranges from the sentinel-annotated basename.
		highlights, cleanBasename := extractHighlights(hlBasename)

		f.Basename = cleanBasename

		out = append(out, FilenameMatch{
			File:       f,
			Score:      score,
			MatchKind:  "substring",
			Highlights: highlights,
		})
	}
	return out, rows.Err()
}

// extractHighlights parses a string containing highlightSentinelStart /
// highlightSentinelEnd byte markers, returns the highlight ranges (byte offsets
// in the cleaned string) and the cleaned string with markers removed.
func extractHighlights(marked string) ([]HighlightRange, string) {
	var ranges []HighlightRange
	var sb strings.Builder
	sb.Grow(len(marked))

	i := 0
	for i < len(marked) {
		if strings.HasPrefix(marked[i:], highlightSentinelStart) {
			start := sb.Len()
			i += len(highlightSentinelStart)
			// Collect until end sentinel.
			for i < len(marked) && !strings.HasPrefix(marked[i:], highlightSentinelEnd) {
				sb.WriteByte(marked[i])
				i++
			}
			end := sb.Len()
			if i < len(marked) {
				i += len(highlightSentinelEnd)
			}
			if end > start {
				ranges = append(ranges, HighlightRange{Start: start, End: end})
			}
		} else {
			sb.WriteByte(marked[i])
			i++
		}
	}
	return ranges, sb.String()
}

// escapeFTS5 wraps a query string for safe use as an FTS5 MATCH phrase.
//
// The trigram tokenizer handles substring matching natively, so we convert the
// query into a quoted phrase: lowercase the input, double any internal double
// quotes, and surround with double quotes. This prevents FTS5 special characters
// (*, (, ), :) from being interpreted as operators.
func escapeFTS5(q string) string {
	q = strings.ToLower(q)
	// Double any embedded double-quote characters.
	q = strings.ReplaceAll(q, `"`, `""`)
	return `"` + q + `"`
}

// isFTSNoMatch returns true when err is a SQLite error indicating that the FTS
// query matched nothing or was syntactically invalid in a benign way. We treat
// these as empty-result rather than hard errors.
func isFTSNoMatch(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "fts5: syntax error") ||
		err == sql.ErrNoRows
}
