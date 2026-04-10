package indexer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"universal-search/internal/store"
)

// TestReconcileIndex_NoChunks — empty DB → ReconcileIndex completes without error,
// status.TotalFiles remains 0.
func TestReconcileIndex_NoChunks(t *testing.T) {
	p, _, _ := newTestPipeline(t, nil)

	p.ReconcileIndex()

	p.mu.RLock()
	total := p.status.TotalFiles
	p.mu.RUnlock()

	if total != 0 {
		t.Fatalf("expected TotalFiles=0 after reconcile with no chunks, got %d", total)
	}
}

// TestReconcileIndex_AllVectorsPresent — REQ-004, EDGE-017
// Chunks in SQLite with matching vectors in HNSW → no files re-queued.
func TestReconcileIndex_AllVectorsPresent(t *testing.T) {
	p, s, idx := newTestPipeline(t, nil)

	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")
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

	// Insert 2 chunks and add their vectors to HNSW.
	vecID1 := "f1-c0"
	vecID2 := "f1-c1"
	if _, err := s.InsertChunk(store.ChunkRecord{FileID: fileID, VectorID: vecID1, ChunkIndex: 0}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertChunk(store.ChunkRecord{FileID: fileID, VectorID: vecID2, ChunkIndex: 1}); err != nil {
		t.Fatal(err)
	}

	// Add vectors to HNSW index so they're present.
	vec := make([]float32, 768)
	for i := range vec {
		vec[i] = 0.1
	}
	if err := idx.Add(vecID1, vec); err != nil {
		t.Fatalf("idx.Add: %v", err)
	}
	if err := idx.Add(vecID2, vec); err != nil {
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

// TestReconcileIndex_MissingVectors — REQ-004, REQ-005
// File has chunks in SQLite but no vectors in HNSW → TotalFiles incremented by 1.
func TestReconcileIndex_MissingVectors(t *testing.T) {
	p, s, _ := newTestPipeline(t, nil)

	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")
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

	// Insert chunks but do NOT add vectors to HNSW.
	if _, err := s.InsertChunk(store.ChunkRecord{FileID: fileID, VectorID: "f1-c0", ChunkIndex: 0}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertChunk(store.ChunkRecord{FileID: fileID, VectorID: "f1-c1", ChunkIndex: 1}); err != nil {
		t.Fatal(err)
	}

	p.ReconcileIndex()

	p.mu.RLock()
	total := p.status.TotalFiles
	p.mu.RUnlock()

	if total != 1 {
		t.Fatalf("expected TotalFiles=1 after reconcile with missing vectors, got %d", total)
	}
}

// TestReconcileIndex_PartialVectors — EDGE-002
// File has 3 chunks, only 2 are in HNSW → file is flagged for re-indexing.
func TestReconcileIndex_PartialVectors(t *testing.T) {
	p, s, idx := newTestPipeline(t, nil)

	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(filePath, []byte("hello world content"), 0o644); err != nil {
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

	// Insert 3 chunks.
	vecIDs := []string{"f1-c0", "f1-c1", "f1-c2"}
	for i, vid := range vecIDs {
		if _, err := s.InsertChunk(store.ChunkRecord{FileID: fileID, VectorID: vid, ChunkIndex: i}); err != nil {
			t.Fatal(err)
		}
	}

	// Add only 2 of 3 vectors to HNSW.
	vec := make([]float32, 768)
	for i := range vec {
		vec[i] = 0.1
	}
	if err := idx.Add(vecIDs[0], vec); err != nil {
		t.Fatalf("idx.Add: %v", err)
	}
	if err := idx.Add(vecIDs[1], vec); err != nil {
		t.Fatalf("idx.Add: %v", err)
	}
	// vecIDs[2] is NOT added → missing.

	p.ReconcileIndex()

	p.mu.RLock()
	total := p.status.TotalFiles
	p.mu.RUnlock()

	if total != 1 {
		t.Fatalf("expected TotalFiles=1 for file with partial vectors, got %d", total)
	}
}

// TestStartupRescan_SkipsNonExistentFolder — REQ-015, EDGE-011
// Calling StartupRescan with a non-existent folder should not panic or error.
func TestStartupRescan_SkipsNonExistentFolder(t *testing.T) {
	p, _, _ := newTestPipeline(t, nil)

	// Should not panic.
	p.StartupRescan([]string{"/nonexistent/path/12345xyz"})

	p.mu.RLock()
	total := p.status.TotalFiles
	p.mu.RUnlock()

	if total != 0 {
		t.Fatalf("expected TotalFiles=0 after skipping nonexistent folder, got %d", total)
	}
}

// TestStartupRescan_QueueModifiedFile — REQ-006, EDGE-010
// A file whose mtime differs from the stored ModifiedAt should be re-queued.
func TestStartupRescan_QueueModifiedFile(t *testing.T) {
	p, s, _ := newTestPipeline(t, nil)

	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Store with old ModifiedAt (1 hour ago) — different from actual file mtime.
	_, err := s.UpsertFile(store.FileRecord{
		Path:        filePath,
		FileType:    "text",
		Extension:   ".txt",
		SizeBytes:   5,
		ModifiedAt:  time.Now().Add(-1 * time.Hour),
		IndexedAt:   time.Now(),
		ContentHash: "oldhash",
	})
	if err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}

	p.StartupRescan([]string{dir})

	p.mu.RLock()
	total := p.status.TotalFiles
	p.mu.RUnlock()

	// processSingleFile increments TotalFiles when the job is processed.
	// But SubmitFile puts it in jobCh — the status is incremented in processSingleFile.
	// Since there's no worker running in the test pipeline, we check that the job
	// was submitted by checking the jobCh length.
	jobsQueued := len(p.jobCh)
	if jobsQueued == 0 && total == 0 {
		t.Fatal("expected file to be queued for re-indexing (modified mtime), but no job submitted")
	}
}

// TestStartupRescan_SkipsUnchangedFile — idempotent behavior
// A file with matching mtime should NOT be re-queued.
func TestStartupRescan_SkipsUnchangedFile(t *testing.T) {
	p, s, _ := newTestPipeline(t, nil)

	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Get the actual mtime from disk.
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}

	// Store with the real mtime — no change detected.
	_, err = s.UpsertFile(store.FileRecord{
		Path:        filePath,
		FileType:    "text",
		Extension:   ".txt",
		SizeBytes:   info.Size(),
		ModifiedAt:  info.ModTime(),
		IndexedAt:   time.Now(),
		ContentHash: "currenthash",
	})
	if err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}

	// Drain any initial jobs (shouldn't be any, but just in case).
	for len(p.jobCh) > 0 {
		<-p.jobCh
	}

	p.StartupRescan([]string{dir})

	p.mu.RLock()
	total := p.status.TotalFiles
	p.mu.RUnlock()

	jobsQueued := len(p.jobCh)
	if jobsQueued > 0 || total > 0 {
		t.Fatalf("expected unchanged file not to be queued, but got %d jobs in channel, TotalFiles=%d", jobsQueued, total)
	}
}

// TestStartupRescan_CleanupDeletedFile — REQ-007, EDGE-009
// A file in SQLite that no longer exists on disk should be removed from SQLite and HNSW.
func TestStartupRescan_CleanupDeletedFile(t *testing.T) {
	p, s, idx := newTestPipeline(t, nil)

	// Use a path that definitely doesn't exist on disk.
	ghostPath := "/tmp/nonexistent_test_file_reconcile_xyz_12345.txt"

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

	vecID := "ghost-f1-c0"
	if _, err := s.InsertChunk(store.ChunkRecord{FileID: fileID, VectorID: vecID, ChunkIndex: 0}); err != nil {
		t.Fatal(err)
	}

	// Add vector to HNSW.
	vec := make([]float32, 768)
	for i := range vec {
		vec[i] = 0.2
	}
	if err := idx.Add(vecID, vec); err != nil {
		t.Fatalf("idx.Add: %v", err)
	}

	// Verify vector is in HNSW before cleanup.
	if !idx.Has(vecID) {
		t.Fatal("vector should be present in HNSW before cleanup")
	}

	// Call cleanupDeletedFiles directly (same package).
	log := testLogger().WithGroup("test")
	p.cleanupDeletedFiles(log)

	// Verify file is removed from SQLite.
	if _, err := s.GetFileByPath(ghostPath); err == nil {
		t.Fatal("expected file to be removed from SQLite, but it still exists")
	}

	// Verify vector is removed from HNSW.
	if idx.Has(vecID) {
		t.Fatal("expected vector to be removed from HNSW after file cleanup")
	}
}

// TestStartupRescan_SkipsExcludedDirs — EDGE-004
// Files inside excluded directories should not be queued.
func TestStartupRescan_SkipsExcludedDirs(t *testing.T) {
	p, s, _ := newTestPipeline(t, nil)

	// Create a temp dir with a node_modules subdir containing a .txt file.
	dir := t.TempDir()
	excludedDir := filepath.Join(dir, "node_modules")
	if err := os.MkdirAll(excludedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	excludedFile := filepath.Join(excludedDir, "package.txt")
	if err := os.WriteFile(excludedFile, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Add the excluded pattern to the store.
	if err := s.AddExcludedPattern("node_modules"); err != nil {
		t.Fatalf("AddExcludedPattern: %v", err)
	}

	// Drain any existing jobs.
	for len(p.jobCh) > 0 {
		<-p.jobCh
	}

	p.StartupRescan([]string{dir})

	jobsQueued := len(p.jobCh)
	if jobsQueued > 0 {
		t.Fatalf("expected no jobs queued for file in excluded dir, got %d", jobsQueued)
	}

	p.mu.RLock()
	total := p.status.TotalFiles
	p.mu.RUnlock()

	if total > 0 {
		t.Fatalf("expected TotalFiles=0 with excluded dir, got %d", total)
	}
}
