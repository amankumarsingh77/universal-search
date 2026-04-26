package query

import (
	"io"
	"log/slog"
	"testing"

	"findo/internal/store"
)

// newTestStore returns an in-memory Store for cache tests.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := store.NewStore(":memory:", logger)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestCacheSchemaVersionConstant verifies the constant is set to 3 (REQ-013).
func TestCacheSchemaVersionConstant(t *testing.T) {
	if CacheSchemaVersion != 3 {
		t.Fatalf("expected CacheSchemaVersion=3, got %d", CacheSchemaVersion)
	}
}

// TestUpsertRecordsSchemaVersion verifies that Set writes schema_version=2 to the DB.
func TestUpsertRecordsSchemaVersion(t *testing.T) {
	s := newTestStore(t)
	cache := NewParsedQueryCache(s)

	spec := FilterSpec{SemanticQuery: "test photos"}
	if err := cache.Set("test photos", spec); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	db := store.DBForTesting(s)
	var ver int
	err := db.QueryRow(
		`SELECT schema_version FROM parsed_query_cache WHERE query_text_normalized = ?`,
		"test photos",
	).Scan(&ver)
	if err != nil {
		t.Fatalf("query schema_version: %v", err)
	}
	if ver != CacheSchemaVersion {
		t.Fatalf("expected schema_version=%d, got %d", CacheSchemaVersion, ver)
	}
}

// TestReadSkipsOldVersions verifies that a row at schema_version=0 is treated as a
// cache miss (REQ-014 / EDGE-010).
func TestReadSkipsOldVersions(t *testing.T) {
	s := newTestStore(t)
	cache := NewParsedQueryCache(s)

	// Seed a row directly at schema_version=0 (the old default).
	db := store.DBForTesting(s)
	_, err := db.Exec(
		`INSERT INTO parsed_query_cache (query_text_normalized, spec_json, schema_version, created_at, last_used_at)
		 VALUES (?, ?, 0, strftime('%s','now'), strftime('%s','now'))`,
		"old query", `{"semantic_query":"old query"}`,
	)
	if err != nil {
		t.Fatalf("seed old row: %v", err)
	}

	got, err := cache.Get("old query")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected cache miss for version-0 row, got non-nil spec: %+v", got)
	}
}

// TestReadWithMixedVersions verifies that when both v0 and v2 rows exist for the
// same key, the reader returns the v2 row (REQ-015 / EDGE-011).
func TestReadWithMixedVersions(t *testing.T) {
	s := newTestStore(t)
	cache := NewParsedQueryCache(s)

	db := store.DBForTesting(s)

	// Seed v0 row first (old cache entry).
	_, err := db.Exec(
		`INSERT INTO parsed_query_cache (query_text_normalized, spec_json, schema_version, created_at, last_used_at)
		 VALUES (?, ?, 0, strftime('%s','now'), strftime('%s','now'))`,
		"photos from yesterday", `{"semantic_query":"stale"}`,
	)
	if err != nil {
		t.Fatalf("seed v0 row: %v", err)
	}

	// Now write v2 row via the cache (INSERT OR REPLACE replaces on primary key).
	spec := FilterSpec{SemanticQuery: "photos from yesterday"}
	if err := cache.Set("photos from yesterday", spec); err != nil {
		t.Fatalf("Set v2 row: %v", err)
	}

	got, err := cache.Get("photos from yesterday")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got == nil {
		t.Fatal("expected a cache hit for v2 row, got nil")
	}
	if got.SemanticQuery != "photos from yesterday" {
		t.Fatalf("expected SemanticQuery=%q, got %q", "photos from yesterday", got.SemanticQuery)
	}
	if got.Source != SourceCache {
		t.Fatalf("expected Source=cache, got %q", got.Source)
	}
}

// TestEvictOldParsedQueryCache_StillWorks ensures EvictOldParsedQueryCache operates
// correctly after the schema_version column is added (EDGE-020).
func TestEvictOldParsedQueryCache_StillWorks(t *testing.T) {
	s := newTestStore(t)
	cache := NewParsedQueryCache(s)

	// Write a current entry.
	if err := cache.Set("recent query", FilterSpec{SemanticQuery: "recent"}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Seed an old entry by direct SQL with an old timestamp.
	db := store.DBForTesting(s)
	oldTS := int64(0) // Unix epoch — definitely older than 30 days
	_, err := db.Exec(
		`INSERT INTO parsed_query_cache (query_text_normalized, spec_json, schema_version, created_at, last_used_at)
		 VALUES (?, ?, 0, ?, ?)`,
		"ancient query", `{}`, oldTS, oldTS,
	)
	if err != nil {
		t.Fatalf("seed old entry: %v", err)
	}

	if err := s.EvictOldParsedQueryCache(); err != nil {
		t.Fatalf("EvictOldParsedQueryCache: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM parsed_query_cache`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 entry after eviction, got %d", count)
	}
}
