package embedder

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

// REF-042: GeminiConfig knobs thread through to the constructed embedder.
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

	// RateLimit: allow two Allow() calls but block the third within the window.
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
