// Package store provides SQLite-backed persistence for file and chunk metadata.
package store

import (
	"fmt"
	"strings"
)

// FilenameGlob returns files whose path matches a glob pattern.
//
// The pattern uses standard glob syntax: '*' matches any sequence of
// characters, '?' matches any single character. Literal '%' and '_'
// characters are escaped so they are not treated as SQL wildcards.
//
// Results are capped at limit. If limit <= 0, up to 50 results are returned.
func (s *Store) FilenameGlob(pattern string, limit int) ([]FileRecord, error) {
	if pattern == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	likePattern := globToLike(pattern)
	// ORDER BY modified_at DESC ensures predictable, recency-ordered results.
	// Spec EDGE-7 requires this ordering when the pattern is bare `*`; applying
	// it unconditionally also makes non-star glob results deterministic.
	rows, err := s.db.Query(
		`SELECT id, path, file_type, extension, size_bytes, modified_at, indexed_at, content_hash, thumbnail_path, basename, parent, stem`+
			` FROM files WHERE path LIKE ? ESCAPE '\' ORDER BY modified_at DESC LIMIT ?`,
		likePattern, limit)
	if err != nil {
		return nil, fmt.Errorf("filename glob query: %w", err)
	}
	defer rows.Close()

	var files []FileRecord
	for rows.Next() {
		var f FileRecord
		if err := rows.Scan(&f.ID, &f.Path, &f.FileType, &f.Extension, &f.SizeBytes,
			&f.ModifiedAt, &f.IndexedAt, &f.ContentHash, &f.ThumbnailPath,
			&f.Basename, &f.Parent, &f.Stem); err != nil {
			return nil, fmt.Errorf("scan glob row: %w", err)
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// globToLike translates a glob pattern to a SQL LIKE pattern.
//
// Conversion rules:
//  1. Escape backslash, then literal '%' and '_' so they are not misinterpreted.
//  2. Replace glob '*' with SQL '%'.
//  3. Replace glob '?' with SQL '_'.
func globToLike(pattern string) string {
	// Step 1: escape the backslash escape character itself first.
	pattern = strings.ReplaceAll(pattern, `\`, `\\`)
	// Step 2: escape literal SQL wildcard characters.
	pattern = strings.ReplaceAll(pattern, `%`, `\%`)
	pattern = strings.ReplaceAll(pattern, `_`, `\_`)
	// Step 3: convert glob wildcards to SQL wildcards.
	pattern = strings.ReplaceAll(pattern, `*`, `%`)
	pattern = strings.ReplaceAll(pattern, `?`, `_`)
	return pattern
}

// isGlob reports whether pattern contains any glob wildcard characters.
func isGlob(pattern string) bool {
	return strings.ContainsAny(pattern, "*?")
}
