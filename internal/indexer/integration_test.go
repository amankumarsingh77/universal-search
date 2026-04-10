package indexer

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"universal-search/internal/store"
	"universal-search/internal/vectorstore"
)

// newTestComponents returns a store backed by an in-memory SQLite DB, a fresh
// HNSW index, and a temp path for saving the index to disk.
func newTestComponents(t *testing.T) (*store.Store, *vectorstore.Index, string) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := store.NewStore(":memory:", logger)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	idx := vectorstore.NewIndex(logger)
	indexPath := filepath.Join(t.TempDir(), "test.hnsw")
	return s, idx, indexPath
}

// newTestPipelineNoWorker creates a Pipeline without starting the worker goroutine.
// Methods like ReconcileIndex and StartupRescan can be called directly.
func newTestPipelineNoWorker(t *testing.T, s *store.Store, idx *vectorstore.Index) *Pipeline {
	t.Helper()
	p, _, _ := newTestPipeline(t, nil)
	// Replace store and index with the ones provided.
	p.store = s
	p.index = idx
	return p
}

// TestIntegration_ReconcileFindsOrphanedFile verifies that ReconcileIndex
// increments TotalFiles and sets IsRunning=true when a chunk's vector is absent
// from the HNSW index.
func TestIntegration_ReconcileFindsOrphanedFile(t *testing.T) {
	s, idx, _ := newTestComponents(t)
	p := newTestPipelineNoWorker(t, s, idx)

	dir := t.TempDir()
	filePath := filepath.Join(dir, "orphaned.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filePath)

	fileID, err := s.UpsertFile(store.FileRecord{
		Path:       filePath,
		FileType:   "text",
		Extension:  ".txt",
		SizeBytes:  info.Size(),
		ModifiedAt: info.ModTime(),
		IndexedAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}

	if _, err := s.InsertChunk(store.ChunkRecord{
		FileID:     fileID,
		VectorID:   "f1-c0",
		ChunkIndex: 0,
	}); err != nil {
		t.Fatalf("InsertChunk: %v", err)
	}

	// Do NOT add vector to HNSW — so Has("f1-c0") == false.

	p.ReconcileIndex()

	p.mu.RLock()
	total := p.status.TotalFiles
	running := p.status.IsRunning
	p.mu.RUnlock()

	if total != 1 {
		t.Fatalf("expected TotalFiles=1 after ReconcileIndex with orphaned vector, got %d", total)
	}
	if !running {
		t.Fatal("expected IsRunning=true during reconcile with orphaned file")
	}
}

// TestIntegration_ReconcileSkipsFullyIndexedFile verifies that ReconcileIndex
// does NOT re-queue files whose chunks are all present in HNSW.
func TestIntegration_ReconcileSkipsFullyIndexedFile(t *testing.T) {
	s, idx, _ := newTestComponents(t)
	p := newTestPipelineNoWorker(t, s, idx)

	dir := t.TempDir()
	filePath := filepath.Join(dir, "indexed.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filePath)

	fileID, err := s.UpsertFile(store.FileRecord{
		Path:        filePath,
		FileType:    "text",
		Extension:   ".txt",
		SizeBytes:   info.Size(),
		ModifiedAt:  info.ModTime(),
		IndexedAt:   time.Now(),
		ContentHash: "abc123",
	})
	if err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}

	if _, err := s.InsertChunk(store.ChunkRecord{
		FileID:     fileID,
		VectorID:   "f1-c0",
		ChunkIndex: 0,
	}); err != nil {
		t.Fatalf("InsertChunk: %v", err)
	}

	// Add vector to HNSW so it IS present.
	vec := make([]float32, 768)
	if err := idx.Add("f1-c0", vec); err != nil {
		t.Fatalf("idx.Add: %v", err)
	}

	p.ReconcileIndex()

	p.mu.RLock()
	total := p.status.TotalFiles
	p.mu.RUnlock()

	if total != 0 {
		t.Fatalf("expected TotalFiles=0 when all vectors present, got %d", total)
	}
}

// TestIntegration_ReconcileEmptyDB verifies that ReconcileIndex completes
// without panic or error when the store has no chunk records.
func TestIntegration_ReconcileEmptyDB(t *testing.T) {
	s, idx, _ := newTestComponents(t)
	p := newTestPipelineNoWorker(t, s, idx)

	// No store entries — should be a no-op.
	p.ReconcileIndex()

	p.mu.RLock()
	total := p.status.TotalFiles
	p.mu.RUnlock()

	if total != 0 {
		t.Fatalf("expected TotalFiles=0 with empty DB, got %d", total)
	}
}

// TestIntegration_AtomicSave_NoTmpFilesAfterSave verifies that Save() leaves
// the final .graph and .map files in place and removes all .tmp files.
func TestIntegration_AtomicSave_NoTmpFilesAfterSave(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	idx := vectorstore.NewIndex(logger)

	vec1 := make([]float32, 768)
	vec2 := make([]float32, 768)
	vec1[0] = 0.1
	vec2[0] = 0.2

	if err := idx.Add("v1", vec1); err != nil {
		t.Fatalf("idx.Add v1: %v", err)
	}
	if err := idx.Add("v2", vec2); err != nil {
		t.Fatalf("idx.Add v2: %v", err)
	}

	indexPath := filepath.Join(t.TempDir(), "test.hnsw")
	if err := idx.Save(indexPath); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Final files must exist.
	if _, err := os.Stat(indexPath + ".graph"); err != nil {
		t.Fatalf("expected %s.graph to exist: %v", indexPath, err)
	}
	if _, err := os.Stat(indexPath + ".map"); err != nil {
		t.Fatalf("expected %s.map to exist: %v", indexPath, err)
	}

	// Temp files must NOT exist.
	if _, err := os.Stat(indexPath + ".graph.tmp"); err == nil {
		t.Fatalf("expected %s.graph.tmp to be removed after Save", indexPath)
	}
	if _, err := os.Stat(indexPath + ".map.tmp"); err == nil {
		t.Fatalf("expected %s.map.tmp to be removed after Save", indexPath)
	}
}

// TestIntegration_LoadIndex_FallbackOnCorrupt verifies that LoadIndex returns
// an error on corrupt files, and that NewIndex creates a valid fallback index.
func TestIntegration_LoadIndex_FallbackOnCorrupt(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.hnsw")

	// Write garbage bytes to the .graph file.
	if err := os.WriteFile(path+".graph", []byte("not a graph"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// LoadIndex must return a non-nil error.
	_, err := vectorstore.LoadIndex(path, logger)
	if err == nil {
		t.Fatal("expected LoadIndex to return an error for corrupt graph file, got nil")
	}

	// App.go fallback: create NewIndex when LoadIndex fails.
	idx := vectorstore.NewIndex(logger)
	if idx == nil {
		t.Fatal("expected NewIndex to return a valid index, got nil")
	}

	// The fallback index must not contain any vectors.
	if idx.Has("any-id") {
		t.Fatal("expected fresh index Has() to return false for any ID")
	}
}

// TestIntegration_StartupRescan_QueueModifiedFile verifies that StartupRescan
// queues a file whose stored ModifiedAt differs from the actual mtime on disk.
func TestIntegration_StartupRescan_QueueModifiedFile(t *testing.T) {
	s, idx, _ := newTestComponents(t)
	p := newTestPipelineNoWorker(t, s, idx)

	dir := t.TempDir()
	filePath := filepath.Join(dir, "modified.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filePath)

	// Store with a ModifiedAt 2 hours in the past to force a mtime mismatch.
	_, err := s.UpsertFile(store.FileRecord{
		Path:        filePath,
		FileType:    "text",
		Extension:   ".txt",
		SizeBytes:   info.Size(),
		ModifiedAt:  time.Now().Add(-2 * time.Hour),
		IndexedAt:   time.Now(),
		ContentHash: "oldhash",
	})
	if err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}

	// Drain any pre-existing jobs.
	for len(p.jobCh) > 0 {
		<-p.jobCh
	}

	p.StartupRescan([]string{dir})

	jobsQueued := len(p.jobCh)
	pending := p.pendingJobs.Load()
	if jobsQueued == 0 && pending == 0 {
		t.Fatal("expected file to be queued for re-indexing (modified mtime), but no job submitted")
	}
}

// TestIntegration_StartupRescan_CleanupDeletedFile verifies that
// cleanupDeletedFiles removes a file record from SQLite and its vector from
// HNSW when the file no longer exists on disk.
func TestIntegration_StartupRescan_CleanupDeletedFile(t *testing.T) {
	s, idx, _ := newTestComponents(t)
	p := newTestPipelineNoWorker(t, s, idx)

	// Use a path that definitely doesn't exist on disk.
	ghostPath := filepath.Join(t.TempDir(), "ghost_deleted_file.txt")

	fileID, err := s.UpsertFile(store.FileRecord{
		Path:        ghostPath,
		FileType:    "text",
		Extension:   ".txt",
		SizeBytes:   100,
		ModifiedAt:  time.Now(),
		IndexedAt:   time.Now(),
		ContentHash: "deadbeef",
	})
	if err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}

	vecID := "orphan-v1"
	if _, err := s.InsertChunk(store.ChunkRecord{
		FileID:     fileID,
		VectorID:   vecID,
		ChunkIndex: 0,
	}); err != nil {
		t.Fatalf("InsertChunk: %v", err)
	}

	// Add vector to HNSW.
	vec := make([]float32, 768)
	if err := idx.Add(vecID, vec); err != nil {
		t.Fatalf("idx.Add: %v", err)
	}

	if !idx.Has(vecID) {
		t.Fatal("vector should be present in HNSW before cleanup")
	}

	// The ghost file does not exist on disk — call cleanupDeletedFiles directly.
	log := slog.Default()
	p.cleanupDeletedFiles(log)

	// Store record must be removed.
	if _, err := s.GetFileByPath(ghostPath); err == nil {
		t.Fatal("expected file record to be removed from SQLite, but it still exists")
	}

	// Vector must be removed from HNSW.
	if idx.Has(vecID) {
		t.Fatal("expected vector to be removed from HNSW after file cleanup")
	}
}
