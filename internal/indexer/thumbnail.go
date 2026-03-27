package indexer

import (
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"

	_ "golang.org/x/image/webp"
)

const thumbnailSize = 80

// GenerateThumbnail creates a JPEG thumbnail for the given file.
// Returns the output path, or ("", nil) if no thumbnail is applicable.
func GenerateThumbnail(filePath, outputDir string, fileType string) (string, error) {
	hash := fmt.Sprintf("%x", filePath) // simple deterministic name
	name := hash
	if len(name) > 16 {
		name = name[:16]
	}
	outPath := filepath.Join(outputDir, name+".jpg")

	if _, err := os.Stat(outPath); err == nil {
		return outPath, nil // already exists
	}

	switch fileType {
	case "video":
		return outPath, generateVideoThumbnail(filePath, outPath)
	case "image":
		return outPath, generateImageThumbnail(filePath, outPath)
	default:
		return "", nil // no thumbnail for text/audio
	}
}

func generateVideoThumbnail(videoPath, outPath string) error {
	return exec.Command("ffmpeg",
		"-ss", "1",
		"-i", videoPath,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale=%d:-1", thumbnailSize),
		"-q:v", "8",
		"-y", outPath,
	).Run()
}

func generateImageThumbnail(imagePath, outPath string) error {
	f, err := os.Open(imagePath)
	if err != nil {
		return err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return fmt.Errorf("decode image: %w", err)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= thumbnailSize && h <= thumbnailSize {
		// Already small enough, just copy as JPEG
		return saveJPEG(img, outPath)
	}

	// Simple nearest-neighbor resize (no external dep)
	var newW, newH int
	if w > h {
		newW = thumbnailSize
		newH = h * thumbnailSize / w
	} else {
		newH = thumbnailSize
		newW = w * thumbnailSize / h
	}
	if newW == 0 {
		newW = 1
	}
	if newH == 0 {
		newH = 1
	}

	resized := image.NewRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			srcX := x * w / newW
			srcY := y * h / newH
			resized.Set(x, y, img.At(bounds.Min.X+srcX, bounds.Min.Y+srcY))
		}
	}
	return saveJPEG(resized, outPath)
}

func saveJPEG(img image.Image, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return jpeg.Encode(f, img, &jpeg.Options{Quality: 75})
}
