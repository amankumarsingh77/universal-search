package embedder

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"math/rand"
)

// FakeEmbedder is a deterministic, network-free Embedder used in tests. Given
// the same input it always returns the same unit-length vector, so cosine
// similarity is meaningful.
type FakeEmbedder struct {
	model string
	dims  int
}

var _ Embedder = (*FakeEmbedder)(nil)

// NewFake constructs a FakeEmbedder with the given model id and dimensions.
func NewFake(model string, dims int) *FakeEmbedder {
	return &FakeEmbedder{model: model, dims: dims}
}

// ModelID returns the configured model identifier.
func (f *FakeEmbedder) ModelID() string { return f.model }

// Dimensions returns the configured embedding dimensionality.
func (f *FakeEmbedder) Dimensions() int { return f.dims }

// EmbedQuery returns the deterministic unit vector for text.
func (f *FakeEmbedder) EmbedQuery(_ context.Context, text string) ([]float32, error) {
	return f.vectorFor(text), nil
}

// EmbedBatch returns one deterministic unit vector per input, keyed by
// Title|Text|Data.
func (f *FakeEmbedder) EmbedBatch(_ context.Context, inputs []ChunkInput) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	out := make([][]float32, len(inputs))
	for i, c := range inputs {
		out[i] = f.vectorFor(c.Title + "|" + c.Text + "|" + string(c.Data))
	}
	return out, nil
}

func (f *FakeEmbedder) vectorFor(s string) []float32 {
	sum := sha256.Sum256([]byte(s))
	seed := int64(binary.LittleEndian.Uint64(sum[:8]))
	rng := rand.New(rand.NewSource(seed))
	v := make([]float32, f.dims)
	for i := range v {
		v[i] = float32(rng.NormFloat64())
	}
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return v
	}
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
	return v
}
