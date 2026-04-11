package main

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"universal-search/internal/indexer"
	"universal-search/internal/store"
	"universal-search/internal/vectorstore"
)

func newTestAppFull(t *testing.T) *App {
	t.Helper()
	s, err := store.NewStore(":memory:", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	idx := vectorstore.NewIndex(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	p := indexer.NewPipeline(s, idx, nil, t.TempDir(), logger, nil)
	t.Cleanup(func() { p.Stop() })

	return &App{
		ctx:      context.Background(),
		logger:   logger,
		store:    s,
		pipeline: p,
	}
}

// TestSetGeminiAPIKey_EmptyKey verifies that an empty key is rejected immediately
// without making any network calls or modifying state.
func TestSetGeminiAPIKey_EmptyKey(t *testing.T) {
	a := newTestAppFull(t)

	err := a.SetGeminiAPIKey("")
	if err == nil {
		t.Fatal("expected error for empty key, got nil")
	}

	// Whitespace-only key should also be rejected.
	err = a.SetGeminiAPIKey("   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only key, got nil")
	}
}

// TestSetGeminiAPIKey_PipelinePauseRestore verifies that if validation fails,
// the pipeline is restored to its original pause state.
func TestSetGeminiAPIKey_PipelinePauseRestore(t *testing.T) {
	t.Run("pipeline_was_running_restored_to_running", func(t *testing.T) {
		a := newTestAppFull(t)

		// Confirm pipeline starts unpaused.
		if a.pipeline.Status().Paused {
			t.Fatal("expected pipeline to start unpaused")
		}

		// Use a clearly invalid key — embed call will fail, but that tests the
		// pause/restore path since we pause before the network call.
		_ = a.SetGeminiAPIKey("invalid-key-that-will-fail-validation")

		// Pipeline must be back to running (unpaused) even though validation failed.
		if a.pipeline.Status().Paused {
			t.Error("expected pipeline to be unpaused after failed SetGeminiAPIKey")
		}
	})

	t.Run("pipeline_was_paused_stays_paused", func(t *testing.T) {
		a := newTestAppFull(t)

		// Pause the pipeline first.
		a.pipeline.Pause()
		if !a.pipeline.Status().Paused {
			t.Fatal("expected pipeline to be paused")
		}

		// Validation will fail, but pipeline should remain paused.
		_ = a.SetGeminiAPIKey("invalid-key-that-will-fail-validation")

		if !a.pipeline.Status().Paused {
			t.Error("expected pipeline to remain paused after failed SetGeminiAPIKey")
		}
	})
}

// TestGetHasGeminiKey_NoEmbedder verifies false is returned when no embedder is set.
func TestGetHasGeminiKey_NoEmbedder(t *testing.T) {
	a := newTestAppFull(t)
	// newTestAppFull sets embedder to nil.
	if a.GetHasGeminiKey() {
		t.Error("expected GetHasGeminiKey to return false when embedder is nil")
	}
}
