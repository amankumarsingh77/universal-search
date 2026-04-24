package app

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"findo/internal/apperr"
	"findo/internal/config"
	"findo/internal/embedder"
	"findo/internal/store"
)

// errorEmbedder wraps the fake embedder but returns a configurable error from EmbedQuery.
type errorEmbedder struct {
	queryErr    error
	pausedUntil time.Time
}

func (e *errorEmbedder) ModelID() string        { return "fake" }
func (e *errorEmbedder) Dimensions() int        { return 768 }
func (e *errorEmbedder) PausedUntil() time.Time { return e.pausedUntil }
func (e *errorEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return nil, e.queryErr
}
func (e *errorEmbedder) EmbedBatch(_ context.Context, _ []embedder.ChunkInput) ([][]float32, error) {
	return nil, nil
}

// newSearchFiltersTestApp builds a minimal App for SearchWithFilters tests.
func newSearchFiltersTestApp(t *testing.T) *App {
	t.Helper()
	s, err := store.NewStore(":memory:", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &App{
		cfg:    config.DefaultConfig(),
		ctx:    context.Background(),
		logger: slog.Default(),
		store:  s,
	}
}

// TestSearchWithFilters_EmbedRateLimited_PopulatesErrorCode — REQ-009, EDGE-005
// When EmbedQuery returns an ErrRateLimited error, SearchWithFilters must return
// ErrorCode == ERR_RATE_LIMITED, empty Results, and must NOT fall back to
// filename search (asserted by inserting a file that would match by name).
func TestSearchWithFilters_EmbedRateLimited_PopulatesErrorCode(t *testing.T) {
	a := newSearchFiltersTestApp(t)

	// Insert a file that would match "budget" by filename — if searchFilenameOnly
	// were called, it would appear in Results.
	_, err := a.store.UpsertFile(store.FileRecord{
		Path:       "/home/user/budget.pdf",
		FileType:   "document",
		Extension:  ".pdf",
		SizeBytes:  1024,
		ModifiedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	rateLimitErr := apperr.Wrap(apperr.ErrRateLimited.Code, "rate limited by provider", nil)
	a.embedder = &errorEmbedder{queryErr: rateLimitErr}

	result, err := a.SearchWithFilters("budget", "budget", nil)
	if err != nil {
		t.Fatalf("SearchWithFilters returned unexpected error: %v", err)
	}
	if result.ErrorCode != apperr.ErrRateLimited.Code {
		t.Errorf("expected ErrorCode=%q, got %q", apperr.ErrRateLimited.Code, result.ErrorCode)
	}
	if len(result.Results) != 0 {
		t.Errorf("expected empty Results on rate-limit, got %d results", len(result.Results))
	}
}

// TestSearchWithFilters_EmbedGenericError_PopulatesErrorCode — REQ-010, EDGE-006
// When EmbedQuery returns a generic (non-rate-limit) error, SearchWithFilters
// must return ErrorCode == ERR_EMBED_FAILED and empty Results (no filename fallback).
func TestSearchWithFilters_EmbedGenericError_PopulatesErrorCode(t *testing.T) {
	a := newSearchFiltersTestApp(t)

	// Insert a file that would match "crash" by filename — if searchFilenameOnly
	// were called, it would appear in Results.
	_, err := a.store.UpsertFile(store.FileRecord{
		Path:       "/home/user/crash_report.txt",
		FileType:   "document",
		Extension:  ".txt",
		SizeBytes:  512,
		ModifiedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	a.embedder = &errorEmbedder{queryErr: fmt.Errorf("boom")}

	result, err := a.SearchWithFilters("crash report", "crash report", nil)
	if err != nil {
		t.Fatalf("SearchWithFilters returned unexpected error: %v", err)
	}
	if result.ErrorCode != apperr.ErrEmbedFailed.Code {
		t.Errorf("expected ErrorCode=%q, got %q", apperr.ErrEmbedFailed.Code, result.ErrorCode)
	}
	if len(result.Results) != 0 {
		t.Errorf("expected empty Results on generic embed error, got %d results", len(result.Results))
	}
}

// TestSearchWithFilters_EmbedRateLimited_PopulatesRetryAfterMs — REQ-021, EDGE-005
// When EmbedQuery returns ErrRateLimited and the embedder has a non-zero PausedUntil,
// SearchWithFilters must populate RetryAfterMs with the remaining milliseconds.
func TestSearchWithFilters_EmbedRateLimited_PopulatesRetryAfterMs(t *testing.T) {
	a := newSearchFiltersTestApp(t)

	rateLimitErr := apperr.Wrap(apperr.ErrRateLimited.Code, "rate limited by provider", nil)
	pauseUntil := time.Now().Add(5 * time.Second)
	a.embedder = &errorEmbedder{queryErr: rateLimitErr, pausedUntil: pauseUntil}

	result, err := a.SearchWithFilters("query", "query", nil)
	if err != nil {
		t.Fatalf("SearchWithFilters returned unexpected error: %v", err)
	}
	if result.ErrorCode != apperr.ErrRateLimited.Code {
		t.Errorf("expected ErrorCode=%q, got %q", apperr.ErrRateLimited.Code, result.ErrorCode)
	}
	// RetryAfterMs should be between 4000 and 5000 (allowing for small time drift).
	if result.RetryAfterMs < 4000 || result.RetryAfterMs > 5000 {
		t.Errorf("expected RetryAfterMs in [4000,5000], got %d", result.RetryAfterMs)
	}
}

// TestSearchWithFilters_EmbedRateLimited_ZeroPausedUntil_RetryAfterMsIsZero — EDGE-006
// When EmbedQuery returns ErrRateLimited but PausedUntil is zero, RetryAfterMs must be 0.
func TestSearchWithFilters_EmbedRateLimited_ZeroPausedUntil_RetryAfterMsIsZero(t *testing.T) {
	a := newSearchFiltersTestApp(t)

	rateLimitErr := apperr.Wrap(apperr.ErrRateLimited.Code, "rate limited by provider", nil)
	a.embedder = &errorEmbedder{queryErr: rateLimitErr} // pausedUntil zero

	result, err := a.SearchWithFilters("query", "query", nil)
	if err != nil {
		t.Fatalf("SearchWithFilters returned unexpected error: %v", err)
	}
	if result.ErrorCode != apperr.ErrRateLimited.Code {
		t.Errorf("expected ErrorCode=%q, got %q", apperr.ErrRateLimited.Code, result.ErrorCode)
	}
	if result.RetryAfterMs != 0 {
		t.Errorf("expected RetryAfterMs=0 when PausedUntil is zero, got %d", result.RetryAfterMs)
	}
}

// TestSearchWithFilters_OfflineUnchanged — REQ-012
// When embedder is nil (offline), SearchWithFilters must return empty ErrorCode
// and use filename search (existing behaviour unchanged).
func TestSearchWithFilters_OfflineUnchanged(t *testing.T) {
	a := newSearchFiltersTestApp(t)
	// embedder is nil → offline mode.

	// Insert a file so filename search can find it.
	_, err := a.store.UpsertFile(store.FileRecord{
		Path:       "/home/user/notes.txt",
		FileType:   "document",
		Extension:  ".txt",
		SizeBytes:  256,
		ModifiedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.SearchWithFilters("notes", "", nil)
	if err != nil {
		t.Fatalf("SearchWithFilters returned unexpected error in offline mode: %v", err)
	}
	if result.ErrorCode != "" {
		t.Errorf("expected empty ErrorCode in offline mode, got %q", result.ErrorCode)
	}
	// Filename search should have run and found the file.
	found := false
	for _, r := range result.Results {
		if r.FileName == "notes.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected filename-fallback to find notes.txt, got results: %+v", result.Results)
	}
}
