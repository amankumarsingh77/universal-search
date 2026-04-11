package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"universal-search/internal/indexer"
	"universal-search/internal/store"
	"universal-search/internal/vectorstore"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	s, err := store.NewStore(":memory:", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &App{store: s, logger: slog.Default()}
}

func TestSeedDefaultIgnorePatterns_SeedsOnEmptyDB(t *testing.T) {
	a := newTestApp(t)
	a.seedDefaultIgnorePatterns()

	patterns, err := a.GetIgnoredFolders()
	if err != nil {
		t.Fatalf("GetIgnoredFolders returned error: %v", err)
	}
	if len(patterns) != len(defaultIgnorePatterns) {
		t.Errorf("expected %d patterns, got %d", len(defaultIgnorePatterns), len(patterns))
	}
}

func TestSeedDefaultIgnorePatterns_SkipsIfPatternsExist(t *testing.T) {
	a := newTestApp(t)

	// Add one pattern before seeding.
	if err := a.store.AddExcludedPattern("mypattern"); err != nil {
		t.Fatal(err)
	}

	a.seedDefaultIgnorePatterns()

	patterns, err := a.GetIgnoredFolders()
	if err != nil {
		t.Fatalf("GetIgnoredFolders returned error: %v", err)
	}
	if len(patterns) != 1 {
		t.Errorf("expected 1 pattern (seeding should be skipped), got %d", len(patterns))
	}
}

func TestAddIgnoredFolder_EmptyPattern_ReturnsError(t *testing.T) {
	a := newTestApp(t)

	if err := a.AddIgnoredFolder(""); err == nil {
		t.Error("expected error for empty pattern, got nil")
	}

	if err := a.AddIgnoredFolder("  "); err == nil {
		t.Error("expected error for whitespace-only pattern, got nil")
	}
}

func TestAddIgnoredFolder_DuplicatePattern_Succeeds(t *testing.T) {
	a := newTestApp(t)

	if err := a.AddIgnoredFolder("node_modules"); err != nil {
		t.Fatalf("first add failed: %v", err)
	}
	if err := a.AddIgnoredFolder("node_modules"); err != nil {
		t.Fatalf("second add (duplicate) failed: %v", err)
	}

	patterns, err := a.GetIgnoredFolders()
	if err != nil {
		t.Fatalf("GetIgnoredFolders returned error: %v", err)
	}
	if len(patterns) != 1 {
		t.Errorf("expected 1 pattern after duplicate add, got %d", len(patterns))
	}
}

func TestRemoveIgnoredFolder_NonExistent_ReturnsNil(t *testing.T) {
	a := newTestApp(t)

	if err := a.RemoveIgnoredFolder("nonexistent"); err != nil {
		t.Errorf("expected nil for removing nonexistent pattern, got: %v", err)
	}
}

