package app

import (
	"encoding/json"
	"log/slog"
	"testing"

	"findo/internal/indexer"
	"findo/internal/store"
	"findo/internal/vectorstore"
)

// newTestAppWithPipeline returns an App wired with an in-memory store and a real pipeline.
func newTestAppWithPipeline(t *testing.T) *App {
	t.Helper()
	s, err := store.NewStore(":memory:", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	idx := vectorstore.NewDefaultIndex(slog.Default())
	p := indexer.NewPipeline(s, idx, nil, t.TempDir(), slog.Default(), nil, indexer.DefaultPipelineConfig())
	t.Cleanup(func() { p.Stop() })
	return &App{store: s, pipeline: p, logger: slog.Default()}
}

// seedRegistry records n failures for the given code into the pipeline registry.
func seedRegistry(p *indexer.Pipeline, path, code, message string, attempts int) {
	p.Registry().Record(path, code, message, attempts)
}

// TestGetIndexStatus_IncludesFailureGroups — REQ-040
// Seed registry with 6 distinct error codes; FailedFileGroups must have length 5
// (top-5 by count) in descending order.
func TestGetIndexStatus_IncludesFailureGroups(t *testing.T) {
	a := newTestAppWithPipeline(t)
	reg := a.pipeline.Registry()

	// Seed: A=5 files, B=4, C=3, D=2, E=2, F=1
	for i := 0; i < 5; i++ {
		reg.Record("/a/file"+string(rune('0'+i))+".txt", "ERR_A", "error A", 1)
	}
	for i := 0; i < 4; i++ {
		reg.Record("/b/file"+string(rune('0'+i))+".txt", "ERR_B", "error B", 1)
	}
	for i := 0; i < 3; i++ {
		reg.Record("/c/file"+string(rune('0'+i))+".txt", "ERR_C", "error C", 1)
	}
	for i := 0; i < 2; i++ {
		reg.Record("/d/file"+string(rune('0'+i))+".txt", "ERR_D", "error D", 1)
	}
	for i := 0; i < 2; i++ {
		reg.Record("/e/file"+string(rune('0'+i))+".txt", "ERR_E", "error E", 1)
	}
	reg.Record("/f/file0.txt", "ERR_F", "error F", 1)

	status := a.GetIndexStatus()

	if len(status.FailedFileGroups) != 5 {
		t.Fatalf("expected 5 failure groups (top-5), got %d", len(status.FailedFileGroups))
	}

	// Verify descending order by count.
	for i := 1; i < len(status.FailedFileGroups); i++ {
		if status.FailedFileGroups[i].Count > status.FailedFileGroups[i-1].Count {
			t.Errorf("groups not in descending order: index %d (%d) > index %d (%d)",
				i, status.FailedFileGroups[i].Count,
				i-1, status.FailedFileGroups[i-1].Count)
		}
	}

	// Top group must have count 5.
	if status.FailedFileGroups[0].Count != 5 {
		t.Errorf("expected top group count=5, got %d", status.FailedFileGroups[0].Count)
	}
}

// TestGetIndexFailures_ReturnsSnapshot — REQ-041
// Seed the registry with 3 failures; GetIndexFailures returns 3 entries with correct fields.
func TestGetIndexFailures_ReturnsSnapshot(t *testing.T) {
	a := newTestAppWithPipeline(t)
	reg := a.pipeline.Registry()

	reg.Record("/foo/bar.txt", "ERR_A", "msg-a", 1)
	reg.Record("/foo/baz.txt", "ERR_B", "msg-b", 2)
	reg.Record("/foo/qux.txt", "ERR_C", "msg-c", 3)

	failures := a.GetIndexFailures()

	if len(failures) != 3 {
		t.Fatalf("expected 3 failures, got %d", len(failures))
	}

	// Build a lookup map for path → entry.
	byPath := make(map[string]IndexFailureDTO)
	for _, f := range failures {
		byPath[f.Path] = f
	}

	cases := []struct {
		path     string
		code     string
		message  string
		attempts int
	}{
		{"/foo/bar.txt", "ERR_A", "msg-a", 1},
		{"/foo/baz.txt", "ERR_B", "msg-b", 2},
		{"/foo/qux.txt", "ERR_C", "msg-c", 3},
	}
	for _, c := range cases {
		f, ok := byPath[c.path]
		if !ok {
			t.Errorf("missing entry for path %q", c.path)
			continue
		}
		if f.Code != c.code {
			t.Errorf("path %q: expected Code=%q, got %q", c.path, c.code, f.Code)
		}
		if f.Message != c.message {
			t.Errorf("path %q: expected Message=%q, got %q", c.path, c.message, f.Message)
		}
		if f.Attempts != c.attempts {
			t.Errorf("path %q: expected Attempts=%d, got %d", c.path, c.attempts, f.Attempts)
		}
		if f.LastFailedAt == 0 {
			t.Errorf("path %q: LastFailedAt is zero", c.path)
		}
	}
}

// TestFailureGroupDTO_JSONShape — REQ-042
// Marshal a FailureGroupDTO; field names must match the spec exactly.
func TestFailureGroupDTO_JSONShape(t *testing.T) {
	dto := FailureGroupDTO{
		Code:        "ERR_EMBED_FAILED",
		Label:       "Embedding failed",
		Count:       7,
		SampleFiles: []string{"/a.txt", "/b.txt"},
	}
	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	for _, key := range []string{"code", "label", "count", "sampleFiles"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q to be present", key)
		}
	}
	if raw["code"] != "ERR_EMBED_FAILED" {
		t.Errorf("expected code=ERR_EMBED_FAILED, got %v", raw["code"])
	}
	if raw["count"].(float64) != 7 {
		t.Errorf("expected count=7, got %v", raw["count"])
	}
}

