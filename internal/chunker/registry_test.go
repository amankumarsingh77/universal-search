package chunker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"findo/internal/apperr"
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
	// Inject a stub transcoder so the test runs without a real HEIC file.
	fakeJPEG := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10} // JPEG magic bytes
	reg := NewRegistry().WithHEICTranscoder(func(_ string) ([]byte, error) {
		return fakeJPEG, nil
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "photo.heic")
	if err := os.WriteFile(path, []byte("dummy heic data"), 0o644); err != nil {
		t.Fatal(err)
	}

	chunks, ft, err := reg.ChunkFile(path)
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
	called := false
	reg := NewRegistry().WithHEICTranscoder(func(_ string) ([]byte, error) {
		called = true
		return []byte{0xff, 0xd8, 0xff}, nil
	})

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

	chunks, ft, err := reg.ChunkFile(path)
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

// ---------------------------------------------------------------------------
// Phase 4: apperr code assertions for chunker failure sites — REQ-003
// ---------------------------------------------------------------------------

// TestChunkBinary_FileTooLarge_IsWrapped — EDGE-018, REQ-003
// ChunkBinary on a file exceeding maxBinarySize must return ERR_FILE_TOO_LARGE.
func TestChunkBinary_FileTooLarge_IsWrapped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxBinarySize + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	_, err = ChunkBinary(path, "application/octet-stream")
	if err == nil {
		t.Fatal("expected ERR_FILE_TOO_LARGE, got nil")
	}

	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error does not wrap *apperr.Error: %v", err)
	}
	if appErr.Code != apperr.ErrFileTooLarge.Code {
		t.Errorf("appErr.Code = %q, want %q", appErr.Code, apperr.ErrFileTooLarge.Code)
	}
	// errors.Is must also work via the Is() method.
	if !errors.Is(err, apperr.ErrFileTooLarge) {
		t.Error("errors.Is(err, apperr.ErrFileTooLarge) should be true")
	}
}

// TestChunkDocument_UnsupportedFormat_IsWrapped — REQ-003
// ChunkDocument on an unsupported extension must return ERR_UNSUPPORTED_FORMAT.
func TestChunkDocument_UnsupportedFormat_IsWrapped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.unknown")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ChunkDocument(path)
	if err == nil {
		t.Fatal("expected ERR_UNSUPPORTED_FORMAT, got nil")
	}

	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error does not wrap *apperr.Error: %v", err)
	}
	if appErr.Code != apperr.ErrUnsupportedFormat.Code {
		t.Errorf("appErr.Code = %q, want %q", appErr.Code, apperr.ErrUnsupportedFormat.Code)
	}
	if !errors.Is(err, apperr.ErrUnsupportedFormat) {
		t.Error("errors.Is(err, apperr.ErrUnsupportedFormat) should be true")
	}
}

// TestChunkFile_HEIC_ExtractionFailed_IsWrapped — EDGE-017, REQ-003
// A HEIC file where the transcoder returns an error must produce ERR_EXTRACTION_FAILED.
func TestChunkFile_HEIC_ExtractionFailed_IsWrapped(t *testing.T) {
	transcoderErr := fmt.Errorf("ffmpeg: no such codec")
	reg := NewRegistry().WithHEICTranscoder(func(_ string) ([]byte, error) {
		return nil, transcoderErr
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.heic")
	if err := os.WriteFile(path, []byte("fake heic data"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := reg.ChunkFile(path)
	if err == nil {
		t.Fatal("expected ERR_EXTRACTION_FAILED, got nil")
	}

	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error does not wrap *apperr.Error: %v", err)
	}
	if appErr.Code != apperr.ErrExtractionFailed.Code {
		t.Errorf("appErr.Code = %q, want %q", appErr.Code, apperr.ErrExtractionFailed.Code)
	}
	// Cause must still be reachable via errors.Is.
	if !errors.Is(err, transcoderErr) {
		t.Error("errors.Is(err, transcoderErr) should be true via unwrap chain")
	}
}

// TestChunkFile_HEIC_FileTooLarge_IsWrapped — EDGE-018, REQ-003
// An oversized HEIC file must return ERR_FILE_TOO_LARGE (via Registry.ChunkFile).
func TestChunkFile_HEIC_FileTooLarge_IsWrapped(t *testing.T) {
	reg := NewRegistry()

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
	f.Close()

	_, _, err = reg.ChunkFile(path)
	if err == nil {
		t.Fatal("expected ERR_FILE_TOO_LARGE, got nil")
	}

	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error does not wrap *apperr.Error: %v", err)
	}
	if appErr.Code != apperr.ErrFileTooLarge.Code {
		t.Errorf("appErr.Code = %q, want %q", appErr.Code, apperr.ErrFileTooLarge.Code)
	}
}

// TestChunkDocument_LibreOfficeMissing_IsExtractionFailed — EDGE-017, REQ-003
// When LibreOffice is missing and a legacy office file is given, ChunkDocument
// must return ERR_EXTRACTION_FAILED (Permanent classification).
func TestChunkDocument_LibreOfficeMissing_IsExtractionFailed(t *testing.T) {
	if HasLibreOffice() {
		t.Skip("LibreOffice is installed; cannot test missing-libreoffice path")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "report.doc")
	if err := os.WriteFile(path, []byte("fake doc content"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ChunkDocument(path)
	if err == nil {
		t.Fatal("expected ERR_EXTRACTION_FAILED for missing LibreOffice, got nil")
	}

	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error does not wrap *apperr.Error: %v", err)
	}
	if appErr.Code != apperr.ErrExtractionFailed.Code {
		t.Errorf("appErr.Code = %q, want %q", appErr.Code, apperr.ErrExtractionFailed.Code)
	}
	// Must be Permanent classification (EDGE-017).
	if c := apperr.Classify(err); c != apperr.ClassPermanent {
		t.Errorf("Classify = %q, want ClassPermanent", c)
	}
}

// TestChunkFile_TypeUnknown_ReturnsNil — EDGE-019, REQ-003
// ChunkFile on a TypeUnknown file must return (nil, TypeUnknown, nil).
// This is the "skip silently" path — not an error.
func TestChunkFile_TypeUnknown_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mystery.dat")
	if err := os.WriteFile(path, []byte{0x00, 0x01, 0x02, 0xff, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}

	chunks, ft, err := ChunkFile(path)
	if err != nil {
		t.Fatalf("EDGE-019: expected nil error for TypeUnknown, got: %v", err)
	}
	if ft != TypeUnknown {
		t.Errorf("expected TypeUnknown, got %q", ft)
	}
	if chunks != nil {
		t.Errorf("expected nil chunks for TypeUnknown, got %d chunks", len(chunks))
	}
}
