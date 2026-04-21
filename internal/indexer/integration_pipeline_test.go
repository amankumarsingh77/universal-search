//go:build integration

package indexer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"

	"universal-search/internal/embedder"
	"universal-search/internal/store"
	"universal-search/internal/vectorstore"
)

var intLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// writeTextCorpus creates n small .txt files under dir. Each file has a unique
// body so the content hash is distinct.
func writeTextCorpus(t *testing.T, dir string, n int) []string {
	t.Helper()
	paths := make([]string, n)
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, fmt.Sprintf("file%03d.txt", i))
		body := fmt.Sprintf("Document number %d with distinct contents.\n", i)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", p, err)
		}
		paths[i] = p
	}
	return paths
}

// countingEmbedder wraps a FakeEmbedder and records how many EmbedBatch calls
// happened. It can optionally inject a canned error on the Nth call.
type countingEmbedder struct {
	inner    *embedder.FakeEmbedder
	calls    atomic.Int32
	failOnce atomic.Int32 // call index to fail at, or 0 to disable
	failErr  error
}

func (c *countingEmbedder) ModelID() string { return c.inner.ModelID() }
func (c *countingEmbedder) Dimensions() int { return c.inner.Dimensions() }
func (c *countingEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return c.inner.EmbedQuery(ctx, text)
}
func (c *countingEmbedder) EmbedBatch(ctx context.Context, inputs []embedder.ChunkInput) ([][]float32, error) {
	n := c.calls.Add(1)
	if fail := c.failOnce.Load(); fail != 0 && n == fail {
		return nil, c.failErr
	}
	return c.inner.EmbedBatch(ctx, inputs)
}

// countChunksForFile returns the number of chunk rows for the given file ID.
func countChunksForFile(t *testing.T, s *store.Store, fileID int64) int {
	t.Helper()
	all, err := s.GetAllChunks()
	if err != nil {
		t.Fatalf("GetAllChunks: %v", err)
	}
	n := 0
	for _, c := range all {
		if c.FileID == fileID {
			n++
		}
	}
	return n
}

// TestIntegration_ShutdownMidIndex — REF-084: submit a folder of 50 files,
// cancel the pipeline mid-run via Stop(), assert (a) Stop returns within 15s,
// (b) no file row exists with a non-empty content_hash that lacks chunks.
func TestIntegration_ShutdownMidIndex(t *testing.T) {
	dir := t.TempDir()
	writeTextCorpus(t, dir, 50)

	s, err := store.NewStore(filepath.Join(t.TempDir(), "int.db"), intLogger)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	idx := vectorstore.NewDefaultIndex(intLogger)
	fake := embedder.NewFake("int-fake", 64)

	p := NewPipeline(s, idx, fake, t.TempDir(), intLogger, nil, PipelineConfig{Workers: 4, JobQueueSize: 64, SaveEveryN: 50})

	p.SubmitFolder(dir, nil, false)

	// Let indexing make progress, then cancel.
	time.Sleep(50 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		p.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("pipeline did not stop within 15s after Stop()")
	}

	// Invariant: every file row with a non-empty content_hash must have at
	// least one chunk (two-phase commit: hash is written last).
	files, err := s.GetAllFiles()
	if err != nil {
		t.Fatalf("GetAllFiles: %v", err)
	}
	for _, f := range files {
		if f.ContentHash == "" {
			continue
		}
		if countChunksForFile(t, s, f.ID) == 0 {
			t.Errorf("file id=%d path=%s has content_hash=%q but zero chunks — two-phase commit violated",
				f.ID, f.Path, f.ContentHash)
		}
	}
}

// TestIntegration_TwoPhaseCommitRollback — REF-084: a failure during embedding
// must leave the file row with ContentHash="" (the placeholder) and no chunks
// in the store, so the next indexer pass re-processes the file.
func TestIntegration_TwoPhaseCommitRollback(t *testing.T) {
	dir := t.TempDir()
	paths := writeTextCorpus(t, dir, 3)

	s, err := store.NewStore(filepath.Join(t.TempDir(), "int.db"), intLogger)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	idx := vectorstore.NewDefaultIndex(intLogger)
	fake := &countingEmbedder{
		inner:   embedder.NewFake("int-fake", 64),
		failErr: errors.New("synthetic embed failure"),
	}
	fake.failOnce.Store(2) // fail on the 2nd EmbedBatch call

	p := NewPipeline(s, idx, fake, t.TempDir(), intLogger, nil, PipelineConfig{Workers: 1, JobQueueSize: 8, SaveEveryN: 50})
	defer p.Stop()

	for _, fp := range paths {
		p.processSingleFile(fp)
	}

	var rollbackCount int
	for _, fp := range paths {
		rec, err := s.GetFileByPath(fp)
		if err != nil {
			continue
		}
		if rec.ContentHash == "" && countChunksForFile(t, s, rec.ID) == 0 {
			rollbackCount++
		}
	}
	if rollbackCount == 0 {
		t.Fatal("expected at least one file to exhibit rollback (empty hash + no chunks), got none")
	}
}

