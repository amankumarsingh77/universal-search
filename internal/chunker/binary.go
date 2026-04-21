package chunker

import (
	"fmt"
	"os"
)

// maxBinarySize is the maximum file size (50 MB) that ChunkBinary will load
// into memory. Files larger than this are skipped to prevent OOM.
const maxBinarySize = 50 * 1024 * 1024

// ChunkBinary reads a binary file into a single Chunk, rejecting files above the size cap.
func ChunkBinary(filePath, mimeType string) ([]Chunk, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("stat binary file: %w", err)
	}
	if info.Size() > maxBinarySize {
		return nil, fmt.Errorf("file too large (%d bytes, max %d): %s", info.Size(), maxBinarySize, filePath)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read binary file: %w", err)
	}
	return []Chunk{{
		Content:  data,
		MimeType: mimeType,
		Index:    0,
	}}, nil
}