func TestGetFilePreview_ReturnsTextContent(t *testing.T) {
	a := newTestApp(t)

	f, err := os.CreateTemp(t.TempDir(), "preview-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	content := "hello world\nthis is a test file"
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()

	got, err := a.GetFilePreview(f.Name())
	if err != nil {
		t.Fatalf("GetFilePreview returned error: %v", err)
	}
	if got != content {
		t.Fatalf("expected %q, got %q", content, got)
	}
}

func TestGetFilePreview_TruncatesAtMaxBytes(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")

	// Write more than 8192 bytes of text
	data := make([]byte, 10000)
	for i := range data {
		data[i] = 'a'
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := a.GetFilePreview(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) > 8192 {
		t.Fatalf("expected at most 8192 bytes, got %d", len(got))
	}
}

func TestGetFilePreview_RejectsBinaryFile(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "binary.bin")

	// Write bytes with a null byte (binary)
	data := []byte{'h', 'e', 'l', 'l', 'o', 0, 'w', 'o', 'r', 'l', 'd'}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := a.GetFilePreview(path)
	if err == nil {
		t.Fatal("expected error for binary file, got nil")
	}
}

func TestGetFilePreview_RejectsNonExistentFile(t *testing.T) {
	a := newTestApp(t)

	_, err := a.GetFilePreview("/nonexistent/path/file.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestGetFilePreview_RejectsEmptyFile(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")

	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := a.GetFilePreview(path)
	if err == nil {
		t.Fatal("expected error for empty file, got nil")
	}
}

func TestGetFilePreview_RejectsInvalidUTF8(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.txt")

	// Write invalid UTF-8 sequence (not a null byte, so passes binary check first)
	data := []byte{0xff, 0xfe, 'h', 'e', 'l', 'l', 'o'}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := a.GetFilePreview(path)
	if err == nil {
		t.Fatal("expected error for invalid UTF-8, got nil")
	}
}

// TestSearchResultDTO_HasScoreField is a compile-time check that Score field exists.
func TestSearchResultDTO_HasScoreField(t *testing.T) {
	dto := SearchResultDTO{
		Score: 0.95,
	}
	if dto.Score != 0.95 {
		t.Fatalf("expected Score 0.95, got %v", dto.Score)
	}
}

// newTestPipeline returns a minimal Pipeline wired to the given store.
// The pipeline has no embedder (nil) — sufficient for SubmitFolder which only
// enqueues work; actual file processing would fail but is not exercised here.
func newTestPipeline(t *testing.T, s *store.Store) *indexer.Pipeline {
	t.Helper()
	idx := vectorstore.NewIndex(slog.Default())
	p := indexer.NewPipeline(s, idx, nil, t.TempDir(), slog.Default(), nil)
	t.Cleanup(func() { p.Stop() })
	return p
}

// TestReindexFolder_NilStore — REQ-001
// When the store is nil ReindexFolder must return without panicking.
func TestReindexFolder_NilStore(t *testing.T) {
	a := &App{store: nil, pipeline: nil, logger: slog.Default()}
	// Should not panic.
	a.ReindexFolder("/some/path")
}

// TestReindexFolder_NilPipeline — REQ-001
// When the pipeline is nil ReindexFolder must return without panicking.
func TestReindexFolder_NilPipeline(t *testing.T) {
	s, err := store.NewStore(":memory:", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	a := &App{store: s, pipeline: nil, logger: slog.Default()}
	// Should not panic.
	a.ReindexFolder("/some/path")
}

// TestReindexFolder_Success — REQ-001, REQ-002
// Happy path: store has patterns, pipeline is valid — must not panic.
func TestReindexFolder_Success(t *testing.T) {
	s, err := store.NewStore(":memory:", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Seed a couple of exclude patterns.
	for _, p := range []string{"node_modules", ".git"} {
		if err := s.AddExcludedPattern(p); err != nil {
			t.Fatal(err)
		}
	}

	p := newTestPipeline(t, s)
	a := &App{store: s, pipeline: p, logger: slog.Default()}
	// Should not panic and should submit the folder.
	a.ReindexFolder("/any/folder")
}

// TestReindexFolder_StoreError_DoesNotPanic — EDGE-004
// When GetExcludedPatterns fails (closed store) ReindexFolder must not panic.
func TestReindexFolder_StoreError_DoesNotPanic(t *testing.T) {
	s, err := store.NewStore(":memory:", slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	p := newTestPipeline(t, s)
	a := &App{store: s, pipeline: p, logger: slog.Default()}

	// Close the store so all DB calls return an error.
	s.Close()
	// Should not panic.
	a.ReindexFolder("/any/folder")
}

// TestReindexFolder_NonExistentPath — REQ-006 / EDGE-003
// Passing a path that does not exist on disk must not panic —
// the pipeline handles non-existent paths gracefully during processing.
func TestReindexFolder_NonExistentPath(t *testing.T) {
	s, err := store.NewStore(":memory:", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	p := newTestPipeline(t, s)
	a := &App{store: s, pipeline: p, logger: slog.Default()}
	// Should not panic.
	a.ReindexFolder("/does/not/exist/at/all")
}
