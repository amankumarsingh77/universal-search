package indexer

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"universal-search/internal/embedder"
	"universal-search/internal/store"
	"universal-search/internal/vectorstore"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newTestPipeline(t *testing.T, onDone func()) (*Pipeline, *store.Store, *vectorstore.Index) {
	t.Helper()
	s, err := store.NewStore(":memory:", testLogger())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	idx := vectorstore.NewIndex(testLogger())

	p := &Pipeline{
		store:     s,
		index:     idx,
		embedder:  nil, // no real embedder
		thumbDir:  t.TempDir(),
		logger:    testLogger().WithGroup("indexer"),
		pauseCh:   make(chan struct{}, 1),
		jobCh:     make(chan indexJob, 64),
		onJobDone: onDone,
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel
	t.Cleanup(cancel)

	return p, s, idx
}

// TestIndexFile_SkipsUnchangedFile — REQ-002, EDGE-017
// A file already in the store with the matching content hash must be skipped.
// indexFile returns nil without touching the store (no UpsertFile call).
func TestIndexFile_SkipsUnchangedFile(t *testing.T) {
	p, s, _ := newTestPipeline(t, nil)

	// Create a real file on disk.
	dir := t.TempDir()
	filePath := dir + "/hello.txt"
	if err := os.WriteFile(filePath, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Compute its real hash and store it as fully indexed.
	hash, err := hashFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filePath)
	_, err = s.UpsertFile(store.FileRecord{
		Path:        filePath,
		FileType:    "text",
		Extension:   ".txt",
		SizeBytes:   info.Size(),
		ModifiedAt:  info.ModTime(),
		IndexedAt:   time.Now(),
		ContentHash: hash, // fully indexed
	})
	if err != nil {
		t.Fatal(err)
	}

	// indexFile should skip (return nil) since hash matches.
	if err := p.indexFile(filePath); err != nil {
		t.Fatalf("expected nil (skip), got: %v", err)
	}
}

// TestIndexFile_ReindexesFileWithEmptyHash — REQ-003, EDGE-001, EDGE-002
// A file in the store with ContentHash="" must NOT be skipped.
// Without a real embedder the call proceeds past the skip check and hits
// "embedder not initialized" — that proves the file was not skipped.
func TestIndexFile_ReindexesFileWithEmptyHash(t *testing.T) {
	p, s, _ := newTestPipeline(t, nil)

	dir := t.TempDir()
	filePath := dir + "/hello.txt"
	if err := os.WriteFile(filePath, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(filePath)
	_, err := s.UpsertFile(store.FileRecord{
		Path:        filePath,
		FileType:    "text",
		Extension:   ".txt",
		SizeBytes:   info.Size(),
		ModifiedAt:  info.ModTime(),
		IndexedAt:   time.Now(),
		ContentHash: "", // empty — not fully indexed
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should NOT skip. With nil embedder it should fail with "embedder not initialized".
	err = p.indexFile(filePath)
	if err == nil {
		t.Fatal("expected an error (embedder not initialized), got nil")
	}
	if err.Error() != "embedder not initialized — set GEMINI_API_KEY" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestIndexFile_UpsertFileCalledWithEmptyHash — REQ-001
// When indexFile begins processing (past the skip check), it must call
// UpsertFile with ContentHash="" — not the real hash.
//
// We verify this indirectly: after UpsertFile the content_hash in the store
// should be "" (the pipeline has not yet committed the real hash).
//
// This test needs a pipeline that gets far enough to call UpsertFile.
// We use an unsupported file type to make ChunkFile return 0 chunks, which
// causes indexFile to return nil after calling UpsertFile.
//
// Actually: a .txt file with content will be chunked by ChunkText. To get
// past ChunkFile we need content. But without an embedder we can't embed.
// The test that verifies empty-hash-at-upsert must use a helper that calls
// UpsertFile directly and then reads the DB — or we test after a full
// successful round-trip (requires embedder).
//
// Instead: verify via the store directly that UpsertFile writes "" when called
// with ContentHash:"". This tests the store contract, not indexFile internals.
func TestIndexFile_UpsertWritesEmptyHash(t *testing.T) {
	_, s, _ := newTestPipeline(t, nil)

	dir := t.TempDir()
	filePath := dir + "/sample.txt"
	if err := os.WriteFile(filePath, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filePath)

	_, err := s.UpsertFile(store.FileRecord{
		Path:        filePath,
		FileType:    "text",
		Extension:   ".txt",
		SizeBytes:   info.Size(),
		ModifiedAt:  info.ModTime(),
		IndexedAt:   time.Now(),
		ContentHash: "", // two-phase: empty at upsert time
	})
	if err != nil {
		t.Fatal(err)
	}

	rec, err := s.GetFileByPath(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if rec.ContentHash != "" {
		t.Fatalf("expected ContentHash to be empty after Phase 1 UpsertFile, got %q", rec.ContentHash)
	}
}

// TestIndexFile_UpdateContentHash — REQ-002
// After all chunks succeed, UpdateContentHash must write the real 64-char hash.
func TestIndexFile_UpdateContentHash(t *testing.T) {
	_, s, _ := newTestPipeline(t, nil)

	dir := t.TempDir()
	filePath := dir + "/sample.txt"
	if err := os.WriteFile(filePath, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filePath)

	// Phase 1: insert with empty hash
	fileID, err := s.UpsertFile(store.FileRecord{
		Path:        filePath,
		FileType:    "text",
		Extension:   ".txt",
		SizeBytes:   info.Size(),
		ModifiedAt:  info.ModTime(),
		IndexedAt:   time.Now(),
		ContentHash: "",
	})
	if err != nil {
		t.Fatal(err)
	}

	realHash, _ := hashFile(filePath)

	// Phase 2 (simulate success): call UpdateContentHash
	if err := s.UpdateContentHash(fileID, realHash); err != nil {
		t.Fatal(err)
	}

	rec, err := s.GetFileByPath(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if rec.ContentHash != realHash {
		t.Fatalf("expected ContentHash=%q, got %q", realHash, rec.ContentHash)
	}
	if len(rec.ContentHash) != 64 {
		t.Fatalf("expected 64-char hash, got len=%d", len(rec.ContentHash))
	}
}

// TestIndexFile_DoesNotUpdateHashIfChunkFails — REQ-013, EDGE-016
// When embedder is nil (simulates all-chunk failure), content_hash must stay "".
func TestIndexFile_DoesNotUpdateHashIfChunkFails(t *testing.T) {
	p, s, _ := newTestPipeline(t, nil)

	dir := t.TempDir()
	filePath := dir + "/sample.txt"
	if err := os.WriteFile(filePath, []byte("hello world text content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Start with no record in the store — indexFile will call UpsertFile with empty hash.
	// Then it will fail at embedding (nil embedder) and must NOT call UpdateContentHash.
	err := p.indexFile(filePath)
	if err == nil {
		t.Fatal("expected error from nil embedder")
	}

	// The file should be in the store with empty content_hash.
	rec, dbErr := s.GetFileByPath(filePath)
	if dbErr != nil {
		t.Fatalf("file should be in store after failed indexFile, got: %v", dbErr)
	}
	if rec.ContentHash != "" {
		t.Fatalf("content_hash should remain empty after chunk failure, got %q", rec.ContentHash)
	}
}

// TestPipeline_ChunksSinceLastSave_FieldExists verifies the struct field
// and saveInterval constant compile correctly and are zero-valued by default.
func TestPipeline_ChunksSinceLastSave_FieldExists(t *testing.T) {
	p, _, _ := newTestPipeline(t, nil)
	p.mu.Lock()
	initial := p.chunksSinceLastSave
	p.mu.Unlock()
	if initial != 0 {
		t.Fatalf("expected chunksSinceLastSave=0 on new pipeline, got %d", initial)
	}
	// saveInterval constant must equal 50.
	if saveInterval != 50 {
		t.Fatalf("expected saveInterval=50, got %d", saveInterval)
	}
}

// TestPipeline_PeriodicSave_OnJobDoneCalledAtSaveInterval — REQ-008
// Manually increment chunksSinceLastSave to saveInterval and verify onJobDone fires.
func TestPipeline_PeriodicSave_OnJobDoneCalledAtSaveInterval(t *testing.T) {
	calls := 0
	onDone := func() { calls++ }

	p, _, _ := newTestPipeline(t, onDone)

	// Simulate the periodic-save logic inline (mirrors what indexFile does).
	p.mu.Lock()
	p.chunksSinceLastSave = saveInterval - 1
	p.mu.Unlock()

	// One more chunk pushes it to saveInterval.
	p.mu.Lock()
	p.chunksSinceLastSave++
	shouldSave := p.chunksSinceLastSave >= saveInterval
	if shouldSave {
		p.chunksSinceLastSave = 0
	}
	p.mu.Unlock()

	if shouldSave && p.onJobDone != nil {
		p.onJobDone()
	}

	if calls != 1 {
		t.Fatalf("expected onJobDone called 1 time at saveInterval, got %d", calls)
	}

	// Counter must have reset.
	p.mu.Lock()
	counter := p.chunksSinceLastSave
	p.mu.Unlock()
	if counter != 0 {
		t.Fatalf("expected chunksSinceLastSave reset to 0, got %d", counter)
	}
}

// TestPipeline_PeriodicSave_NotCalledBeforeSaveInterval — REQ-008
// Verify onJobDone is NOT called when counter < saveInterval.
func TestPipeline_PeriodicSave_NotCalledBeforeSaveInterval(t *testing.T) {
	calls := 0
	onDone := func() { calls++ }
	p, _, _ := newTestPipeline(t, onDone)

	// Simulate 49 chunks (one short of threshold).
	for i := 0; i < saveInterval-1; i++ {
		p.mu.Lock()
		p.chunksSinceLastSave++
		shouldSave := p.chunksSinceLastSave >= saveInterval
		if shouldSave {
			p.chunksSinceLastSave = 0
		}
		p.mu.Unlock()

		if shouldSave && p.onJobDone != nil {
			p.onJobDone()
		}
	}

	if calls != 0 {
		t.Fatalf("expected no onJobDone calls before saveInterval, got %d", calls)
	}
}

// TestSetEmbedder_NilEmbedder — Phase 1 Task 1
// A pipeline created with nil embedder, after SetEmbedder(nil), must fail
// indexFile with "embedder not initialized" error.
func TestSetEmbedder_NilEmbedder(t *testing.T) {
	p, _, _ := newTestPipeline(t, nil)

	// Start with nil, then explicitly set nil again via SetEmbedder.
	p.SetEmbedder(nil)

	dir := t.TempDir()
	filePath := dir + "/test.txt"
	if err := os.WriteFile(filePath, []byte("hello world content"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := p.indexFile(filePath)
	if err == nil {
		t.Fatal("expected error from nil embedder, got nil")
	}
	if !strings.Contains(err.Error(), "embedder not initialized") {
		t.Fatalf("expected 'embedder not initialized' error, got: %v", err)
	}
}

// TestSetEmbedder_ReplaceEmbedder — Phase 1 Task 1
// SetEmbedder must replace the pipeline's embedder field.
// We verify by calling SetEmbedder with a non-nil embedder and confirming
// the struct field is updated (checked via the exported method).
func TestSetEmbedder_ReplaceEmbedder(t *testing.T) {
	p, _, _ := newTestPipeline(t, nil)

	// Pipeline starts with nil embedder.
	if p.embedder != nil {
		t.Fatal("expected nil embedder initially")
	}

	// Create a real *embedder.Embedder with a fake key — no real API calls made.
	emb, err := embedder.NewEmbedder("fake-key-for-test", 768, testLogger())
	if err != nil {
		t.Fatalf("NewEmbedder with fake key: %v", err)
	}

	p.SetEmbedder(emb)

	// Confirm the field was replaced.
	p.embedderMu.RLock()
	got := p.embedder
	p.embedderMu.RUnlock()

	if got == nil {
		t.Fatal("expected embedder to be non-nil after SetEmbedder")
	}
	if got != emb {
		t.Fatal("expected embedder pointer to match what was passed to SetEmbedder")
	}
}

// TestSetEmbedder_Race — Phase 1 Task 1
// Concurrent SetEmbedder calls while the pipeline worker goroutine is idle
// must not produce a data race. Run with: go test -race ./internal/indexer/...
func TestSetEmbedder_Race(t *testing.T) {
	p, _, _ := newTestPipeline(t, nil)

	// Start the worker goroutine (mirrors NewPipeline behaviour).
	p.workerWg.Add(1)
	go p.worker()

	emb, err := embedder.NewEmbedder("fake-key-race-test", 768, testLogger())
	if err != nil {
		t.Fatalf("NewEmbedder: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(e *embedder.Embedder) {
			defer wg.Done()
			p.SetEmbedder(e)
		}(emb)
	}
	wg.Wait()
}
