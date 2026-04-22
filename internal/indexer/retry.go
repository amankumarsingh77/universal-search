package indexer

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"findo/internal/apperr"
	"findo/internal/embedder"
)

// defaultBackoffs are the exponential backoff durations used by the retry
// coordinator for TransientRetry errors: 1s before attempt 2, 2s before
// attempt 3, 4s before attempt 4 (which would be terminal since maxAttempts=3).
var defaultBackoffs = []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

// retryJob carries a single retry attempt through the retry coordinator's queue.
// pathStamp captures the per-path cancel stamp at the time the job was scheduled;
// if the stamp has advanced by dequeue time the job is silently dropped (EDGE-010).
type retryJob struct {
	path        string
	code        string
	message     string
	attempts    int   // attempt number for the next indexJob (≥2)
	generation  int32 // pipeline generation when scheduled
	pathStamp   int64 // pathStamps[path] at schedule time; used for EDGE-010 detection
	scheduledAt time.Time
}

// RetryCoordinator dispatches transient failures back into the indexing pipeline
// after a suitable wait (either a timed backoff or a rate-limiter unpause).
//
// PendingRetryFiles lifecycle:
//   - Schedule() increments pipeline.status.PendingRetryFiles (inside p.mu) before returning.
//   - The decrement happens exactly once, in decPending(), which is called when the
//     coordinator drops or re-submits the job.
//
// EDGE-010 (SubmitFile collision) handling:
//   - Each path has a per-path stamp in pathStamps (incremented by CancelPath).
//   - Schedule() embeds the current stamp in the retryJob.pathStamp field.
//   - When SubmitFile calls CancelPath, the stamp is bumped; the job's stamp no
//     longer matches, so the coordinator drops it silently at dequeue.
//   - SubmitFile also decrements the external counters (PendingRetryFiles,
//     retryCoord.pendingCount) immediately, so decPending will not double-decrement:
//     the job's pathStamp mismatch causes it to be dropped via a fast-path that
//     skips decPending (the counters were already decremented by SubmitFile).
type RetryCoordinator struct {
	pipeline *Pipeline
	limiter  *embedder.RateLimiter

	retryCh chan retryJob

	ctx    context.Context
	cancel context.CancelFunc

	pendingCount atomic.Int32

	maxAttempts int
	backoffs    []time.Duration

	pathMu     sync.Mutex
	pathStamps map[string]int64 // path → monotonic cancel stamp

	stopWg sync.WaitGroup
}

// NewRetryCoordinator creates a RetryCoordinator wired to the given pipeline
// and rate limiter. Call Start() to begin processing.
func NewRetryCoordinator(p *Pipeline, limiter *embedder.RateLimiter) *RetryCoordinator {
	ctx, cancel := context.WithCancel(p.ctx)
	return &RetryCoordinator{
		pipeline:    p,
		limiter:     limiter,
		retryCh:     make(chan retryJob, 512),
		ctx:         ctx,
		cancel:      cancel,
		maxAttempts: maxAttempts,
		backoffs:    defaultBackoffs,
		pathStamps:  make(map[string]int64),
	}
}

// Start spawns the coordinator goroutine. Must be called once.
func (rc *RetryCoordinator) Start() {
	rc.stopWg.Add(1)
	go rc.loop()
}

// Stop cancels the coordinator's context and waits for the goroutine to exit.
func (rc *RetryCoordinator) Stop() {
	rc.cancel()
	rc.stopWg.Wait()
}

// Schedule enqueues a retry job for path. The caller is responsible for
// incrementing pipeline.status.PendingRetryFiles (inside p.mu) immediately after.
// nextAttempt is the attempt number for the re-submitted job (≥2).
func (rc *RetryCoordinator) Schedule(path, code, message string, nextAttempt int) {
	rc.pathMu.Lock()
	stamp := rc.pathStamps[path]
	rc.pathMu.Unlock()

	job := retryJob{
		path:        path,
		code:        code,
		message:     message,
		attempts:    nextAttempt,
		generation:  rc.pipeline.generation.Load(),
		pathStamp:   stamp,
		scheduledAt: time.Now(),
	}

	select {
	case rc.retryCh <- job:
		rc.pendingCount.Add(1)
	case <-rc.ctx.Done():
	}
}

// PendingCount returns the number of retry jobs currently queued or waiting.
func (rc *RetryCoordinator) PendingCount() int {
	return int(rc.pendingCount.Load())
}

// CancelPath bumps the per-path cancel stamp for path so that any pending retry
// job for it will be silently dropped at dequeue without touching the counters
// (which SubmitFile already decremented). Returns true if a stamp existed (likely
// indicating an active pending retry).
//
// EDGE-010: called by SubmitFile before submitting a fresh job for a path that
// may currently be in the retry queue. SubmitFile has already decremented both
// PendingRetryFiles and pendingCount, so the coordinator must not call decPending
// again — hence the fast-path in process() that checks pathStamp.
func (rc *RetryCoordinator) CancelPath(path string) bool {
	rc.pathMu.Lock()
	defer rc.pathMu.Unlock()
	_, had := rc.pathStamps[path]
	rc.pathStamps[path]++
	return had
}

