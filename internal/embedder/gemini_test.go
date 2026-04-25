package embedder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"findo/internal/apperr"

	"google.golang.org/genai"
)

// newTestEmbedder builds a *GeminiEmbedder wired to an injected doer that
// drives the real (*GeminiEmbedder).EmbedBatch implementation without touching
// the network. fn receives the number of inputs in the current batch and
// returns the vectors (or an error) for that batch.
func newTestEmbedder(fn func(batchSize int) ([][]float32, error)) *GeminiEmbedder {
	doer := func(_ context.Context, _ string, contents []*genai.Content, _ *genai.EmbedContentConfig) ([][]float32, error) {
		return fn(len(contents))
	}
	e := newWithFunc(doer, 3, slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.limiter = NewRateLimiter(1000, time.Minute)
	return e
}

func makeVecs(n int, base float32) [][]float32 {
	vecs := make([][]float32, n)
	for i := range vecs {
		vecs[i] = []float32{base + float32(i)}
	}
	return vecs
}

func makeChunks(n int) []ChunkInput {
	chunks := make([]ChunkInput, n)
	for i := range chunks {
		chunks[i] = ChunkInput{Title: "t", Text: "text"}
	}
	return chunks
}

// ---------------------------------------------------------------------------
// Existing RateLimiter tests
// ---------------------------------------------------------------------------

func TestRateLimiter_AllowsWithinLimit(t *testing.T) {
	rl := NewRateLimiter(5, time.Second)
	for i := 0; i < 5; i++ {
		if !rl.Allow() {
			t.Fatalf("request %d should have been allowed", i)
		}
	}
	if rl.Allow() {
		t.Fatal("6th request should have been denied")
	}
}

func TestRateLimiter_AllowsAfterWindowExpires(t *testing.T) {
	rl := NewRateLimiter(2, 100*time.Millisecond)
	rl.Allow()
	rl.Allow()
	if rl.Allow() {
		t.Fatal("3rd request should be denied")
	}
	time.Sleep(150 * time.Millisecond)
	if !rl.Allow() {
		t.Fatal("request after window should be allowed")
	}
}

// ---------------------------------------------------------------------------
// RateLimiter global pause tests
// ---------------------------------------------------------------------------

func TestRateLimiter_PauseUntil_BlocksWait(t *testing.T) {
	rl := NewRateLimiter(100, time.Minute) // generous limit so Allow() won't block

	pause := time.Now().Add(150 * time.Millisecond)
	rl.PauseUntil(pause)

	ctx := context.Background()
	start := time.Now()
	if err := rl.Wait(ctx); err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 100*time.Millisecond {
		t.Fatalf("Wait returned too early: elapsed=%v, expected >=100ms", elapsed)
	}
}

func TestRateLimiter_PauseUntil_LaterTimeWins(t *testing.T) {
	rl := NewRateLimiter(100, time.Minute)

	early := time.Now().Add(50 * time.Millisecond)
	late := time.Now().Add(200 * time.Millisecond)

	rl.PauseUntil(late)
	rl.PauseUntil(early) // should be ignored; late is later

	got := rl.PausedUntil()
	if !got.Equal(late) {
		t.Fatalf("PausedUntil = %v, want %v", got, late)
	}
}

func TestRateLimiter_WaitIfPaused_ReturnsImmediatelyWhenNotPaused(t *testing.T) {
	rl := NewRateLimiter(100, time.Minute)

	ctx := context.Background()
	start := time.Now()
	if err := rl.WaitIfPaused(ctx); err != nil {
		t.Fatalf("WaitIfPaused returned error: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Fatalf("WaitIfPaused took too long when not paused: %v", elapsed)
	}
}

func TestRateLimiter_Wait_ContextCancellation(t *testing.T) {
	rl := NewRateLimiter(100, time.Minute)

	// Pause for 10 seconds — context will cancel before it expires
	rl.PauseUntil(time.Now().Add(10 * time.Second))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := rl.Wait(ctx)
	if err == nil {
		t.Fatal("Wait should have returned an error when context is cancelled")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// parseRetryAfter tests
// ---------------------------------------------------------------------------

func TestParseRetryAfter_WithRetryDelay(t *testing.T) {
	err := errors.New(`rpc error: code = ResourceExhausted desc = retry_delay:{seconds:30}`)
	d := parseRetryAfter(err)
	if d != 30*time.Second {
		t.Fatalf("expected 30s, got %v", d)
	}
}

func TestParseRetryAfter_NoRetryDelay(t *testing.T) {
	err := errors.New("some other error without retry info")
	d := parseRetryAfter(err)
	if d != 0 {
		t.Fatalf("expected 0, got %v", d)
	}
}

// ---------------------------------------------------------------------------
// EmbedBatch tests — exercise the real (*Embedder).EmbedBatch path via an
// injected embedFn, so any regression in the production batching loop is
// caught by these assertions.
// ---------------------------------------------------------------------------

func TestEmbedBatch_EmptyInput(t *testing.T) {
	e := newTestEmbedder(func(batchSize int) ([][]float32, error) {
		t.Fatal("embed function should not be called for empty input")
		return nil, nil
	})

	result, err := e.EmbedBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result, got %v", result)
	}
}

func TestEmbedBatch_SingleChunk(t *testing.T) {
	calls := 0
	e := newTestEmbedder(func(batchSize int) ([][]float32, error) {
		calls++
		return makeVecs(batchSize, 1.0), nil
	})

	result, err := e.EmbedBatch(context.Background(), makeChunks(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
}

func TestEmbedBatch_ExactlyMaxBatchSize(t *testing.T) {
	calls := 0
	e := newTestEmbedder(func(batchSize int) ([][]float32, error) {
		calls++
		return makeVecs(batchSize, 0), nil
	})

	result, err := e.EmbedBatch(context.Background(), makeChunks(DefaultGeminiConfig().BatchSize))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call for exactly %d chunks, got %d", DefaultGeminiConfig().BatchSize, calls)
	}
	if len(result) != DefaultGeminiConfig().BatchSize {
		t.Fatalf("expected %d results, got %d", DefaultGeminiConfig().BatchSize, len(result))
	}
}

func TestEmbedBatch_SplitsAtBoundary(t *testing.T) {
	calls := 0
	e := newTestEmbedder(func(batchSize int) ([][]float32, error) {
		calls++
		return makeVecs(batchSize, 0), nil
	})

	result, err := e.EmbedBatch(context.Background(), makeChunks(150))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls for 150 chunks, got %d", calls)
	}
	if len(result) != 150 {
		t.Fatalf("expected 150 results, got %d", len(result))
	}
}

func TestEmbedBatch_PreservesOrder(t *testing.T) {
	var globalIdx int
	e := newTestEmbedder(func(batchSize int) ([][]float32, error) {
		vecs := make([][]float32, batchSize)
		for i := range vecs {
			vecs[i] = []float32{float32(globalIdx)}
			globalIdx++
		}
		return vecs, nil
	})

	result, err := e.EmbedBatch(context.Background(), makeChunks(150))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, vec := range result {
		if vec[0] != float32(i) {
			t.Fatalf("result[%d] has value %v, want %v", i, vec[0], float32(i))
		}
	}
}

// TestEmbedBatch_CardinalityMismatch ensures EmbedBatch fails fast if the
// underlying embed call returns fewer vectors than inputs, so partial results
// never leak to callers that might commit them.
func TestEmbedBatch_CardinalityMismatch(t *testing.T) {
	e := newTestEmbedder(func(batchSize int) ([][]float32, error) {
		return makeVecs(batchSize-1, 0), nil // one short on purpose
	})

	_, err := e.EmbedBatch(context.Background(), makeChunks(10))
	if err == nil {
		t.Fatal("expected cardinality mismatch error, got nil")
	}
}

// TestEmbedBatch_PropagatesError ensures a transport error bubbles up without
// retry masking (retries are exercised separately by embed() unit tests).
func TestEmbedBatch_PropagatesError(t *testing.T) {
	boom := errors.New("boom")
	e := newTestEmbedder(func(batchSize int) ([][]float32, error) {
		return nil, boom
	})

	_, err := e.EmbedBatch(context.Background(), makeChunks(5))
	if !errors.Is(err, boom) {
		t.Fatalf("expected errors.Is(err, boom), got %v", err)
	}
}

// ---------------------------------------------------------------------------
// GeminiConfig plumbing — verifies config knobs reach the constructed embedder
// (REF-042 from the maintainability refactor).
// ---------------------------------------------------------------------------

func TestGeminiConfig_AppliedToEmbedder(t *testing.T) {
	cfg := GeminiConfig{
		APIKey:                "test",
		Model:                 "custom-model",
		Dimensions:            512,
		RateLimitPerMinute:    2,
		BatchSize:             7,
		RetryMaxAttempts:      1,
		RetryInitialBackoffMs: 100,
		RetryMaxBackoffMs:     200,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	e, err := NewGeminiEmbedderFromConfig(cfg, logger)
	if err != nil {
		t.Fatalf("NewGeminiEmbedderFromConfig: %v", err)
	}

	if e.ModelID() != "custom-model" {
		t.Errorf("Model: got %q want custom-model", e.ModelID())
	}
	if e.Dimensions() != 512 {
		t.Errorf("Dimensions: got %d want 512", e.Dimensions())
	}
	if e.maxBatchSize != 7 {
		t.Errorf("maxBatchSize: got %d want 7", e.maxBatchSize)
	}
	if e.maxRetries != 1 {
		t.Errorf("maxRetries: got %d want 1", e.maxRetries)
	}
	if e.initialDelay != 100*time.Millisecond {
		t.Errorf("initialDelay: got %v want 100ms", e.initialDelay)
	}
	if e.maxDelay != 200*time.Millisecond {
		t.Errorf("maxDelay: got %v want 200ms", e.maxDelay)
	}

	if !e.Limiter().Allow() {
		t.Fatal("Allow() #1 should succeed (budget=2)")
	}
	if !e.Limiter().Allow() {
		t.Fatal("Allow() #2 should succeed (budget=2)")
	}
	if e.Limiter().Allow() {
		t.Fatal("Allow() #3 should be rejected — rate limit is 2/minute")
	}
}

// ---------------------------------------------------------------------------
// REQ-015 / EDGE-005: 429 terminal errors must satisfy errors.Is(apperr.ErrRateLimited)
// ---------------------------------------------------------------------------

// newFastEmbedder creates a test embedder with near-zero delays so tests run
// without real backoff waits.
func newFastEmbedder(doer embedFunc) *GeminiEmbedder {
	e := newWithFunc(doer, 3, slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.limiter = NewRateLimiter(1000, time.Minute)
	e.initialDelay = 1 * time.Millisecond
	e.maxDelay = 2 * time.Millisecond
	return e
}

// TestEmbedQueryWrapsRateLimitedOn429 verifies that a 429 error with maxRetries=0
// (single attempt) is returned wrapped as apperr.ErrRateLimited.
func TestEmbedQueryWrapsRateLimitedOn429(t *testing.T) {
	rateLimitErr := fmt.Errorf("googleapi: Error 429: rate limit exceeded")
	doer := func(_ context.Context, _ string, _ []*genai.Content, _ *genai.EmbedContentConfig) ([][]float32, error) {
		return nil, rateLimitErr
	}

	e := newFastEmbedder(doer)
	e.maxRetries = 0

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := e.EmbedQuery(ctx, "x")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !errors.Is(err, apperr.ErrRateLimited) {
		t.Errorf("expected errors.Is(err, apperr.ErrRateLimited) == true, got false; err = %v", err)
	}
	if errors.Is(err, apperr.ErrEmbedFailed) {
		t.Errorf("expected errors.Is(err, apperr.ErrEmbedFailed) == false, but it was true")
	}
	// Original cause must be preserved via Unwrap.
	if !errors.Is(err, rateLimitErr) {
		t.Errorf("expected original cause to be preserved via Unwrap, but errors.Is(err, rateLimitErr) is false")
	}
}

// TestEmbedQueryGenericErrorNotRateLimited verifies that a non-429 error is NOT
// wrapped as apperr.ErrRateLimited, and still propagates the original cause.
func TestEmbedQueryGenericErrorNotRateLimited(t *testing.T) {
	cause := fmt.Errorf("dial tcp: connection refused")
	doer := func(_ context.Context, _ string, _ []*genai.Content, _ *genai.EmbedContentConfig) ([][]float32, error) {
		return nil, cause
	}

	e := newFastEmbedder(doer)
	e.maxRetries = 0

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := e.EmbedQuery(ctx, "x")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if errors.Is(err, apperr.ErrRateLimited) {
		t.Errorf("expected errors.Is(err, apperr.ErrRateLimited) == false, got true")
	}
	if !errors.Is(err, cause) {
		t.Errorf("expected original cause to be preserved, but errors.Is(err, cause) is false")
	}
}

// TestEmbedQueryRateLimitedAfterRetriesExhausted verifies that the post-loop
// exhaustion path wraps the last 429 error with apperr.ErrRateLimited.
func TestEmbedQueryRateLimitedAfterRetriesExhausted(t *testing.T) {
	rateLimitErr := fmt.Errorf("googleapi: Error 429: quota exceeded for project")
	doer := func(_ context.Context, _ string, _ []*genai.Content, _ *genai.EmbedContentConfig) ([][]float32, error) {
		return nil, rateLimitErr
	}

	e := newFastEmbedder(doer)
	e.maxRetries = 1 // one retry, then exhausted

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := e.EmbedQuery(ctx, "x")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !errors.Is(err, apperr.ErrRateLimited) {
		t.Errorf("expected errors.Is(err, apperr.ErrRateLimited) == true, got false; err = %v", err)
	}
	if !errors.Is(err, rateLimitErr) {
		t.Errorf("expected original cause to be preserved, but errors.Is(err, rateLimitErr) is false")
	}
}

func newWithFunc(doer embedFunc, dims int32, logger *slog.Logger) *GeminiEmbedder {
	def := DefaultGeminiConfig()
	return &GeminiEmbedder{
		doer:         doer,
		model:        def.Model,
		dims:         dims,
		maxBatchSize: def.BatchSize,
		maxRetries:   def.RetryMaxAttempts,
		initialDelay: time.Duration(def.RetryInitialBackoffMs) * time.Millisecond,
		maxDelay:     time.Duration(def.RetryMaxBackoffMs) * time.Millisecond,
		limiter:      NewRateLimiter(def.RateLimitPerMinute, defaultRateWindow),
		logger:       logger,
	}
}