// TestEmitStatusLoop_IncludesPendingAndGroups — REQ-043
// Marshal an IndexStatusDTO; verify JSON contains pendingRetryFiles and failedFileGroups keys.
func TestEmitStatusLoop_IncludesPendingAndGroups(t *testing.T) {
	dto := IndexStatusDTO{
		TotalFiles:        10,
		IndexedFiles:      5,
		FailedFiles:       2,
		PendingRetryFiles: 1,
		FailedFileGroups: []FailureGroupDTO{
			{Code: "ERR_A", Label: "A error", Count: 2, SampleFiles: []string{"/f.txt"}},
		},
	}
	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if _, ok := raw["pendingRetryFiles"]; !ok {
		t.Error("expected JSON key 'pendingRetryFiles' to be present in IndexStatusDTO")
	}
	if _, ok := raw["failedFileGroups"]; !ok {
		t.Error("expected JSON key 'failedFileGroups' to be present in IndexStatusDTO")
	}
}

// TestGetIndexFailures_NoIO — REQ-044
// GetIndexFailures must only read from the in-memory registry, not from store/index.
// We construct an App whose store is closed (any call would error) and pipeline
// has a registry with data — the method must succeed without touching the store.
func TestGetIndexFailures_NoIO(t *testing.T) {
	s, err := store.NewStore(":memory:", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	idx := vectorstore.NewDefaultIndex(slog.Default())
	p := indexer.NewPipeline(s, idx, nil, t.TempDir(), slog.Default(), nil, indexer.DefaultPipelineConfig())
	defer p.Stop()

	p.Registry().Record("/some/file.txt", "ERR_A", "oops", 1)

	// Close the store so any DB call from GetIndexFailures would error.
	s.Close()

	a := &App{store: s, pipeline: p, logger: slog.Default()}

	// Must not panic, must return 1 entry.
	failures := a.GetIndexFailures()
	if len(failures) != 1 {
		t.Fatalf("expected 1 failure from registry (no IO), got %d", len(failures))
	}
}

// TestGetIndexFailures_Empty — EDGE-008
// Empty registry → GetIndexFailures returns a non-nil empty slice that marshals as [].
func TestGetIndexFailures_Empty(t *testing.T) {
	a := newTestAppWithPipeline(t)

	failures := a.GetIndexFailures()
	if failures == nil {
		t.Fatal("expected non-nil slice for empty registry, got nil")
	}
	if len(failures) != 0 {
		t.Fatalf("expected 0 failures, got %d", len(failures))
	}

	data, err := json.Marshal(failures)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("expected '[]' JSON for empty slice, got %s", string(data))
	}
}

// TestGetIndexStatus_NoFailures — EDGE-009
// No failures → FailedFileGroups is non-nil empty slice, PendingRetryFiles is 0.
func TestGetIndexStatus_NoFailures(t *testing.T) {
	a := newTestAppWithPipeline(t)

	status := a.GetIndexStatus()

	if status.FailedFileGroups == nil {
		t.Fatal("expected non-nil FailedFileGroups when no failures")
	}
	if len(status.FailedFileGroups) != 0 {
		t.Errorf("expected 0 failure groups, got %d", len(status.FailedFileGroups))
	}
	if status.PendingRetryFiles != 0 {
		t.Errorf("expected PendingRetryFiles=0, got %d", status.PendingRetryFiles)
	}

	// Also verify JSON serializes as [] not null.
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	groups, ok := raw["failedFileGroups"]
	if !ok {
		t.Fatal("expected 'failedFileGroups' key in marshaled IndexStatusDTO")
	}
	if groups == nil {
		t.Error("expected 'failedFileGroups' to serialize as [] not null")
	}
}

// TestGetIndexFailures_NonUTF8Path — EDGE-020
// A failure with a non-UTF-8 path must not cause json.Marshal to error.
func TestGetIndexFailures_NonUTF8Path(t *testing.T) {
	a := newTestAppWithPipeline(t)
	// Record a path with an invalid UTF-8 byte sequence.
	a.pipeline.Registry().Record("/foo/\xff/bar.txt", "ERR_A", "bad path", 1)

	failures := a.GetIndexFailures()
	if len(failures) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(failures))
	}

	// json.Marshal must not return an error (Go replaces invalid UTF-8 with U+FFFD).
	_, err := json.Marshal(failures)
	if err != nil {
		t.Fatalf("json.Marshal failed for non-UTF-8 path: %v", err)
	}
}

// TestGetIndexStatus_NilPipeline — defensive check: nil pipeline returns safe zero value.
func TestGetIndexStatus_NilPipeline(t *testing.T) {
	a := &App{pipeline: nil, logger: slog.Default()}
	status := a.GetIndexStatus()
	if status.FailedFileGroups == nil {
		t.Error("expected non-nil FailedFileGroups even with nil pipeline")
	}
	if status.PendingRetryFiles != 0 {
		t.Errorf("expected PendingRetryFiles=0 for nil pipeline, got %d", status.PendingRetryFiles)
	}
}

// TestGetIndexFailures_NilPipeline — defensive check: nil pipeline returns empty slice.
func TestGetIndexFailures_NilPipeline(t *testing.T) {
	a := &App{pipeline: nil, logger: slog.Default()}
	failures := a.GetIndexFailures()
	if failures == nil {
		t.Fatal("expected non-nil slice for nil pipeline")
	}
	if len(failures) != 0 {
		t.Fatalf("expected 0 failures for nil pipeline, got %d", len(failures))
	}
}
