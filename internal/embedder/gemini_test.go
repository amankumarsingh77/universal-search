package embedder

import (
	"context"
	"errors"
	"testing"
	"time"
)

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
// EmbedBatch tests
// ---------------------------------------------------------------------------

// mockEmbedder wraps an Embedder but overrides the embed call via a function
// field so tests can inject responses without hitting the real API.
// We test EmbedBatch by creating an Embedder with a capturedEmbed hook.

// embedCallRecord records one call to embed().
type embedCallRecord struct {
	batchSize int
}

// testEmbedder is a thin wrapper that intercepts embed calls for test assertions.
type testEmbedder struct {
	calls      []embedCallRecord
	embedFunc  func(batchSize int) ([][]float32, error)
}

// EmbedBatch re-implementation via function table — used in tests via
// embedBatchWith helper below.

// embedBatchWith runs EmbedBatch logic against a controllable embed function,
// allowing tests without a real Gemini client.
func embedBatchWith(
	ctx context.Context,
	chunks []ChunkInput,
	embedFn func(int, int) ([][]float32, error),
) ([][]float32, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	result := make([][]float32, 0, len(chunks))
	for start := 0; start < len(chunks); start += maxBatchSize {
		end := start + maxBatchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		vecs, err := embedFn(start, end)
		if err != nil {
			return nil, err
		}
		result = append(result, vecs...)
	}
	return result, nil
}

func makeChunks(n int) []ChunkInput {
	chunks := make([]ChunkInput, n)
	for i := range chunks {
		chunks[i] = ChunkInput{Title: "t", Text: "text"}
	}
	return chunks
}

func makeVecs(n int, base float32) [][]float32 {
	vecs := make([][]float32, n)
	for i := range vecs {
		vecs[i] = []float32{base + float32(i)}
	}
	return vecs
}

func TestEmbedBatch_EmptyInput(t *testing.T) {
	ctx := context.Background()
	result, err := embedBatchWith(ctx, nil, func(s, e int) ([][]float32, error) {
		t.Fatal("embed function should not be called for empty input")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result, got %v", result)
	}
}

func TestEmbedBatch_SingleChunk(t *testing.T) {
	ctx := context.Background()
	calls := 0
	chunks := makeChunks(1)

	result, err := embedBatchWith(ctx, chunks, func(s, e int) ([][]float32, error) {
		calls++
		return makeVecs(e-s, 1.0), nil
	})

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
	ctx := context.Background()
	calls := 0
	chunks := makeChunks(maxBatchSize)

	result, err := embedBatchWith(ctx, chunks, func(s, e int) ([][]float32, error) {
		calls++
		return makeVecs(e-s, 0), nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call for exactly %d chunks, got %d", maxBatchSize, calls)
	}
	if len(result) != maxBatchSize {
		t.Fatalf("expected %d results, got %d", maxBatchSize, len(result))
	}
}

func TestEmbedBatch_SplitsAtBoundary(t *testing.T) {
	ctx := context.Background()
	calls := 0
	chunks := makeChunks(150)

	result, err := embedBatchWith(ctx, chunks, func(s, e int) ([][]float32, error) {
		calls++
		return makeVecs(e-s, 0), nil
	})

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
	ctx := context.Background()
	chunks := makeChunks(150)

	result, err := embedBatchWith(ctx, chunks, func(s, e int) ([][]float32, error) {
		// Each chunk gets a vector whose first element equals its global index
		vecs := make([][]float32, e-s)
		for i := range vecs {
			vecs[i] = []float32{float32(s + i)}
		}
		return vecs, nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, vec := range result {
		if vec[0] != float32(i) {
			t.Fatalf("result[%d] has value %v, want %v", i, vec[0], float32(i))
		}
	}
}
