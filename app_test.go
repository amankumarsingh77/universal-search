package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"universal-search/internal/store"
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