// DropAll cancels all pending retries. Typically called from ResetStatus.
// It drains the buffered channel and decrements pendingCount for each drained job.
// The pipeline lock is expected to be held by the caller to zero PendingRetryFiles.
func (rc *RetryCoordinator) DropAll(_ int32) {
	// Bump all path stamps so any jobs that escape the drain (in-flight in process())
	// will also be dropped via the pathStamp check.
	rc.pathMu.Lock()
	for k := range rc.pathStamps {
		rc.pathStamps[k]++
	}
	rc.pathMu.Unlock()

	// Drain the buffered channel.
	drained := 0
	for {
		select {
		case <-rc.retryCh:
			drained++
		default:
			goto done
		}
	}
done:
	if drained > 0 {
		rc.pendingCount.Add(-int32(drained))
	}
}

// loop is the coordinator's single processing goroutine.
func (rc *RetryCoordinator) loop() {
	defer rc.stopWg.Done()
	for {
		select {
		case job := <-rc.retryCh:
			rc.process(job)
		case <-rc.ctx.Done():
			// Drain remaining jobs — counters were already zeroed by ResetStatus/Stop.
			for {
				select {
				case <-rc.retryCh:
					rc.pendingCount.Add(-1)
				default:
					return
				}
			}
		}
	}
}

// process handles a single retryJob: checks staleness/cancellation, waits, then
// re-submits to the pipeline.
func (rc *RetryCoordinator) process(job retryJob) {
	// Generation check (REQ-025, REQ-026).
	if rc.pipeline.generation.Load() != job.generation {
		rc.decPending()
		return
	}

	// Path stamp check (EDGE-010). If SubmitFile bumped the stamp after this job
	// was scheduled, the job is stale. SubmitFile already decremented the counters,
	// so we use dropStaleOnly (no counter change).
	rc.pathMu.Lock()
	currentStamp := rc.pathStamps[job.path]
	rc.pathMu.Unlock()
	if currentStamp != job.pathStamp {
		// Counters already decremented by SubmitFile — only decrement pendingCount.
		rc.pendingCount.Add(-1)
		return
	}

	// Wait based on error classification.
	cls := apperr.Classify(apperr.New(job.code, job.message))
	backoffIdx := job.attempts - 2 // attempt=2 → backoffs[0], attempt=3 → backoffs[1]
	if backoffIdx < 0 {
		backoffIdx = 0
	}

	switch cls {
	case apperr.ClassTransientRetry:
		backoff := rc.backoffs[len(rc.backoffs)-1] // cap at max
		if backoffIdx < len(rc.backoffs) {
			backoff = rc.backoffs[backoffIdx]
		}
		select {
		case <-time.After(backoff):
		case <-rc.ctx.Done():
			rc.decPending()
			return
		}

	case apperr.ClassTransientWait:
		if err := rc.limiter.WaitForUnpause(rc.ctx); err != nil {
			rc.decPending()
			return
		}
	}

	// Re-check generation and stamp after wake.
	if rc.pipeline.generation.Load() != job.generation {
		rc.decPending()
		return
	}
	rc.pathMu.Lock()
	currentStamp = rc.pathStamps[job.path]
	rc.pathMu.Unlock()
	if currentStamp != job.pathStamp {
		rc.pendingCount.Add(-1) // SubmitFile already decremented the pipeline counter
		return
	}

	// Clear path stamp now that this job is being dispatched.
	rc.pathMu.Lock()
	if rc.pathStamps[job.path] == job.pathStamp {
		delete(rc.pathStamps, job.path)
	}
	rc.pathMu.Unlock()

	// Decrement PendingRetryFiles *before* re-submit so the invariant
	// Indexed+Failed+Pending ≤ Total holds at every snapshot (REQ-031).
	rc.decPending()

	// Re-submit to pipeline as a single-file job with the accumulated attempts.
	rc.pipeline.pendingJobs.Add(1)
	select {
	case rc.pipeline.jobCh <- indexJob{
		typ:      jobSingleFile,
		filePath: job.path,
		attempts: job.attempts,
	}:
	case <-rc.ctx.Done():
		rc.pipeline.pendingJobs.Add(-1)
	}
}

// decPending decrements the coordinator's internal counter and the pipeline's
// PendingRetryFiles field.
func (rc *RetryCoordinator) decPending() {
	rc.pendingCount.Add(-1)
	rc.pipeline.mu.Lock()
	if rc.pipeline.status.PendingRetryFiles > 0 {
		rc.pipeline.status.PendingRetryFiles--
	}
	rc.pipeline.mu.Unlock()
}
