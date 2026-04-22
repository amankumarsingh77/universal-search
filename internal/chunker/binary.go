package chunker

import (
	"fmt"
	"os"

	"findo/internal/apperr"
)

// maxBinarySize is the maximum file size (50 MB) that ChunkBinary will load
// into memory. Files larger than this are skipped to prevent OOM.
const maxBinarySize = 50 * 1024 * 1024

// ChunkBinary reads a binary file into a single Chunk, rejecting files above the size cap.
func ChunkBinary(filePath, mimeType string) ([]Chunk, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, apperr.Wrap(apperr.ErrFileUnreadable.Code, "cannot stat binary file", err)
	}
	if info.Size() > maxBinarySize {
		return nil, apperr.Wrap(apperr.ErrFileTooLarge.Code,
			fmt.Sprintf("file too large (%d bytes, max %d)", info.Size(), maxBinarySize), nil)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, apperr.Wrap(apperr.ErrFileUnreadable.Code, "cannot read binary file", err)
	}
	return []Chunk{{
		Content:  data,
		MimeType: mimeType,
		Index:    0,
	}}, nil
}
