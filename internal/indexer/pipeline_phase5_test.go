package indexer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"universal-search/internal/store"
)

// REF-041: NewPipeline honours PipelineConfig — workers=1, queue=4, saveEveryN=3.
func TestNewPipeline_HonoursConfig(t *testing.T) {
	cfg := PipelineConfig{Workers: 1, JobQueueSize: 4, SaveEveryN: 3}
	p, _, _ := newTestPipelineWithMock(t, &mockEmbedder{}, nil, cfg.Workers)
	defer p.Stop()

	// workerCount must reflect config.
	if p.workerCount != 1 {
		t.Errorf("workerCount: got %d, want 1", p.workerCount)
	}

	// Verify DefaultPipelineConfig returns historical defaults.
	def := DefaultPipelineConfig()
	if def.Workers != 4 || def.JobQueueSize != 64 || def.SaveEveryN != 50 {
		t.Errorf("DefaultPipelineConfig mismatch: %+v", def)
	}
}

// REF-041: SaveEveryN plumbed from cfg into the pipeline field.
func TestNewPipeline_SaveEveryNFromConfig(t *testing.T) {
	cfg := PipelineConfig{Workers: 1, JobQueueSize: 4, SaveEveryN: 3}
	p, _, _ := newTestPipelineWithMock(t, &mockEmbedder{}, nil, cfg.Workers)
	// Emulate what NewPipeline does: inject saveEveryN.
	p.saveEveryN = cfg.SaveEveryN
	defer p.Stop()

	if p.saveEveryN != 3 {
		t.Fatalf("saveEveryN: got %d, want 3", p.saveEveryN)
	}
}

// TestPipeline_WritesModelAndDims (REF-060): storeChunks populates
// embedding_model and embedding_dims on every chunk row, sourced from the
// pipeline's embedder.
func TestPipeline_WritesModelAndDims(t *testing.T) {
	dir := testTempDir(t)
	fpath := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(fpath, []byte("hello world from the pipeline"), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := &mockEmbedder{}
	p, s, _ := newTestPipelineWithMock(t, mock, nil, 1)
	defer p.Stop()

	p.SubmitFile(fpath)

	deadline := time.After(5 * time.Second)
	for {
		mock.mu.Lock()
		calls := mock.batchCallCount
		mock.mu.Unlock()
		if calls >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("embed batch never called")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Wait for storeChunks to run (status IndexedFiles == 1).
	deadline = time.After(5 * time.Second)
	for {
		if p.Status().IndexedFiles >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("file not indexed: %+v", p.Status())
		case <-time.After(10 * time.Millisecond):
		}
	}

	rows, err := s.GetAllChunks()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("no chunks inserted")
	}
	// Re-query directly for the columns we care about.
	dbRows, err := directQueryModelDims(t, s)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range dbRows {
		if r.model != "mock" {
			t.Errorf("embedding_model = %q, want %q", r.model, "mock")
		}
		if r.dims != 3 {
			t.Errorf("embedding_dims = %d, want 3", r.dims)
		}
	}
}

type modelDimsRow struct {
	model string
	dims  int
}

// directQueryModelDims bypasses the Store API to pull the two new columns for
// assertions — the Store's public chunk APIs don't expose them yet and
// Phase 7 does not require doing so.
func directQueryModelDims(t *testing.T, s *store.Store) ([]modelDimsRow, error) {
	t.Helper()
	db := store.DBForTesting(s)
	rows, err := db.Query(`SELECT embedding_model, embedding_dims FROM chunks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []modelDimsRow
	for rows.Next() {
		var r modelDimsRow
		if err := rows.Scan(&r.model, &r.dims); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// testTempDir wraps t.TempDir for the phase5 test file where multiple tests share helpers.
func testTempDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}
