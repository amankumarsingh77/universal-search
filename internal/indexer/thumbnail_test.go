package indexer

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateImageThumbnail(t *testing.T) {
	dir := t.TempDir()

	// Create a 200x100 test PNG image
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	srcPath := filepath.Join(dir, "test.png")
	f, err := os.Create(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	outPath, err := GenerateThumbnail(srcPath, dir, "image")
	if err != nil {
		t.Fatalf("GenerateThumbnail failed: %v", err)
	}
	if outPath == "" {
		t.Fatal("expected non-empty output path")
	}

	// Verify the thumbnail file exists and is a valid JPEG
	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("thumbnail file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("thumbnail file is empty")
	}

	// Decode and check dimensions
	thumbF, err := os.Open(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer thumbF.Close()
	thumbImg, _, err := image.Decode(thumbF)
	if err != nil {
		t.Fatalf("failed to decode thumbnail: %v", err)
	}
	bounds := thumbImg.Bounds()
	if bounds.Dx() > thumbnailSize || bounds.Dy() > thumbnailSize {
		t.Errorf("thumbnail too large: %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestGenerateThumbnail_TextReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	outPath, err := GenerateThumbnail("/some/file.txt", dir, "text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outPath != "" {
		t.Errorf("expected empty path for text files, got %q", outPath)
	}
}
