package vectorstore

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

var testLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestVectorIndex_AddAndSearch(t *testing.T) {
	idx := NewDefaultIndex(testLogger)

	vec1 := make([]float32, 768)
	vec1[0] = 1.0
	vec2 := make([]float32, 768)
	vec2[1] = 1.0
	vec3 := make([]float32, 768)
	vec3[0] = 0.9
	vec3[1] = 0.1

	err := idx.Add("id-1", vec1)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	idx.Add("id-2", vec2)
	idx.Add("id-3", vec3)

	query := make([]float32, 768)
	query[0] = 1.0
	results, err := idx.Search(query, 2)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "id-1" {
		t.Fatalf("expected id-1 as top result, got %s", results[0].ID)
	}
}

func TestVectorIndex_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.hnsw")

	idx := NewDefaultIndex(testLogger)
	vec := make([]float32, 768)
	vec[0] = 1.0
	idx.Add("id-1", vec)

	err := idx.Save(path)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if _, err := os.Stat(path + ".graph"); os.IsNotExist(err) {
		t.Fatal("graph file not created")
	}

	idx2, err := LoadIndex(path, testLogger)
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}

	query := make([]float32, 768)
	query[0] = 1.0
	results, err := idx2.Search(query, 1)
	if err != nil {
		t.Fatalf("Search on loaded index failed: %v", err)
	}
	if results[0].ID != "id-1" {
		t.Fatalf("expected id-1, got %s", results[0].ID)
	}
}

func TestVectorIndex_Delete(t *testing.T) {
	idx := NewDefaultIndex(testLogger)
	vec := make([]float32, 768)
	vec[0] = 1.0
	idx.Add("id-1", vec)

	deleted := idx.Delete("id-1")
	if !deleted {
		t.Fatal("expected deletion to succeed")
	}

	deleted = idx.Delete("nonexistent")
	if deleted {
		t.Fatal("expected deletion of nonexistent to fail")
	}
}

// REQ-010: Has() returns true after Add, false for unknown ID, false after Delete.
func TestVectorIndex_Has_AfterAdd(t *testing.T) {
	idx := NewDefaultIndex(testLogger)
	vec := make([]float32, 768)
	vec[0] = 1.0

	if idx.Has("id-1") {
		t.Fatal("Has should return false before Add")
	}

	idx.Add("id-1", vec)
	if !idx.Has("id-1") {
		t.Fatal("Has should return true after Add")
	}
}

// REQ-010: Has() returns false for unknown ID.
func TestVectorIndex_Has_UnknownID(t *testing.T) {
	idx := NewDefaultIndex(testLogger)
	if idx.Has("does-not-exist") {
		t.Fatal("Has should return false for unknown ID")
	}
}

// REQ-010: Has() returns false after Delete.
func TestVectorIndex_Has_AfterDelete(t *testing.T) {
	idx := NewDefaultIndex(testLogger)
	vec := make([]float32, 768)
	vec[0] = 1.0
	idx.Add("id-1", vec)

	idx.Delete("id-1")
	if idx.Has("id-1") {
		t.Fatal("Has should return false after Delete")
	}
}

// EDGE-014: Has() returns true for in-memory vector before Save() to disk.
func TestVectorIndex_Has_BeforeSave(t *testing.T) {
	idx := NewDefaultIndex(testLogger)
	vec := make([]float32, 768)
	vec[0] = 1.0
	idx.Add("id-inmem", vec)

	// No Save called — vector only exists in memory.
	if !idx.Has("id-inmem") {
		t.Fatal("Has should return true for in-memory vector not yet saved to disk")
	}
}

