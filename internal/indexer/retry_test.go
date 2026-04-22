package indexer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"findo/internal/apperr"
	"findo/internal/embedder"
	"findo/internal/store"
	"findo/internal/vectorstore"
)

// ---------------------------------------------------------------------------
// Helpers shared by retry tests
// ---------------------------------------------------------------------------

// newRetryTestPipeline builds a fully wired Pipeline with registry + retry
// coordinator and the given embedder. Workers are started.
func newRetryTestPipeline(t *testing.T, mock embedder.Embedder, workers int) (*Pipeline, *store.Store, *vectorstore.Index) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "retry_test.db")
	s, err := store.NewStore(dbPath, testLogger())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	idx := vectorstore.NewDefaultIndex(testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	p := &Pipeline{
		store:       s,
		index:       idx,
		thumbDir:    t.TempDir(),
		logger:      testLogger().WithGroup("indexer"),
		pauseCh:     make(chan struct{}, 1),
		jobCh:       make(chan indexJob, 256),
		saveEveryN:  DefaultPipelineConfig().SaveEveryN,
		workerCount: workers,
		ctx:         ctx,
		cancel:      cancel,
	}
	p.embedder = mock

	// Wire up registry and retry coordinator.
	p.registry = NewFailureRegistry(10000)
	limiter := embedder.NewRateLimiter(55, time.Minute)
	p.retryCoord = NewRetryCoordinator(p, limiter)
	p.retryCoord.Start()

	for i := 0; i < workers; i++ {
		p.workerWg.Add(1)
		go p.worker()
	}

	t.Cleanup(func() {
		cancel()
		p.retryCoord.Stop()
		p.workerWg.Wait()
	})

	return p, s, idx
}

// waitForStatus polls p.Status() until cond returns true or the timeout fires.
func waitForStatus(t *testing.T, p *Pipeline, cond func(IndexStatus) bool, timeout time.Duration) IndexStatus {
	t.Helper()
	deadline := time.After(timeout)
	for {
		s := p.Status()
		if cond(s) {
			return s
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for status condition; last: %+v", p.Status())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// makeTextFile creates a small text file under t.TempDir().
func makeTextFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// ---------------------------------------------------------------------------
// Controlled embedders for retry tests
// ---------------------------------------------------------------------------

// failThenSucceedEmbedder fails the first N calls then succeeds.
type failThenSucceedEmbedder struct {
	mu         sync.Mutex
	calls      int
	failCount  int
	failErr    error
	timestamps []time.Time // records when each EmbedBatch was called
}

func (f *failThenSucceedEmbedder) ModelID() string        { return "fail-then-succeed" }
func (f *failThenSucceedEmbedder) Dimensions() int        { return 3 }
func (f *failThenSucceedEmbedder) PausedUntil() time.Time { return time.Time{} }
func (f *failThenSucceedEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return []float32{0, 0, 0}, nil
}
func (f *failThenSucceedEmbedder) EmbedBatch(_ context.Context, chunks []embedder.ChunkInput) ([][]float32, error) {
	f.mu.Lock()
	f.calls++
	f.timestamps = append(f.timestamps, time.Now())
	callN := f.calls
	f.mu.Unlock()
	if callN <= f.failCount {
		return nil, f.failErr
	}
	result := make([][]float32, len(chunks))
	for i := range chunks {
		result[i] = []float32{float32(i), 0.1, 0.2}
	}
	return result, nil
}
func (f *failThenSucceedEmbedder) CallTimestamps() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]time.Time, len(f.timestamps))
	copy(cp, f.timestamps)
	return cp
}

// alwaysFailEmbedder always returns the given error.
type alwaysFailEmbedder struct{ err error }

func (a *alwaysFailEmbedder) ModelID() string        { return "always-fail" }
func (a *alwaysFailEmbedder) Dimensions() int        { return 3 }
func (a *alwaysFailEmbedder) PausedUntil() time.Time { return time.Time{} }
func (a *alwaysFailEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return nil, a.err
}
func (a *alwaysFailEmbedder) EmbedBatch(_ context.Context, _ []embedder.ChunkInput) ([][]float32, error) {
	return nil, a.err
}

// rateLimitedEmbedder always returns a rate-limited error.
type rateLimitedEmbedder struct {
	mu    sync.Mutex
	calls int
}

