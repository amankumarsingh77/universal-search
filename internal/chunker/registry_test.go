package chunker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassify_KnownExtensions(t *testing.T) {
	tests := []struct {
		path     string
		expected FileType
	}{
		{"photo.jpg", TypeImage},
		{"photo.heic", TypeImage},
		{"animated.gif", TypeImage},
		{"clip.mp4", TypeVideo},
		{"clip.webm", TypeVideo},
		{"song.mp3", TypeAudio},
		{"song.flac", TypeAudio},
		{"doc.pdf", TypeDocument},
		{"slides.pptx", TypeDocument},
		{"sheet.xlsx", TypeDocument},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, tt.path)
			os.WriteFile(path, []byte("dummy"), 0o644)
			got := Classify(path)
			if got != tt.expected {
				t.Errorf("Classify(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestClassify_TextByContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("[database]\nhost = \"localhost\""), 0o644)

	got := Classify(path)
	if got != TypeText {
		t.Errorf("expected TypeText for .toml file, got %q", got)
	}
}

func TestClassify_UnknownBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mystery.dat")
	os.WriteFile(path, []byte{0x00, 0x01, 0x02, 0xff}, 0o644)

	got := Classify(path)
	if got != TypeUnknown {
		t.Errorf("expected TypeUnknown for binary .dat, got %q", got)
	}
}

func TestMimeType(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"photo.jpg", "image/jpeg"},
		{"animated.gif", "image/gif"},
		{"video.mp4", "video/mp4"},
		{"song.mp3", "audio/mpeg"},
		{"doc.pdf", "application/pdf"},
		{"photo.heic", "image/heic"},
		{"unknown.xyz", ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := MimeType(tt.path)
			if got != tt.expected {
				t.Errorf("MimeType(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestChunkFile_HEICTranscodes(t *testing.T) {
	// Stub out the real ffmpeg call so the test runs without a real HEIC file.
	fakeJPEG := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10} // JPEG magic bytes
	orig := transcodeHEIC
	transcodeHEIC = func(_ string) ([]byte, error) {
		return fakeJPEG, nil
	}
	defer func() { transcodeHEIC = orig }()

	dir := t.TempDir()
	path := filepath.Join(dir, "photo.heic")
	if err := os.WriteFile(path, []byte("dummy heic data"), 0o644); err != nil {
		t.Fatal(err)
	}

	chunks, ft, err := ChunkFile(path)
	if err != nil {
		t.Fatalf("ChunkFile HEIC error: %v", err)
	}
	if ft != TypeImage {
		t.Errorf("expected TypeImage, got %q", ft)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].MimeType != "image/jpeg" {
		t.Errorf("expected mime image/jpeg, got %q", chunks[0].MimeType)
	}
	if string(chunks[0].Content) != string(fakeJPEG) {
		t.Errorf("chunk content mismatch: got %v, want %v", chunks[0].Content, fakeJPEG)
	}
}

func TestChunkFile_HEICOversizedSkipsTranscode(t *testing.T) {
	orig := transcodeHEIC
	called := false
	transcodeHEIC = func(_ string) ([]byte, error) {
		called = true
		return []byte{0xff, 0xd8, 0xff}, nil
	}
	defer func() { transcodeHEIC = orig }()

	dir := t.TempDir()
	path := filepath.Join(dir, "huge.heic")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxBinarySize + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	chunks, ft, err := ChunkFile(path)
	if ft != TypeImage {
		t.Errorf("expected TypeImage, got %q", ft)
	}
	if err == nil {
		t.Fatal("expected file too large error, got nil")
	}
	if !strings.Contains(err.Error(), "file too large") {
		t.Fatalf("expected file too large error, got: %v", err)
	}
	if chunks != nil {
		t.Fatalf("expected nil chunks on error, got %d", len(chunks))
	}
	if called {
		t.Fatal("expected transcodeHEIC not to be called for oversized file")
	}
}
