package search

import (
	"testing"

	"universal-search/internal/store"
	"universal-search/internal/vectorstore"
)

func TestEngine_SearchReturnsRankedResults(t *testing.T) {
	db, err := store.NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	idx := vectorstore.NewIndex()

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

	engine := New(db, idx)

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
	db, err := store.NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	idx := vectorstore.NewIndex()

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

	engine := New(db, idx)

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

func TestEngine_SearchEmptyIndex(t *testing.T) {
	db, err := store.NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	idx := vectorstore.NewIndex()

	engine := New(db, idx)

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