// REQ-009: Save() creates .graph and .map; no .tmp files remain after save completes.
func TestVectorIndex_Save_AtomicNoTmpFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.hnsw")

	idx := NewDefaultIndex(testLogger)
	vec := make([]float32, 768)
	vec[0] = 1.0
	idx.Add("id-1", vec)

	if err := idx.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Final files must exist.
	if _, err := os.Stat(path + ".graph"); os.IsNotExist(err) {
		t.Fatal(".graph file must exist after Save")
	}
	if _, err := os.Stat(path + ".map"); os.IsNotExist(err) {
		t.Fatal(".map file must exist after Save")
	}

	// Tmp files must NOT exist after Save completes.
	if _, err := os.Stat(path + ".graph.tmp"); !os.IsNotExist(err) {
		t.Fatal(".graph.tmp must not exist after Save completes")
	}
	if _, err := os.Stat(path + ".map.tmp"); !os.IsNotExist(err) {
		t.Fatal(".map.tmp must not exist after Save completes")
	}
}

// EDGE-005: LoadIndex with corrupt .graph returns error.
func TestLoadIndex_CorruptGraph(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.hnsw")

	// Write garbage to .graph file.
	if err := os.WriteFile(path+".graph", []byte("not a valid graph"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Write a minimal .map file.
	if err := os.WriteFile(path+".map", []byte("0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := LoadIndex(path, testLogger)
	if err == nil {
		t.Fatal("LoadIndex should return error for corrupt .graph")
	}
}

// EDGE-006: LoadIndex with missing .map returns error.
func TestLoadIndex_MissingMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nomap.hnsw")

	// Create and save a valid index to get a proper .graph file.
	idx := NewDefaultIndex(testLogger)
	vec := make([]float32, 768)
	vec[0] = 1.0
	idx.Add("id-1", vec)
	if err := idx.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Remove the .map file.
	if err := os.Remove(path + ".map"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	_, err := LoadIndex(path, testLogger)
	if err == nil {
		t.Fatal("LoadIndex should return error when .map is missing")
	}
}

// EDGE-007: LoadIndex with .tmp files alongside valid .graph/.map loads the valid files.
func TestLoadIndex_IgnoresTmpFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.hnsw")

	// Save a valid index.
	idx := NewDefaultIndex(testLogger)
	vec := make([]float32, 768)
	vec[0] = 1.0
	idx.Add("id-1", vec)
	if err := idx.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Place stale/corrupt .tmp files alongside.
	if err := os.WriteFile(path+".graph.tmp", []byte("stale garbage"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(path+".map.tmp", []byte("stale garbage"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// LoadIndex should load the valid .graph/.map and succeed.
	idx2, err := LoadIndex(path, testLogger)
	if err != nil {
		t.Fatalf("LoadIndex should succeed ignoring .tmp files: %v", err)
	}
	if !idx2.Has("id-1") {
		t.Fatal("loaded index should contain id-1")
	}
}

// Regression: adding many vectors must not fail with "no nodes found in
// neighborhood search". The HNSW elevator node may not exist in lower layers,
// so the library must fall back to the layer entry point rather than using a
// nil search point. Adding 200 vectors at 768 dimensions reliably exercises
// the multi-layer code path.
func TestVectorIndex_Add_ManyVectors_NoNeighborhoodError(t *testing.T) {
	idx := NewDefaultIndex(testLogger)
	for i := 0; i < 200; i++ {
		vec := make([]float32, 768)
		vec[i%768] = 1.0
		if err := idx.Add(fmt.Sprintf("id-%d", i), vec); err != nil {
			t.Fatalf("Add(%d) failed: %v", i, err)
		}
	}
}

// Round-trip: Save then LoadIndex preserves all vector IDs.
func TestVectorIndex_RoundTrip_AllIDs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roundtrip.hnsw")

	idx := NewDefaultIndex(testLogger)
	ids := []string{"alpha", "beta", "gamma", "delta"}
	for i, id := range ids {
		vec := make([]float32, 768)
		vec[i] = 1.0
		if err := idx.Add(id, vec); err != nil {
			t.Fatalf("Add(%s): %v", id, err)
		}
	}

	if err := idx.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	idx2, err := LoadIndex(path, testLogger)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	for _, id := range ids {
		if !idx2.Has(id) {
			t.Errorf("loaded index missing id %q", id)
		}
	}
}
