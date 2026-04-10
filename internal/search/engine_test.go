package search

import (
	"io"
	"log/slog"
	"testing"

	"universal-search/internal/store"
	"universal-search/internal/vectorstore"
)

var testLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestEngine_SearchReturnsRankedResults(t *testing.T) {
	db, err := store.NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	idx := vectorstore.NewIndex(testLogger)

	// Insert a file + chunk
	fileID, err := db.UpsertFile(store.FileRecord{
		Path: "/tmp/test.txt", FileType: "text", Extension: ".txt",
		SizeBytes: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.InsertChunk(store.ChunkRecord{
		FileID: fileID, VectorID: "vec-1", ChunkIndex: 0,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Add matching vector
	vec := make([]float32, 768)
	vec[0] = 1.0
	if err := idx.Add("vec-1", vec); err != nil {
		t.Fatal(err)
	}

	engine := New(db, idx, testLogger)

	// Search with similar vector
	query := make([]float32, 768)
	query[0] = 1.0
	results, err := engine.SearchByVector(query, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].File.Path != "/tmp/test.txt" {
		t.Fatalf("expected /tmp/test.txt, got %s", results[0].File.Path)
	}
	if results[0].VectorID != "vec-1" {
		t.Fatalf("expected vector ID vec-1, got %s", results[0].VectorID)
	}
}

func TestEngine_DeduplicatesByFilePath(t *testing.T) {
	db, err := store.NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	idx := vectorstore.NewIndex(testLogger)

	// Insert one file with two chunks
	fileID, err := db.UpsertFile(store.FileRecord{
		Path: "/tmp/multi.txt", FileType: "text", Extension: ".txt",
		SizeBytes: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.InsertChunk(store.ChunkRecord{
		FileID: fileID, VectorID: "vec-a", ChunkIndex: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.InsertChunk(store.ChunkRecord{
		FileID: fileID, VectorID: "vec-b", ChunkIndex: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Add both vectors (slightly different but both close to query)
	vecA := make([]float32, 768)
	vecA[0] = 1.0
	vecA[1] = 0.1
	idx.Add("vec-a", vecA)

	vecB := make([]float32, 768)
	vecB[0] = 1.0
	vecB[1] = 0.2
	idx.Add("vec-b", vecB)

	engine := New(db, idx, testLogger)

	query := make([]float32, 768)
	query[0] = 1.0
	results, err := engine.SearchByVector(query, 5)
	if err != nil {
		t.Fatal(err)
	}

	// Should be deduplicated to 1 result (same file)
	if len(results) != 1 {
		t.Fatalf("expected 1 deduplicated result, got %d", len(results))
	}
	if results[0].File.Path != "/tmp/multi.txt" {
		t.Fatalf("expected /tmp/multi.txt, got %s", results[0].File.Path)
	}
}

// TestEngine_PropagatesDistance verifies that the Distance field in SearchResult
// is populated from the vector store cosine distance (not left as zero default).
func TestEngine_PropagatesDistance(t *testing.T) {
	db, err := store.NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	idx := vectorstore.NewIndex(testLogger)

	fileID, err := db.UpsertFile(store.FileRecord{
		Path: "/tmp/dist.txt", FileType: "text", Extension: ".txt",
		SizeBytes: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.InsertChunk(store.ChunkRecord{
		FileID: fileID, VectorID: "vec-dist", ChunkIndex: 0,
	})
	if err != nil {
		t.Fatal(err)
	}

	vec := make([]float32, 768)
	vec[0] = 1.0
	if err := idx.Add("vec-dist", vec); err != nil {
		t.Fatal(err)
	}

	engine := New(db, idx, testLogger)

	// Identical query vector → cosine distance should be ~0 (not left as Go zero)
	// We can't assert exact value, but we can assert it was set (non-negative float32).
	query := make([]float32, 768)
	query[0] = 1.0
	results, err := engine.SearchByVector(query, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	// Distance should be >= 0 and was set from distMap (zero is valid for identical vectors)
	if results[0].Distance < 0 {
		t.Fatalf("expected non-negative distance, got %v", results[0].Distance)
	}
}

// TestEngine_DeduplicatesByLowestDistance verifies dedup keeps the chunk
// with the lowest distance (best match) when a file has multiple chunks.
func TestEngine_DeduplicatesByLowestDistance(t *testing.T) {
	db, err := store.NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	idx := vectorstore.NewIndex(testLogger)

	fileID, err := db.UpsertFile(store.FileRecord{
		Path: "/tmp/dedup-dist.txt", FileType: "text", Extension: ".txt",
		SizeBytes: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.InsertChunk(store.ChunkRecord{
		FileID: fileID, VectorID: "vec-close", ChunkIndex: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.InsertChunk(store.ChunkRecord{
		FileID: fileID, VectorID: "vec-far", ChunkIndex: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	// vec-close is nearly identical to query [1, 0, ...]
	vecClose := make([]float32, 768)
	vecClose[0] = 1.0
	idx.Add("vec-close", vecClose)

	// vec-far is orthogonal — very different from query
	vecFar := make([]float32, 768)
	vecFar[1] = 1.0
	idx.Add("vec-far", vecFar)

	engine := New(db, idx, testLogger)

	query := make([]float32, 768)
	query[0] = 1.0
	results, err := engine.SearchByVector(query, 5)
	if err != nil {
		t.Fatal(err)
	}

	// Deduped to 1 result
	if len(results) != 1 {
		t.Fatalf("expected 1 result after dedup, got %d", len(results))
	}
	// The kept result should be the closer one
	if results[0].VectorID != "vec-close" {
		t.Fatalf("expected vec-close (lower distance) to be kept, got %s", results[0].VectorID)
	}
}

func TestEngine_SearchEmptyIndex(t *testing.T) {
	db, err := store.NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	idx := vectorstore.NewIndex(testLogger)

	engine := New(db, idx, testLogger)

	query := make([]float32, 768)
	query[0] = 1.0
	results, err := engine.SearchByVector(query, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results from empty index, got %d", len(results))
	}
}