// TestIntegration_RateLimitPauseResume — REF-084: when the embedder surfaces a
// quota-exhausted error, the pipeline records QuotaPaused=true; once the
// embedder recovers, subsequent files succeed.
func TestIntegration_RateLimitPauseResume(t *testing.T) {
	dir := t.TempDir()
	paths := writeTextCorpus(t, dir, 3)

	s, err := store.NewStore(filepath.Join(t.TempDir(), "int.db"), intLogger)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	idx := vectorstore.NewDefaultIndex(intLogger)
	fake := &countingEmbedder{
		inner:   embedder.NewFake("int-fake", 64),
		failErr: errors.New("all gemini api keys are cooling or exhausted"),
	}
	fake.failOnce.Store(2)

	p := NewPipeline(s, idx, fake, t.TempDir(), intLogger, nil, PipelineConfig{Workers: 1, JobQueueSize: 8, SaveEveryN: 50})
	defer p.Stop()

	// First file — succeeds.
	p.processSingleFile(paths[0])
	if p.Status().QuotaPaused {
		t.Fatal("QuotaPaused=true before any quota error; want false")
	}

	// Second file — embedder returns quota error, pipeline flips QuotaPaused.
	p.processSingleFile(paths[1])
	if !p.Status().QuotaPaused {
		t.Fatal("expected QuotaPaused=true after quota-exhausted embed error")
	}
	if p.Status().QuotaResumeAt == "" {
		t.Error("expected QuotaResumeAt to be set after quota pause")
	}

	// Third file — embedder recovers, file indexes normally.
	p.processSingleFile(paths[2])
	rec, err := s.GetFileByPath(paths[2])
	if err != nil {
		t.Fatalf("third file not indexed: %v", err)
	}
	if rec.ContentHash == "" {
		t.Error("third file indexed post-recovery but content_hash is empty (commit did not run)")
	}
}

// TestIntegration_LegacyDatabaseAdoption — REF-084: seed a tempdir file-backed
// SQLite DB in pre-refactor shape (no schema_migrations, but chunks.vector_blob
// and parsed_query_cache already present), then open via store.NewStore. The
// store opens without error and behaves normally for inserts, proving legacy
// adoption + migration 004 ran.
func TestIntegration_LegacyDatabaseAdoption(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")

	legacy := `
		CREATE TABLE files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT NOT NULL UNIQUE,
			file_type TEXT NOT NULL,
			extension TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			modified_at DATETIME NOT NULL,
			indexed_at DATETIME NOT NULL,
			content_hash TEXT NOT NULL DEFAULT '',
			thumbnail_path TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE chunks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_id INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
			vector_id TEXT NOT NULL UNIQUE,
			chunk_index INTEGER NOT NULL,
			start_time REAL NOT NULL DEFAULT 0,
			end_time REAL NOT NULL DEFAULT 0
		);
		CREATE TABLE indexed_folders (id INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT NOT NULL UNIQUE);
		CREATE TABLE excluded_patterns (id INTEGER PRIMARY KEY AUTOINCREMENT, pattern TEXT NOT NULL UNIQUE);
		CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE query_cache (query TEXT PRIMARY KEY, vector BLOB NOT NULL, created_at INTEGER NOT NULL);
		CREATE TABLE parsed_query_cache (
			query_text_normalized TEXT PRIMARY KEY,
			spec_json TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			last_used_at INTEGER NOT NULL
		);
		ALTER TABLE chunks ADD COLUMN vector_blob BLOB;
	`

	seed, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	if _, err := seed.Exec(legacy); err != nil {
		seed.Close()
		t.Fatalf("apply legacy schema: %v", err)
	}
	seed.Close()

	// Open via the real store — must apply legacy adoption + migration 004
	// and succeed.
	s, err := store.NewStore(dbPath, intLogger)
	if err != nil {
		t.Fatalf("NewStore on legacy DB: %v", err)
	}
	defer s.Close()

	// Exercise the store to prove the schema is fully migrated (embedding_model
	// + embedding_dims exist on chunks; InsertChunk writes them).
	fileID, err := s.UpsertFile(store.FileRecord{
		Path: "/tmp/legacy-test.txt", FileType: "text", Extension: ".txt",
		SizeBytes: 10, ModifiedAt: time.Now(), IndexedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertFile on adopted db: %v", err)
	}
	if _, err := s.InsertChunk(store.ChunkRecord{
		FileID: fileID, VectorID: "legacy-vec", ChunkIndex: 0,
		EmbeddingModel: "adopted-model", EmbeddingDims: 64,
	}); err != nil {
		t.Fatalf("InsertChunk with embedding_model: %v — migration 004 did not run", err)
	}

	count, err := s.CountChunksByModel("adopted-model")
	if err != nil {
		t.Fatalf("CountChunksByModel: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 chunk for adopted-model, got %d", count)
	}
}

// TestLegacyDatabaseAdoption — named alias for REF-084. The file-backed case
// lives here; the in-memory variant is in store/migrator_test.go.
func TestLegacyDatabaseAdoption(t *testing.T) { TestIntegration_LegacyDatabaseAdoption(t) }

// TestShutdownMidIndex — named alias for REF-084.
func TestShutdownMidIndex(t *testing.T) { TestIntegration_ShutdownMidIndex(t) }

// TestTwoPhaseCommitRollback — named alias for REF-084.
func TestTwoPhaseCommitRollback(t *testing.T) { TestIntegration_TwoPhaseCommitRollback(t) }

// TestRateLimitPauseResume — named alias for REF-084.
func TestRateLimitPauseResume(t *testing.T) { TestIntegration_RateLimitPauseResume(t) }
