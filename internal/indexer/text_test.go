package indexer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractText_PlainFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("Hello world, this is a test file."), 0o644)

	content, err := ExtractText(path)
	if err != nil {
		t.Fatalf("ExtractText failed: %v", err)
	}
	if content != "Hello world, this is a test file." {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestExtractText_TruncatesLargeFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")
	// Create a file larger than maxTextBytes
	data := make([]byte, maxTextBytes+1000)
	for i := range data {
		data[i] = 'A'
	}
	os.WriteFile(path, data, 0o644)

	content, err := ExtractText(path)
	if err != nil {
		t.Fatalf("ExtractText failed: %v", err)
	}
	if len(content) > maxTextBytes {
		t.Fatalf("content should be truncated to %d, got %d", maxTextBytes, len(content))
	}
}

func TestExtractText_PDFReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pdf")
	os.WriteFile(path, []byte("%PDF-1.4 fake pdf content"), 0o644)

	content, err := ExtractText(path)
	if err != nil {
		t.Fatalf("ExtractText failed: %v", err)
	}
	if content != "" {
		t.Fatalf("expected empty content for PDF, got %q", content)
	}
}
