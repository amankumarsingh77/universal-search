package embedder

import "context"

// Embedder is the narrow interface used by indexing and search code to produce
// vector embeddings. Concrete implementations include GeminiEmbedder (live
// API) and FakeEmbedder (deterministic test double).
type Embedder interface {
	ModelID() string
	Dimensions() int
	EmbedBatch(ctx context.Context, inputs []ChunkInput) ([][]float32, error)
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
}
