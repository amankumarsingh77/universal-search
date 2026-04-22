package chunker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"findo/internal/apperr"
)

const (
	videoChunkLen  = 30.0
	videoOverlap   = 5.0
)

type videoTimeRange struct {
	Start float64
	End   float64
	Index int
}

// GetVideoDuration returns the duration in seconds of a video file via ffprobe.
func GetVideoDuration(path string) (float64, error) {
	out, err := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	).Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w", err)
	}
	return strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
}

func calculateTimeRanges(duration, chunkLen, overlap float64) []videoTimeRange {
	var ranges []videoTimeRange
	step := chunkLen - overlap
	i := 0
	for start := 0.0; start < duration; start += step {
		end := start + chunkLen
		if end > duration {
			end = duration
		}
		ranges = append(ranges, videoTimeRange{Start: start, End: end, Index: i})
		i++
		if end >= duration {
			break
		}
	}
	return ranges
}

func isStillFrame(chunkPath string) (bool, error) {
	duration, err := GetVideoDuration(chunkPath)
	if err != nil {
		return false, err
	}

	tmpDir, err := os.MkdirTemp("", "stillcheck-*")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(tmpDir)

	positions := []float64{duration * 0.25, duration * 0.5, duration * 0.75}
	var sizes []int64

	for i, pos := range positions {
		outPath := filepath.Join(tmpDir, fmt.Sprintf("frame_%d.jpg", i))
		err := exec.Command("ffmpeg",
			"-ss", strconv.FormatFloat(pos, 'f', 2, 64),
			"-i", chunkPath,
			"-frames:v", "1",
			"-q:v", "5",
			"-y", outPath,
		).Run()
		if err != nil {
			return false, fmt.Errorf("extract frame %d: %w", i, err)
		}
		info, err := os.Stat(outPath)
		if err != nil {
			return false, err
		}
		sizes = append(sizes, info.Size())
	}

	for i := 1; i < len(sizes); i++ {
		ratio := float64(min(sizes[0], sizes[i])) / float64(max(sizes[0], sizes[i]))
		if ratio < 0.995 {
			return false, nil
		}
	}
	return true, nil
}

func preprocessChunk(inputPath, outputPath string) error {
	return exec.Command("ffmpeg",
		"-i", inputPath,
		"-vf", "scale=-2:480",
		"-r", "5",
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-an",
		"-y", outputPath,
	).Run()
}

// ChunkVideo splits a video into overlapping 480p/5fps clips ready for embedding.
func ChunkVideo(filePath string) ([]Chunk, error) {
	if _, err := os.Stat(filePath); err != nil {
		return nil, apperr.Wrap(apperr.ErrFileUnreadable.Code, "video file not found", err)
	}

	duration, err := GetVideoDuration(filePath)
	if err != nil {
		return nil, apperr.Wrap(apperr.ErrExtractionFailed.Code, "failed to get video duration", err)
	}

	tmpDir, err := os.MkdirTemp("", "vidchunk-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	ranges := calculateTimeRanges(duration, videoChunkLen, videoOverlap)
	var chunks []Chunk

	for _, r := range ranges {
		chunkPath := filepath.Join(tmpDir, fmt.Sprintf("chunk_%03d.mp4", r.Index))
		args := []string{
			"-ss", strconv.FormatFloat(r.Start, 'f', -1, 64),
			"-i", filePath,
			"-t", strconv.FormatFloat(r.End-r.Start, 'f', -1, 64),
			"-c", "copy",
			"-y", chunkPath,
		}
		if err := exec.Command("ffmpeg", args...).Run(); err != nil {
			return nil, apperr.Wrap(apperr.ErrExtractionFailed.Code, fmt.Sprintf("ffmpeg chunk %d failed", r.Index), err)
		}

		still, err := isStillFrame(chunkPath)
		if err == nil && still {
			continue
		}

		preprocessed := filepath.Join(tmpDir, fmt.Sprintf("pre_%03d.mp4", r.Index))
		if err := preprocessChunk(chunkPath, preprocessed); err != nil {
			continue
		}

		data, err := os.ReadFile(preprocessed)
		if err != nil {
			continue
		}

		chunks = append(chunks, Chunk{
			Content:   data,
			MimeType:  "video/mp4",
			StartTime: r.Start,
			EndTime:   r.End,
			Index:     r.Index,
		})
	}

	return chunks, nil
}
