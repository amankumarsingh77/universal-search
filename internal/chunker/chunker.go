package chunker

// Chunk represents a single extractable unit of content from a file.
type Chunk struct {
	Content   []byte
	Text      string
	MimeType  string
	StartTime float64
	EndTime   float64
	PageStart int
	PageEnd   int
	Index     int
}

// Strategy produces chunks from a file at the given path.
type Strategy func(filePath string) ([]Chunk, error)
