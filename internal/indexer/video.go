package indexer

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// ExtractPreviewClip writes a short ffmpeg clip centered on the given timestamp.
func ExtractPreviewClip(videoPath, outputPath string, timestamp, duration float64) error {
	if _, err := os.Stat(videoPath); err != nil {
		return fmt.Errorf("video file not found: %w", err)
	}
	startTime := timestamp - duration/2
	if startTime < 0 {
		startTime = 0
	}
	err := exec.Command("ffmpeg",
		"-ss", strconv.FormatFloat(startTime, 'f', 2, 64),
		"-i", videoPath,
		"-t", strconv.FormatFloat(duration, 'f', 2, 64),
		"-c", "copy",
		"-an",
		"-y", outputPath,
	).Run()
	if err == nil {
		return nil
	}
	return exec.Command("ffmpeg",
		"-ss", strconv.FormatFloat(startTime, 'f', 2, 64),
		"-i", videoPath,
		"-t", strconv.FormatFloat(duration, 'f', 2, 64),
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-an",
		"-y", outputPath,
	).Run()
}
