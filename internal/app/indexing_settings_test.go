package app

import (
	"errors"
	"testing"

	"findo/internal/apperr"
)

func TestGetIndexingSettings_ReturnsDefaultsWhenUnset(t *testing.T) {
	a := newTestApp(t)
	got := a.GetIndexingSettings()
	if got.WorkersSaved != a.cfg.Indexing.Workers {
		t.Errorf("WorkersSaved = %d, want default %d", got.WorkersSaved, a.cfg.Indexing.Workers)
	}
	if got.WorkersRuntime != a.cfg.Indexing.Workers {
		t.Errorf("WorkersRuntime = %d, want %d", got.WorkersRuntime, a.cfg.Indexing.Workers)
	}
	if got.RateLimitSaved != a.cfg.Embedder.Gemini.RateLimitPerMinute {
		t.Errorf("RateLimitSaved = %d, want default %d", got.RateLimitSaved, a.cfg.Embedder.Gemini.RateLimitPerMinute)
	}
}

func TestGetIndexingSettings_ReturnsPersistedValues(t *testing.T) {
	a := newTestApp(t)
	if err := a.store.SetSetting("indexing.workers", "8"); err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetSetting("indexing.rate_limit_per_minute", "120"); err != nil {
		t.Fatal(err)
	}
	got := a.GetIndexingSettings()
	if got.WorkersSaved != 8 {
		t.Errorf("WorkersSaved = %d, want 8", got.WorkersSaved)
	}
	if got.RateLimitSaved != 120 {
		t.Errorf("RateLimitSaved = %d, want 120", got.RateLimitSaved)
	}
}

func TestSetIndexingSettings_PersistsValidValues(t *testing.T) {
	a := newTestApp(t)
	if err := a.SetIndexingSettings(6, 200); err != nil {
		t.Fatalf("SetIndexingSettings returned error: %v", err)
	}
	w, _ := a.store.GetSetting("indexing.workers", "")
	r, _ := a.store.GetSetting("indexing.rate_limit_per_minute", "")
	if w != "6" {
		t.Errorf("indexing.workers = %q, want %q", w, "6")
	}
	if r != "200" {
		t.Errorf("indexing.rate_limit_per_minute = %q, want %q", r, "200")
	}
}

func TestSetIndexingSettings_RejectsOutOfBounds(t *testing.T) {
	tests := []struct {
		name      string
		workers   int
		rateLimit int
	}{
		{"workers too low", 0, 60},
		{"workers too high", 33, 60},
		{"rate too low", 4, 0},
		{"rate too high", 4, 10001},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestApp(t)
			err := a.SetIndexingSettings(tt.workers, tt.rateLimit)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			var ae *apperr.Error
			if !errors.As(err, &ae) {
				t.Fatalf("expected apperr.Error, got %T: %v", err, err)
			}
			if ae.Code != apperr.ErrConfigInvalid.Code {
				t.Errorf("error code = %q, want %q", ae.Code, apperr.ErrConfigInvalid.Code)
			}
		})
	}
}

func TestApplyPersistedIndexingOverrides_OverridesCfgWhenPresent(t *testing.T) {
	a := newTestApp(t)
	if err := a.store.SetSetting("indexing.workers", "12"); err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetSetting("indexing.rate_limit_per_minute", "300"); err != nil {
		t.Fatal(err)
	}
	a.applyPersistedIndexingOverrides()
	if a.cfg.Indexing.Workers != 12 {
		t.Errorf("cfg.Indexing.Workers = %d, want 12", a.cfg.Indexing.Workers)
	}
	if a.cfg.Embedder.Gemini.RateLimitPerMinute != 300 {
		t.Errorf("cfg.Embedder.Gemini.RateLimitPerMinute = %d, want 300", a.cfg.Embedder.Gemini.RateLimitPerMinute)
	}
}

func TestApplyPersistedIndexingOverrides_IgnoresInvalidValues(t *testing.T) {
	a := newTestApp(t)
	origWorkers := a.cfg.Indexing.Workers
	origRate := a.cfg.Embedder.Gemini.RateLimitPerMinute
	if err := a.store.SetSetting("indexing.workers", "999"); err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetSetting("indexing.rate_limit_per_minute", "not-a-number"); err != nil {
		t.Fatal(err)
	}
	a.applyPersistedIndexingOverrides()
	if a.cfg.Indexing.Workers != origWorkers {
		t.Errorf("cfg.Indexing.Workers = %d, want unchanged %d", a.cfg.Indexing.Workers, origWorkers)
	}
	if a.cfg.Embedder.Gemini.RateLimitPerMinute != origRate {
		t.Errorf("cfg.Embedder.Gemini.RateLimitPerMinute = %d, want unchanged %d", a.cfg.Embedder.Gemini.RateLimitPerMinute, origRate)
	}
}

func TestSetIndexingSettings_NoPartialPersistOnInvalid(t *testing.T) {
	a := newTestApp(t)
	// Seed valid values.
	if err := a.SetIndexingSettings(4, 60); err != nil {
		t.Fatal(err)
	}
	// Attempt with invalid rate limit; workers value (5) must NOT be persisted.
	_ = a.SetIndexingSettings(5, 99999)
	w, _ := a.store.GetSetting("indexing.workers", "")
	if w != "4" {
		t.Errorf("indexing.workers = %q, want %q (must not be overwritten on invalid rate)", w, "4")
	}
}
