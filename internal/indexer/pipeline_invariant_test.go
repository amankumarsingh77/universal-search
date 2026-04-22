package indexer

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"findo/internal/apperr"
	"findo/internal/embedder"
	"findo/internal/store"
	"findo/internal/vectorstore"
)

// ---------------------------------------------------------------------------
// REQ-031 / REQ-032: TestIndexing_CountInvariantHolds
//
// Drives a pipeline with a scripted mix of outcomes (success, permanent failure,
// transient retry → success, transient retry → exhaustion) and asserts that
// at every observed status snapshot:
//
//	IndexedFiles + FailedFiles + PendingRetryFiles ≤ TotalFiles
//
// This is the key invariant that prevents phantom counts.
// ---------------------------------------------------------------------------

// scriptedEmbedder applies a per-file outcome script based on the file's path.
// It uses a map keyed by base filename to decide whether to succeed, fail
// permanently (it can't actually; file paths to checkStale control permanent),
// or fail transiently.
type scriptedEmbedder struct {
	mu       sync.Mutex
	outcomes map[string][]bool // filename → ordered outcomes (true=success, false=fail transient)
}

func (s *scriptedEmbedder) ModelID() string        { return "scripted" }
func (s *scriptedEmbedder) Dimensions() int        { return 3 }
func (s *scriptedEmbedder) PausedUntil() time.Time { return time.Time{} }
func (s *scriptedEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return []float32{0, 0, 0}, nil
}
func (s *scriptedEmbedder) EmbedBatch(_ context.Context, chunks []embedder.ChunkInput) ([][]float32, error) {
	// Use the title (filename) to look up the outcome.
	var title string
	if len(chunks) > 0 {
		title = chunks[0].Title
	}
	s.mu.Lock()
	outcomes := s.outcomes[title]
	if len(outcomes) > 0 {
		outcome := outcomes[0]
		s.outcomes[title] = outcomes[1:]
		s.mu.Unlock()
		if !outcome {
			return nil, apperr.Wrap(apperr.ErrEmbedFailed.Code, "scripted transient fail", fmt.Errorf("scripted"))
		}
	} else {
		s.mu.Unlock()
	}
	result := make([][]float32, len(chunks))
	for i := range chunks {
		result[i] = []float32{float32(i), 0.1, 0.2}
	}
	return result, nil
}