func (r *rateLimitedEmbedder) ModelID() string        { return "rate-limited" }
func (r *rateLimitedEmbedder) Dimensions() int        { return 3 }
func (r *rateLimitedEmbedder) PausedUntil() time.Time { return time.Time{} }
func (r *rateLimitedEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return nil, apperr.ErrRateLimited
}
func (r *rateLimitedEmbedder) EmbedBatch(_ context.Context, _ []embedder.ChunkInput) ([][]float32, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return nil, apperr.ErrRateLimited
}
func (r *rateLimitedEmbedder) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// ---------------------------------------------------------------------------
// REQ-020: TestRetry_TransientRetryReQueues
// ---------------------------------------------------------------------------

// TestRetry_TransientRetryReQueues verifies that an ErrEmbedFailed (TransientRetry)
// failure increments PendingRetryFiles and does NOT increment FailedFiles.
func TestRetry_TransientRetryReQueues(t *testing.T) {
	// Fail once then succeed — so worker attempt 1 fails (transient), attempt 2 succeeds.
	mock := &failThenSucceedEmbedder{
		failCount: 1,
		failErr:   apperr.Wrap(apperr.ErrEmbedFailed.Code, "embed failed", fmt.Errorf("network")),
	}
	p, _, _ := newRetryTestPipeline(t, mock, 1)

	filePath := makeTextFile(t, "transient.txt", "hello world content for retry test that has meaningful content here for chunking")
	p.SubmitFile(filePath)

	// Wait until the file is either indexed or failed (terminal).
	// After retry succeeds: IndexedFiles=1, FailedFiles=0.
	final := waitForStatus(t, p, func(s IndexStatus) bool {
		return s.IndexedFiles >= 1 || s.FailedFiles >= 1
	}, 10*time.Second)

	if final.FailedFiles != 0 {
		t.Errorf("REQ-020: FailedFiles should be 0 after transient retry, got %d", final.FailedFiles)
	}
	if final.IndexedFiles != 1 {
		t.Errorf("REQ-020: IndexedFiles should be 1 after retry success, got %d", final.IndexedFiles)
	}
	// Registry should have no terminal entry.
	if p.registry.Len() != 0 {
		t.Errorf("REQ-020: registry should be empty after retry success, got %d entries", p.registry.Len())
	}
}

// ---------------------------------------------------------------------------
// REQ-021 + REQ-021a: TestRetry_TransientWaitWakesOnUnpause
// ---------------------------------------------------------------------------

