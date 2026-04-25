package chunker

import (
	"fmt"
	"os"
	"unicode/utf8"
)

const (
	sniffSize        = 8192
	maxTextBytes     = 8000
	textChunkSize    = 2000
	textChunkOverlap = 200
)

// IsTextFile reports whether the file appears to be valid UTF-8 text (no NUL bytes).
func IsTextFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	buf := make([]byte, sniffSize)
	n, err := f.Read(buf)
	if n == 0 {
		return false, err
	}
	buf = buf[:n]

	for _, b := range buf {
		if b == 0 {
			return false, nil
		}
	}
	return utf8.Valid(buf), nil
}

// ChunkText splits a text file's contents into overlapping chunks.
func ChunkText(filePath string) ([]Chunk, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return chunkString(string(data), 0), nil
}

// chunkString splits text into overlapping chunks starting at the given index.
func chunkString(content string, startIndex int) []Chunk {
	if len(content) == 0 {
		return nil
	}

	if len(content) <= textChunkSize {
		return []Chunk{{Text: content, Index: startIndex}}
	}

	var chunks []Chunk
	idx := startIndex
	for start := 0; start < len(content); {
		end := start + textChunkSize
		if end > len(content) {
			end = len(content)
		}

		chunks = append(chunks, Chunk{
			Text:  content[start:end],
			Index: idx,
		})
		idx++

		start = end - textChunkOverlap
		if start >= len(content) || end >= len(content) {
			break
		}
	}

	return chunks
}