// TestIndexing_CountInvariantHolds drives a pipeline and snapshots status at
// every transition, asserting the invariant at every snapshot.
func TestIndexing_CountInvariantHolds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping invariant test in short mode")
	}

	dir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "invariant.db")
	s, err := store.NewStore(dbPath, testLogger())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	idx := vectorstore.NewDefaultIndex(testLogger())

	// Create files with different outcome scripts:
	// - success.txt: succeeds immediately
	// - transient_then_ok.txt: fails once (transient), then succeeds
	// - exhausted.txt: always fails (will exhaust maxAttempts)
	// - permanent_fail.txt: nonexistent → ERR_FILE_UNREADABLE (permanent)
	type fileSetup struct {
		name     string
		content  string
		outcomes []bool // outcomes for scripted embedder (true=ok, false=fail transient)
		exists   bool
	}

	const (
		nSuccess   = 3
		nTransient = 2
		nExhausted = 1
		nPermanent = 2
		nTotal     = nSuccess + nTransient + nExhausted + nPermanent
	)

	files := make([]fileSetup, 0, nTotal)
	for i := 0; i < nSuccess; i++ {
		files = append(files, fileSetup{
			name:     fmt.Sprintf("success_%d.txt", i),
			content:  fmt.Sprintf("success file %d content that is meaningful for indexing via embedder pipeline", i),
			outcomes: []bool{true},
			exists:   true,
		})
	}
	for i := 0; i < nTransient; i++ {
		files = append(files, fileSetup{
			name:     fmt.Sprintf("transient_%d.txt", i),
			content:  fmt.Sprintf("transient file %d content that is meaningful for indexing via embedder pipeline", i),
			outcomes: []bool{false, true}, // fail once then succeed
			exists:   true,
		})
	}
	for i := 0; i < nExhausted; i++ {
		files = append(files, fileSetup{
			name:     fmt.Sprintf("exhausted_%d.txt", i),
			content:  fmt.Sprintf("exhausted file %d content that is meaningful for indexing via embedder", i),
			outcomes: []bool{false, false, false}, // always fail
			exists:   true,
		})
	}
	// Permanent failures: nonexistent files.
	for i := 0; i < nPermanent; i++ {
		files = append(files, fileSetup{
			name:   fmt.Sprintf("permanent_%d.txt", i),
			exists: false, // file won't exist → ERR_FILE_UNREADABLE (Permanent)
		})
	}

	// Shuffle for realism.
	rng := rand.New(rand.NewSource(42))
	rng.Shuffle(len(files), func(i, j int) { files[i], files[j] = files[j], files[i] })

	// Build outcome map and create real files.
	outcomes := make(map[string][]bool)
	var paths []string
	for _, f := range files {
		if f.exists {
			fpath := filepath.Join(dir, f.name)
			if err := os.WriteFile(fpath, []byte(f.content), 0o644); err != nil {
				t.Fatalf("write file: %v", err)
			}
			paths = append(paths, fpath)
			outcomes[f.name] = f.outcomes
		} else {
			// Nonexistent path → permanent failure at checkStale.
			paths = append(paths, filepath.Join(dir, "nonexistent", f.name))
		}
	}

	emb := &scriptedEmbedder{outcomes: outcomes}

	ctx, cancel := context.WithCancel(context.Background())
	p := &Pipeline{
		store:       s,
		index:       idx,
		thumbDir:    t.TempDir(),
		logger:      testLogger().WithGroup("indexer"),
		pauseCh:     make(chan struct{}, 1),
		jobCh:       make(chan indexJob, 512),
		saveEveryN:  DefaultPipelineConfig().SaveEveryN,
		workerCount: 2,
		ctx:         ctx,
		cancel:      cancel,
	}
	p.embedder = emb
	p.registry = NewFailureRegistry(10000)
	limiter := embedder.NewRateLimiter(55, time.Minute)
	p.retryCoord = NewRetryCoordinator(p, limiter)
	p.retryCoord.Start()
	for i := 0; i < 2; i++ {
		p.workerWg.Add(1)
		go p.worker()
	}
	t.Cleanup(func() {
		cancel()
		p.retryCoord.Stop()
		p.workerWg.Wait()
	})

	// Snapshot collector — runs in parallel with the pipeline.
	var violations []string
	var snapMu sync.Mutex
	var snapshotCount int

	snapshotDone := make(chan struct{})
	go func() {
		defer close(snapshotDone)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		deadline := time.After(30 * time.Second)
		for {
			select {
			case <-deadline:
				return
			case <-snapshotDone:
				return
			case <-ticker.C:
				st := p.Status()
				total := st.IndexedFiles + st.FailedFiles + st.PendingRetryFiles
				snapMu.Lock()
				snapshotCount++
				if total > st.TotalFiles && st.TotalFiles > 0 {
					violations = append(violations, fmt.Sprintf(
						"snapshot #%d: Indexed=%d + Failed=%d + Pending=%d = %d > Total=%d",
						snapshotCount, st.IndexedFiles, st.FailedFiles, st.PendingRetryFiles, total, st.TotalFiles,
					))
				}
				snapMu.Unlock()
			}
		}
	}()

	// Submit all files.
	p.SetTotalFiles(len(paths))
	for _, fp := range paths {
		p.pendingJobs.Add(1)
		select {
		case p.jobCh <- indexJob{typ: jobSingleFile, filePath: fp}:
		case <-ctx.Done():
			p.pendingJobs.Add(-1)
		}
	}

	// Wait for pipeline to settle: all indexed+failed+pending = 0 pending jobs.
	deadline := time.After(30 * time.Second)
	for {
		st := p.Status()
		// Settled when no pending jobs and either indexing completed or stalled.
		if p.pendingJobs.Load() == 0 && p.retryCoord.PendingCount() == 0 {
			break
		}
		select {
		case <-deadline:
			t.Logf("timeout waiting for pipeline; final status: %+v", p.Status())
			t.Logf("pendingJobs=%d retryPending=%d", p.pendingJobs.Load(), p.retryCoord.PendingCount())
			break
		case <-time.After(10 * time.Millisecond):
		}
		_ = st
	}

	// Signal snapshot goroutine to stop.
	select {
	case snapshotDone <- struct{}{}:
	default:
	}

	// Final invariant check.
	final := p.Status()
	totalFinal := final.IndexedFiles + final.FailedFiles + final.PendingRetryFiles
	if totalFinal > final.TotalFiles {
		t.Errorf("REQ-031: final invariant violated: Indexed=%d + Failed=%d + Pending=%d = %d > Total=%d",
			final.IndexedFiles, final.FailedFiles, final.PendingRetryFiles, totalFinal, final.TotalFiles)
	}

	snapMu.Lock()
	for _, v := range violations {
		t.Errorf("REQ-031: invariant violation during run: %s", v)
	}
	snapCount := snapshotCount
	snapMu.Unlock()

	t.Logf("invariant test: %d snapshots taken, violations=%d", snapCount, len(violations))
	t.Logf("final status: indexed=%d failed=%d pending=%d total=%d",
		final.IndexedFiles, final.FailedFiles, final.PendingRetryFiles, final.TotalFiles)
}