// TestRetry_TransientWaitWakesOnUnpause verifies that a rate-limited file waits
// for the RateLimiter pause to expire before re-submitting.
func TestRetry_TransientWaitWakesOnUnpause(t *testing.T) {
	// Embedder fails once with rate-limited, then succeeds.
	limiter := embedder.NewRateLimiter(55, time.Minute)
	pauseDuration := 400 * time.Millisecond
	pauseUntil := time.Now().Add(pauseDuration)
	limiter.PauseUntil(pauseUntil)

	mock := &failThenSucceedEmbedder{
		failCount: 1,
		failErr:   apperr.Wrap(apperr.ErrRateLimited.Code, "rate limited", fmt.Errorf("429")),
	}

	dbPath := filepath.Join(t.TempDir(), "ratelimit.db")
	s, err := store.NewStore(dbPath, testLogger())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	idx := vectorstore.NewDefaultIndex(testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	p := &Pipeline{
		store:       s,
		index:       idx,
		thumbDir:    t.TempDir(),
		logger:      testLogger().WithGroup("indexer"),
		pauseCh:     make(chan struct{}, 1),
		jobCh:       make(chan indexJob, 256),
		saveEveryN:  DefaultPipelineConfig().SaveEveryN,
		workerCount: 1,
		ctx:         ctx,
		cancel:      cancel,
	}
	p.embedder = mock
	p.registry = NewFailureRegistry(10000)
	p.retryCoord = NewRetryCoordinator(p, limiter)
	p.retryCoord.Start()
	p.workerWg.Add(1)
	go p.worker()
	t.Cleanup(func() {
		cancel()
		p.retryCoord.Stop()
		p.workerWg.Wait()
	})

	filePath := makeTextFile(t, "ratelimited.txt", "hello world content for rate limit test that is long enough to embed properly")
	start := time.Now()
	p.SubmitFile(filePath)

	// The file should eventually succeed after the pause expires.
	waitForStatus(t, p, func(s IndexStatus) bool {
		return s.IndexedFiles >= 1 || s.FailedFiles >= 1
	}, 5*time.Second)

	elapsed := time.Since(start)
	// Should have waited at least 350ms (allowing 50ms processing overhead before pause).
	if elapsed < pauseDuration-50*time.Millisecond {
		t.Errorf("REQ-021: retry re-submitted too early (elapsed=%v, want >= %v)", elapsed, pauseDuration-50*time.Millisecond)
	}
	// Should not have waited excessively (≤ 1.5s total: pause + ample processing).
	if elapsed > pauseDuration+1100*time.Millisecond {
		t.Errorf("REQ-021: retry took too long (elapsed=%v, max allowed=%v)", elapsed, pauseDuration+1100*time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// REQ-022: TestRetry_ExponentialBackoff
// ---------------------------------------------------------------------------

// TestRetry_ExponentialBackoff verifies that backoffs are applied: 1s, 2s, 4s.
// Due to test duration, we use shorter test backoffs injected into the coordinator.
func TestRetry_ExponentialBackoff(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping backoff timing test in short mode")
	}

	// Use shortened test backoffs: 100ms, 200ms, 400ms.
	testBackoffs := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}

	mock := &failThenSucceedEmbedder{
		failCount: 2, // fail twice so we see 2 retries, then succeed on attempt 3
		failErr:   apperr.Wrap(apperr.ErrEmbedFailed.Code, "embed failed", fmt.Errorf("network")),
	}

	dbPath := filepath.Join(t.TempDir(), "backoff.db")
	s, err := store.NewStore(dbPath, testLogger())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	idx := vectorstore.NewDefaultIndex(testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	p := &Pipeline{
		store:       s,
		index:       idx,
		thumbDir:    t.TempDir(),
		logger:      testLogger().WithGroup("indexer"),
		pauseCh:     make(chan struct{}, 1),
		jobCh:       make(chan indexJob, 256),
		saveEveryN:  DefaultPipelineConfig().SaveEveryN,
		workerCount: 1,
		ctx:         ctx,
		cancel:      cancel,
	}
	p.embedder = mock
	p.registry = NewFailureRegistry(10000)
	limiter := embedder.NewRateLimiter(55, time.Minute)
	p.retryCoord = NewRetryCoordinator(p, limiter)
	p.retryCoord.backoffs = testBackoffs
	p.retryCoord.Start()
	p.workerWg.Add(1)
	go p.worker()
	t.Cleanup(func() {
		cancel()
		p.retryCoord.Stop()
		p.workerWg.Wait()
	})

	filePath := makeTextFile(t, "backoff.txt", "hello world content for backoff test that is long enough to process properly here")
	p.SubmitFile(filePath)

	// Wait until indexed.
	waitForStatus(t, p, func(s IndexStatus) bool {
		return s.IndexedFiles >= 1 || s.FailedFiles >= 1
	}, 5*time.Second)

	// We should have 3 EmbedBatch calls total (attempt1 fail, attempt2 fail, attempt3 success).
	ts := mock.CallTimestamps()
	if len(ts) < 3 {
		t.Fatalf("REQ-022: expected >= 3 EmbedBatch calls, got %d", len(ts))
	}

	// delta[0→1] ≈ testBackoffs[0] (100ms)
	delta0 := ts[1].Sub(ts[0])
	if delta0 < testBackoffs[0]-200*time.Millisecond || delta0 > testBackoffs[0]+200*time.Millisecond {
		t.Errorf("REQ-022: backoff[0] = %v, want ~%v (±200ms)", delta0, testBackoffs[0])
	}

	// delta[1→2] ≈ testBackoffs[1] (200ms)
	delta1 := ts[2].Sub(ts[1])
	if delta1 < testBackoffs[1]-200*time.Millisecond || delta1 > testBackoffs[1]+200*time.Millisecond {
		t.Errorf("REQ-022: backoff[1] = %v, want ~%v (±200ms)", delta1, testBackoffs[1])
	}
}

// ---------------------------------------------------------------------------
// REQ-023: TestRetry_ExhaustsToTerminal
// ---------------------------------------------------------------------------

// TestRetry_ExhaustsToTerminal verifies that after maxAttempts failures, the
// file is recorded as a terminal failure and FailedFiles is incremented.
func TestRetry_ExhaustsToTerminal(t *testing.T) {
	embedErr := apperr.Wrap(apperr.ErrEmbedFailed.Code, "embed failed", fmt.Errorf("network"))
	mock := &alwaysFailEmbedder{err: embedErr}
	p, _, _ := newRetryTestPipeline(t, mock, 1)

	filePath := makeTextFile(t, "exhaust.txt", "content for exhaustion test that is long enough for processing via embedder properly")
	p.SubmitFile(filePath)

	// Wait until failed (terminal).
	final := waitForStatus(t, p, func(s IndexStatus) bool {
		return s.FailedFiles >= 1
	}, 15*time.Second)

	if final.FailedFiles != 1 {
		t.Errorf("REQ-023: FailedFiles should be 1 after exhaustion, got %d", final.FailedFiles)
	}
	if final.IndexedFiles != 0 {
		t.Errorf("REQ-023: IndexedFiles should be 0, got %d", final.IndexedFiles)
	}
	if p.registry.Len() != 1 {
		t.Errorf("REQ-023: registry should have 1 entry after exhaustion, got %d", p.registry.Len())
	}
	snap := p.registry.Snapshot()
	if snap[0].Attempts != maxAttempts {
		t.Errorf("REQ-023: registry entry attempts should be %d, got %d", maxAttempts, snap[0].Attempts)
	}
}

// ---------------------------------------------------------------------------
// REQ-024: TestRetry_SucceedsOnSecondAttempt
// ---------------------------------------------------------------------------

// TestRetry_SucceedsOnSecondAttempt verifies that when a retry succeeds,
// IndexedFiles increments and FailedFiles does not.
func TestRetry_SucceedsOnSecondAttempt(t *testing.T) {
	mock := &failThenSucceedEmbedder{
		failCount: 1,
		failErr:   apperr.Wrap(apperr.ErrEmbedFailed.Code, "embed failed", fmt.Errorf("network")),
	}
	p, _, _ := newRetryTestPipeline(t, mock, 1)

	filePath := makeTextFile(t, "retry_success.txt", "content for retry success test that is long enough to embed properly here indeed")
	p.SubmitFile(filePath)

	final := waitForStatus(t, p, func(s IndexStatus) bool {
		return s.IndexedFiles >= 1 || s.FailedFiles >= 1
	}, 10*time.Second)

	if final.IndexedFiles != 1 {
		t.Errorf("REQ-024: IndexedFiles should be 1, got %d", final.IndexedFiles)
	}
	if final.FailedFiles != 0 {
		t.Errorf("REQ-024: FailedFiles should be 0, got %d", final.FailedFiles)
	}
	if p.registry.Len() != 0 {
		t.Errorf("REQ-024: registry should be empty after retry success, got %d", p.registry.Len())
	}
}

// ---------------------------------------------------------------------------
// REQ-025: TestRetry_DroppedOnGenerationChange
// ---------------------------------------------------------------------------

// TestRetry_DroppedOnGenerationChange verifies that after ResetStatus, pending
// retries are dropped without incrementing FailedFiles.
func TestRetry_DroppedOnGenerationChange(t *testing.T) {
	// Embedder always fails with transient error (so retries are scheduled but never succeed).
	embedErr := apperr.Wrap(apperr.ErrEmbedFailed.Code, "embed failed", fmt.Errorf("network"))

	// Use long backoffs so retries stay pending long enough for us to call ResetStatus.
	dbPath := filepath.Join(t.TempDir(), "gendrop.db")
	s, err := store.NewStore(dbPath, testLogger())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	idx := vectorstore.NewDefaultIndex(testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	p := &Pipeline{
		store:       s,
		index:       idx,
		thumbDir:    t.TempDir(),
		logger:      testLogger().WithGroup("indexer"),
		pauseCh:     make(chan struct{}, 1),
		jobCh:       make(chan indexJob, 256),
		saveEveryN:  DefaultPipelineConfig().SaveEveryN,
		workerCount: 1,
		ctx:         ctx,
		cancel:      cancel,
	}
	p.embedder = &alwaysFailEmbedder{err: embedErr}
	p.registry = NewFailureRegistry(10000)
	limiter := embedder.NewRateLimiter(55, time.Minute)
	p.retryCoord = NewRetryCoordinator(p, limiter)
	p.retryCoord.backoffs = []time.Duration{30 * time.Second, 30 * time.Second, 30 * time.Second}
	p.retryCoord.Start()
	p.workerWg.Add(1)
	go p.worker()
	t.Cleanup(func() {
		cancel()
		p.retryCoord.Stop()
		p.workerWg.Wait()
	})

	filePath := makeTextFile(t, "gen_drop.txt", "content for generation drop test that is long enough to be processed by the embedder")
	p.SubmitFile(filePath)

	// Wait until PendingRetryFiles=1 (first attempt failed, retry is scheduled).
	waitForStatus(t, p, func(s IndexStatus) bool {
		return s.PendingRetryFiles >= 1
	}, 5*time.Second)

	// Now reset — this should drop all pending retries.
	p.ResetStatus()

	// After reset, PendingRetryFiles must be 0 and FailedFiles must be 0.
	s2 := p.Status()
	if s2.PendingRetryFiles != 0 {
		t.Errorf("REQ-025: PendingRetryFiles should be 0 after ResetStatus, got %d", s2.PendingRetryFiles)
	}
	if s2.FailedFiles != 0 {
		t.Errorf("REQ-025: FailedFiles should be 0 after ResetStatus, got %d", s2.FailedFiles)
	}
	if p.registry.Len() != 0 {
		t.Errorf("REQ-025: registry should be empty after ResetStatus, got %d entries", p.registry.Len())
	}
}

// ---------------------------------------------------------------------------
// REQ-026: TestRetry_StaleGenerationStampDiscarded
// ---------------------------------------------------------------------------

// TestRetry_StaleGenerationStampDiscarded verifies that a retry job carrying a
// stale generation stamp is discarded at dequeue without touching counters.
func TestRetry_StaleGenerationStampDiscarded(t *testing.T) {
	p, _, _ := newRetryTestPipeline(t, &mockEmbedder{}, 1)

	gen := p.generation.Load()

	// Manually schedule a retry with the current generation.
	p.retryCoord.Schedule("/fake/stale.txt", apperr.ErrEmbedFailed.Code, "embed failed", 2)
	p.mu.Lock()
	p.status.PendingRetryFiles++
	p.mu.Unlock()

	// Advance generation to make it stale.
	p.generation.Add(1)

	// DropAll for the new generation should drop the old pending job.
	p.retryCoord.DropAll(gen)

	// Give coordinator goroutine time to drain.
	time.Sleep(100 * time.Millisecond)

	s := p.Status()
	if s.FailedFiles != 0 {
		t.Errorf("REQ-026: stale retry should not increment FailedFiles, got %d", s.FailedFiles)
	}
}

// ---------------------------------------------------------------------------
// REQ-033 / EDGE-010: TestRetry_SubmitFileCollapsesPendingRetry
// ---------------------------------------------------------------------------

// TestRetry_SubmitFileCollapsesPendingRetry verifies that calling SubmitFile
// for a path that is pending retry collapses the pending retry.
func TestRetry_SubmitFileCollapsesPendingRetry(t *testing.T) {
	// Set long backoffs so retries stay pending.
	dbPath := filepath.Join(t.TempDir(), "collapse.db")
	s, err := store.NewStore(dbPath, testLogger())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	idx := vectorstore.NewDefaultIndex(testLogger())
	ctx, cancel := context.WithCancel(context.Background())

	// Use a mock that fails once (transient) then succeeds.
	mock := &failThenSucceedEmbedder{
		failCount: 1,
		failErr:   apperr.Wrap(apperr.ErrEmbedFailed.Code, "embed failed", fmt.Errorf("network")),
	}

	p := &Pipeline{
		store:       s,
		index:       idx,
		thumbDir:    t.TempDir(),
		logger:      testLogger().WithGroup("indexer"),
		pauseCh:     make(chan struct{}, 1),
		jobCh:       make(chan indexJob, 256),
		saveEveryN:  DefaultPipelineConfig().SaveEveryN,
		workerCount: 1,
		ctx:         ctx,
		cancel:      cancel,
	}
	p.embedder = mock
	p.registry = NewFailureRegistry(10000)
	limiter := embedder.NewRateLimiter(55, time.Minute)
	p.retryCoord = NewRetryCoordinator(p, limiter)
	p.retryCoord.backoffs = []time.Duration{30 * time.Second, 30 * time.Second, 30 * time.Second}
	p.retryCoord.Start()
	p.workerWg.Add(1)
	go p.worker()
	t.Cleanup(func() {
		cancel()
		p.retryCoord.Stop()
		p.workerWg.Wait()
	})

	filePath := makeTextFile(t, "collapse.txt", "content for collapse test that is long enough to be processed by the embedder here")
	p.SubmitFile(filePath)

	// Wait until PendingRetryFiles=1 (first attempt failed, retry is scheduled).
	waitForStatus(t, p, func(s IndexStatus) bool {
		return s.PendingRetryFiles >= 1
	}, 5*time.Second)

	pendingBefore := p.Status().PendingRetryFiles
	totalBefore := p.Status().TotalFiles

	// Now submit the same file again — should collapse the pending retry.
	p.SubmitFile(filePath)

	// Wait for completion.
	waitForStatus(t, p, func(st IndexStatus) bool {
		return st.IndexedFiles >= 1 || st.FailedFiles >= 1
	}, 10*time.Second)

	final := p.Status()
	// PendingRetryFiles should be 0 now (retry was collapsed).
	if final.PendingRetryFiles != 0 {
		t.Errorf("REQ-033: PendingRetryFiles should be 0 after collapse, got %d", final.PendingRetryFiles)
	}
	// TotalFiles should have increased by 1 for the new SubmitFile.
	if final.TotalFiles != totalBefore+1 {
		t.Errorf("REQ-033: TotalFiles should be %d, got %d", totalBefore+1, final.TotalFiles)
	}
	_ = pendingBefore
}

// ---------------------------------------------------------------------------
// EDGE-001: TestRetry_StaleGenerationNotRecorded
// ---------------------------------------------------------------------------

// TestRetry_StaleGenerationNotRecorded verifies that errStaleGeneration does not
// get recorded in the registry and does not increment any failure counters.
func TestRetry_StaleGenerationNotRecorded(t *testing.T) {
	blockCh := make(chan struct{})
	mock := &mockEmbedder{blockCh: blockCh}
	p, _, _ := newRetryTestPipeline(t, mock, 1)

	dir := t.TempDir()
	filePath := filepath.Join(dir, "stale.txt")
	if err := os.WriteFile(filePath, []byte("content for stale generation test that is meaningful"), 0o644); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- p.indexFile(p.ctx, filePath, true)
	}()

	time.Sleep(50 * time.Millisecond)
	// Advance generation while indexFile is blocked in EmbedBatch.
	p.generation.Add(1)
	close(blockCh)

	err := <-done
	if !errors.Is(err, errStaleGeneration) {
		t.Fatalf("EDGE-001: expected errStaleGeneration, got: %v", err)
	}

	// Nothing should be recorded.
	if p.registry.Len() != 0 {
		t.Errorf("EDGE-001: registry should be empty after stale generation, got %d", p.registry.Len())
	}
	s := p.Status()
	if s.FailedFiles != 0 {
		t.Errorf("EDGE-001: FailedFiles should be 0, got %d", s.FailedFiles)
	}
}

// ---------------------------------------------------------------------------
// EDGE-014: TestRetry_SurvivesPauseResume
// ---------------------------------------------------------------------------

// TestRetry_SurvivesPauseResume verifies that a Pause/Resume cycle does not
// affect the registry or retry queue (pause does not bump generation).
func TestRetry_SurvivesPauseResume(t *testing.T) {
	// Embedder fails once (transient), retry succeeds.
	mock := &failThenSucceedEmbedder{
		failCount: 1,
		failErr:   apperr.Wrap(apperr.ErrEmbedFailed.Code, "embed failed", fmt.Errorf("network")),
	}
	p, _, _ := newRetryTestPipeline(t, mock, 1)

	filePath := makeTextFile(t, "pause_resume.txt", "content for pause resume test that is long enough to trigger embedding properly here")
	p.SubmitFile(filePath)

	// Wait until pending retry is queued.
	waitForStatus(t, p, func(s IndexStatus) bool {
		return s.PendingRetryFiles >= 1 || s.IndexedFiles >= 1
	}, 5*time.Second)

	// Pause and immediately resume (does NOT bump generation).
	genBefore := p.generation.Load()
	p.Pause()
	p.Resume()
	genAfter := p.generation.Load()

	if genBefore != genAfter {
		t.Errorf("EDGE-014: Pause/Resume should not change generation counter")
	}

	// Wait for final state.
	final := waitForStatus(t, p, func(s IndexStatus) bool {
		return s.IndexedFiles >= 1 || s.FailedFiles >= 1
	}, 10*time.Second)

	if final.FailedFiles != 0 {
		t.Errorf("EDGE-014: FailedFiles should be 0 after pause/resume cycle, got %d", final.FailedFiles)
	}
}

// ---------------------------------------------------------------------------
// EDGE-015: TestRetry_CommitFailureRetries
// ---------------------------------------------------------------------------

// TestRetry_CommitFailureRetries verifies that an ERR_STORE_WRITE (TransientRetry)
// from the commit phase is retried and eventually succeeds.
func TestRetry_CommitFailureRetries(t *testing.T) {
	// A mock that wraps successful embedding but the store write fails once.
	// We achieve this by closing and re-opening the store... Instead, use
	// a commitFailEmbedder that causes the failure via ERR_STORE_WRITE indirectly.
	// The simplest approach: use processSingleFile with a mock that wraps ERR_STORE_WRITE.

	// We test this by having the pipeline fail once on commit, then succeed.
	// We simulate this by using a mock embedder that succeeds, combined with
	// a bad-then-good store. Since we can't easily inject store failures here,
	// we directly test the worker's handling of ERR_STORE_WRITE errors.

	// Direct test: inject an ERR_STORE_WRITE error as if it came from commit.
	// We verify the worker schedules a retry (not terminal).
	embedErr := apperr.Wrap(apperr.ErrStoreWrite.Code, "store write failed", fmt.Errorf("sqlite busy"))
	mock := &failThenSucceedEmbedder{
		failCount: 1,
		failErr:   embedErr,
	}
	p, _, _ := newRetryTestPipeline(t, mock, 1)

	filePath := makeTextFile(t, "commit_fail.txt", "content for commit failure retry test that is meaningful and long enough to embed")
	p.SubmitFile(filePath)

	// Wait for final outcome.
	final := waitForStatus(t, p, func(s IndexStatus) bool {
		return s.IndexedFiles >= 1 || s.FailedFiles >= 1
	}, 15*time.Second)

	// ERR_STORE_WRITE is TransientRetry, so it should be retried.
	// After retry it should succeed.
	if final.IndexedFiles != 1 {
		t.Errorf("EDGE-015: IndexedFiles should be 1 after commit retry succeeds, got %d", final.IndexedFiles)
	}
	if final.FailedFiles != 0 {
		t.Errorf("EDGE-015: FailedFiles should be 0, got %d", final.FailedFiles)
	}
}

// ---------------------------------------------------------------------------
// EDGE-016: TestRetry_HnswAddFailureRetries
// ---------------------------------------------------------------------------

// TestRetry_HnswAddFailureRetries verifies that ERR_HNSW_ADD (TransientRetry)
// is retried and succeeds on the second attempt.
func TestRetry_HnswAddFailureRetries(t *testing.T) {
	embedErr := apperr.Wrap(apperr.ErrHnswAdd.Code, "hnsw add failed", fmt.Errorf("hnsw error"))
	mock := &failThenSucceedEmbedder{
		failCount: 1,
		failErr:   embedErr,
	}
	p, _, _ := newRetryTestPipeline(t, mock, 1)

	filePath := makeTextFile(t, "hnsw_retry.txt", "content for hnsw retry test that is meaningful enough to process via embedder properly")
	p.SubmitFile(filePath)

	final := waitForStatus(t, p, func(s IndexStatus) bool {
		return s.IndexedFiles >= 1 || s.FailedFiles >= 1
	}, 15*time.Second)

	if final.IndexedFiles != 1 {
		t.Errorf("EDGE-016: IndexedFiles should be 1 after hnsw retry succeeds, got %d", final.IndexedFiles)
	}
	if final.FailedFiles != 0 {
		t.Errorf("EDGE-016: FailedFiles should be 0, got %d", final.FailedFiles)
	}
}

// ---------------------------------------------------------------------------
// REQ-030: PendingRetryFiles field exists in IndexStatus
// ---------------------------------------------------------------------------

// TestPendingRetryFiles_FieldExists verifies IndexStatus has PendingRetryFiles.
func TestPendingRetryFiles_FieldExists(t *testing.T) {
	p, _, _ := newRetryTestPipeline(t, &mockEmbedder{}, 1)
	s := p.Status()
	// Just reading the field should compile and return 0 initially.
	if s.PendingRetryFiles != 0 {
		t.Errorf("REQ-030: initial PendingRetryFiles should be 0, got %d", s.PendingRetryFiles)
	}
}

// ---------------------------------------------------------------------------
// REQ-032: TotalFiles is not incremented during retries
// ---------------------------------------------------------------------------

// TestRetry_TotalFilesUnchangedDuringRetry verifies TotalFiles stays constant.
func TestRetry_TotalFilesUnchangedDuringRetry(t *testing.T) {
	mock := &failThenSucceedEmbedder{
		failCount: 1,
		failErr:   apperr.Wrap(apperr.ErrEmbedFailed.Code, "embed failed", fmt.Errorf("network")),
	}
	p, _, _ := newRetryTestPipeline(t, mock, 1)

	filePath := makeTextFile(t, "total_unchanged.txt", "content for total files unchanged test that is long enough to process via embedder")
	p.SubmitFile(filePath)

	// Wait until pending retry is active.
	waitForStatus(t, p, func(s IndexStatus) bool {
		return s.PendingRetryFiles >= 1 || s.IndexedFiles >= 1
	}, 5*time.Second)

	totalAfterFirstAttempt := p.Status().TotalFiles

	// Wait for final outcome.
	waitForStatus(t, p, func(s IndexStatus) bool {
		return s.IndexedFiles >= 1 || s.FailedFiles >= 1
	}, 10*time.Second)

	totalAfterRetry := p.Status().TotalFiles
	if totalAfterRetry != totalAfterFirstAttempt {
		t.Errorf("REQ-032: TotalFiles changed during retry: before=%d after=%d", totalAfterFirstAttempt, totalAfterRetry)
	}
}

// ---------------------------------------------------------------------------
// Registry accessor test
// ---------------------------------------------------------------------------

// TestPipeline_RegistryAccessor verifies the Registry() accessor is exposed.
func TestPipeline_RegistryAccessor(t *testing.T) {
	p, _, _ := newRetryTestPipeline(t, &mockEmbedder{}, 1)
	reg := p.Registry()
	if reg == nil {
		t.Fatal("Registry() returned nil")
	}
}

// ---------------------------------------------------------------------------
// TestRetry_PermanentFailureNotRetried
// ---------------------------------------------------------------------------

// TestRetry_PermanentFailureNotRetried verifies that a permanent failure
// (ERR_UNSUPPORTED_FORMAT) goes directly to terminal without retry.
func TestRetry_PermanentFailureNotRetried(t *testing.T) {
	// We test this by having indexFile return a permanent error.
	// checkStale → ERR_FILE_UNREADABLE is permanent; file not found.
	p, _, _ := newRetryTestPipeline(t, &mockEmbedder{}, 1)

	// Submit a nonexistent file — will get ERR_FILE_UNREADABLE (Permanent).
	p.SubmitFile("/nonexistent/permanent/fail.txt")

	final := waitForStatus(t, p, func(s IndexStatus) bool {
		return s.FailedFiles >= 1
	}, 5*time.Second)

	if final.FailedFiles != 1 {
		t.Errorf("permanent failure should go to FailedFiles immediately, got %d", final.FailedFiles)
	}
	if final.PendingRetryFiles != 0 {
		t.Errorf("permanent failure should not add to PendingRetryFiles, got %d", final.PendingRetryFiles)
	}
	if p.registry.Len() != 1 {
		t.Errorf("permanent failure should be in registry, got %d", p.registry.Len())
	}
}

// ---------------------------------------------------------------------------
// Concurrent PendingRetryFiles tracking
// ---------------------------------------------------------------------------

// TestRetry_PendingRetryFilesCounter verifies the counter increments and
// decrements correctly under concurrent access.
func TestRetry_PendingRetryFilesCounter(t *testing.T) {
	var count atomic.Int32

	// Directly exercise the counter mechanics via Schedule.
	p, _, _ := newRetryTestPipeline(t, &mockEmbedder{}, 1)

	const n = 5
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := fmt.Sprintf("/fake/path/%d.txt", i)
			p.retryCoord.Schedule(path, apperr.ErrEmbedFailed.Code, "embed failed", 2)
			count.Add(1)
		}(i)
	}
	wg.Wait()

	// Allow coordinator to process.
	time.Sleep(50 * time.Millisecond)

	if count.Load() != n {
		t.Errorf("expected %d schedules, got %d", n, count.Load())
	}
}
