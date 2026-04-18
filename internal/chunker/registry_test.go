package chunker

import (
	"os"
	"path/filepath"
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
