package indexer

import (
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"
)

// REQ-011: Record then Snapshot; entry fields match.
func TestFailureRegistry_REQ011_Shape(t *testing.T) {
	r := NewFailureRegistry(10)
	before := time.Now()
	r.Record("/tmp/foo.pdf", "ERR_EXTRACTION_FAILED", "text extraction from file failed", 1)
	after := time.Now()

	snap := r.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}
	e := snap[0]
	if e.Path != "/tmp/foo.pdf" {
		t.Errorf("Path: got %q want %q", e.Path, "/tmp/foo.pdf")
	}
	if e.Code != "ERR_EXTRACTION_FAILED" {
		t.Errorf("Code: got %q want %q", e.Code, "ERR_EXTRACTION_FAILED")
	}
	if e.Message != "text extraction from file failed" {
		t.Errorf("Message: got %q", e.Message)
	}
	if e.Attempts != 1 {
		t.Errorf("Attempts: got %d want 1", e.Attempts)
	}
	if e.LastFailedAt.Before(before) || e.LastFailedAt.After(after) {
		t.Errorf("LastFailedAt %v not in [%v, %v]", e.LastFailedAt, before, after)
	}
}

// REQ-012: Record same path twice; Len == 1; attempts reflects the second call.
func TestFailureRegistry_REQ012_Dedup(t *testing.T) {
	r := NewFailureRegistry(10)
	r.Record("/tmp/foo.pdf", "ERR_EXTRACTION_FAILED", "text extraction from file failed", 1)
	r.Record("/tmp/foo.pdf", "ERR_EXTRACTION_FAILED", "text extraction from file failed", 2)

	if r.Len() != 1 {
		t.Fatalf("expected Len() == 1, got %d", r.Len())
	}
	snap := r.Snapshot()
	if snap[0].Attempts != 2 {
		t.Errorf("Attempts: got %d want 2", snap[0].Attempts)
	}
}

// REQ-013: Record 5, Reset, Snapshot empty, DroppedCount 0.
func TestFailureRegistry_REQ013_Reset(t *testing.T) {
	r := NewFailureRegistry(10)
	for i := 0; i < 5; i++ {
		r.Record(fmt.Sprintf("/tmp/file%d.pdf", i), "ERR_EXTRACTION_FAILED", "msg", 1)
	}
	r.Reset()

	if r.Len() != 0 {
		t.Errorf("Len() after Reset: got %d want 0", r.Len())
	}
	if r.DroppedCount() != 0 {
		t.Errorf("DroppedCount() after Reset: got %d want 0", r.DroppedCount())
	}
	snap := r.Snapshot()
	if len(snap) != 0 {
		t.Errorf("Snapshot() after Reset: got %d entries want 0", len(snap))
	}
}

// REQ-014: cap=10, record 10 distinct paths, Len == 10, all retrievable.
func TestFailureRegistry_REQ014_AtCap(t *testing.T) {
	const cap = 10
	r := NewFailureRegistry(cap)
	paths := make(map[string]bool)
	for i := 0; i < cap; i++ {
		p := fmt.Sprintf("/tmp/file%d.pdf", i)
		paths[p] = true
		r.Record(p, "ERR_EXTRACTION_FAILED", "msg", 1)
	}

	if r.Len() != cap {
		t.Fatalf("Len(): got %d want %d", r.Len(), cap)
	}
	if r.DroppedCount() != 0 {
		t.Errorf("DroppedCount(): got %d want 0", r.DroppedCount())
	}
	snap := r.Snapshot()
	for _, e := range snap {
		if !paths[e.Path] {
			t.Errorf("unexpected path %q in snapshot", e.Path)
		}
	}
}

// REQ-015: cap=10, record 11 distinct, Len == 10, DroppedCount == 1, first path absent.
func TestFailureRegistry_REQ015_Eviction(t *testing.T) {
	const cap = 10
	r := NewFailureRegistry(cap)
	first := "/tmp/first.pdf"
	r.Record(first, "ERR_EXTRACTION_FAILED", "msg", 1)
	for i := 1; i <= cap; i++ {
		r.Record(fmt.Sprintf("/tmp/file%d.pdf", i), "ERR_EXTRACTION_FAILED", "msg", 1)
	}

	if r.Len() != cap {
		t.Errorf("Len(): got %d want %d", r.Len(), cap)
	}
	if r.DroppedCount() != 1 {
		t.Errorf("DroppedCount(): got %d want 1", r.DroppedCount())
	}
	snap := r.Snapshot()
	for _, e := range snap {
		if e.Path == first {
			t.Errorf("first path %q should have been evicted", first)
		}
	}
}

// REQ-016 / EDGE-006: 100 goroutines × 10 distinct paths each, -race, Len == 1000.
func TestFailureRegistry_REQ016_Race(t *testing.T) {
	// 100 goroutines × 10 paths → 1000 distinct paths
	const goroutines = 100
	const pathsPerGoroutine = 10
	const total = goroutines * pathsPerGoroutine

	r := NewFailureRegistry(total)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			for p := 0; p < pathsPerGoroutine; p++ {
				path := fmt.Sprintf("/tmp/g%d/file%d.pdf", g, p)
				r.Record(path, "ERR_EXTRACTION_FAILED", "msg", 1)
			}
		}()
	}
	wg.Wait()

	if r.Len() != total {
		t.Errorf("Len(): got %d want %d", r.Len(), total)
	}
}

// REQ-017: record 5 of code A and 2 of code B, Groups() returns two groups with correct
// counts, sample files ≤ 3 each, sorted by count desc (A before B).
func TestFailureRegistry_REQ017_Groups(t *testing.T) {
	r := NewFailureRegistry(100)
	for i := 0; i < 5; i++ {
		r.Record(fmt.Sprintf("/tmp/a%d.pdf", i), "ERR_EXTRACTION_FAILED", "text extraction from file failed", 1)
	}
	for i := 0; i < 2; i++ {
		r.Record(fmt.Sprintf("/tmp/b%d.pdf", i), "ERR_FILE_TOO_LARGE", "file exceeds the maximum size for indexing", 1)
	}

	groups := r.Groups()
	if len(groups) != 2 {
		t.Fatalf("Groups(): got %d want 2", len(groups))
	}

	// Sorted by count desc: A(5) before B(2)
	if groups[0].Code != "ERR_EXTRACTION_FAILED" {
		t.Errorf("groups[0].Code: got %q want ERR_EXTRACTION_FAILED", groups[0].Code)
	}
	if groups[0].Count != 5 {
		t.Errorf("groups[0].Count: got %d want 5", groups[0].Count)
	}
	if len(groups[0].SampleFiles) > 3 {
		t.Errorf("groups[0].SampleFiles: got %d want ≤3", len(groups[0].SampleFiles))
	}
	if groups[1].Code != "ERR_FILE_TOO_LARGE" {
		t.Errorf("groups[1].Code: got %q want ERR_FILE_TOO_LARGE", groups[1].Code)
	}
	if groups[1].Count != 2 {
		t.Errorf("groups[1].Count: got %d want 2", groups[1].Count)
	}
	if len(groups[1].SampleFiles) > 3 {
		t.Errorf("groups[1].SampleFiles: got %d want ≤3", len(groups[1].SampleFiles))
	}
}

// REQ-017 / EDGE-012: unknown code yields Label == Code.
func TestFailureRegistry_EDGE012_UnknownCode(t *testing.T) {
	r := NewFailureRegistry(10)
	r.Record("/tmp/foo.xyz", "ERR_FUTURE_UNKNOWN", "some future message", 1)

	groups := r.Groups()
	if len(groups) != 1 {
		t.Fatalf("Groups(): got %d want 1", len(groups))
	}
	if groups[0].Label != "ERR_FUTURE_UNKNOWN" {
		t.Errorf("Label for unknown code: got %q want %q", groups[0].Label, "ERR_FUTURE_UNKNOWN")
	}
}

// EDGE-007: keep recording past cap; DroppedCount keeps incrementing; oldest evicted in order.
func TestFailureRegistry_EDGE007_RepeatedEviction(t *testing.T) {
	const cap = 5
	r := NewFailureRegistry(cap)

	// Record cap + 3 distinct paths.
	// Paths: file0..file7 (8 total), cap=5 → 3 dropped.
	paths := make([]string, cap+3)
	for i := range paths {
		paths[i] = fmt.Sprintf("/tmp/file%d.pdf", i)
		r.Record(paths[i], "ERR_EXTRACTION_FAILED", "msg", 1)
	}

	if r.Len() != cap {
		t.Errorf("Len(): got %d want %d", r.Len(), cap)
	}
	if r.DroppedCount() != 3 {
		t.Errorf("DroppedCount(): got %d want 3", r.DroppedCount())
	}

	// First 3 paths should be evicted.
	snap := r.Snapshot()
	presentPaths := make(map[string]bool)
	for _, e := range snap {
		presentPaths[e.Path] = true
	}
	for i := 0; i < 3; i++ {
		if presentPaths[paths[i]] {
			t.Errorf("path %q should have been evicted", paths[i])
		}
	}
	for i := 3; i < len(paths); i++ {
		if !presentPaths[paths[i]] {
			t.Errorf("path %q should still be present", paths[i])
		}
	}
}

// REQ-012: dedup does NOT move insertion-order position.
func TestFailureRegistry_REQ012_DedupPreservesOrder(t *testing.T) {
	r := NewFailureRegistry(10)
	r.Record("/tmp/a.pdf", "ERR_EXTRACTION_FAILED", "msg", 1)
	r.Record("/tmp/b.pdf", "ERR_EXTRACTION_FAILED", "msg", 1)
	r.Record("/tmp/a.pdf", "ERR_EXTRACTION_FAILED", "updated msg", 2) // update; should stay first

	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("Len: got %d want 2", len(snap))
	}
	if snap[0].Path != "/tmp/a.pdf" {
		t.Errorf("insertion order violated: first entry got %q want /tmp/a.pdf", snap[0].Path)
	}
	if snap[0].Attempts != 2 {
		t.Errorf("Attempts after update: got %d want 2", snap[0].Attempts)
	}
	if snap[0].Message != "updated msg" {
		t.Errorf("Message after update: got %q want 'updated msg'", snap[0].Message)
	}
}

// Snapshot returns a copy; mutations to the returned slice do not affect the registry.
func TestFailureRegistry_SnapshotIsCopy(t *testing.T) {
	r := NewFailureRegistry(10)
	r.Record("/tmp/foo.pdf", "ERR_EXTRACTION_FAILED", "msg", 1)
	snap := r.Snapshot()
	snap[0].Path = "mutated"

	snap2 := r.Snapshot()
	if snap2[0].Path == "mutated" {
		t.Error("Snapshot leaked internal state; mutation affected registry")
	}
}

// Groups sorted by count desc, then by code asc when counts are equal.
func TestFailureRegistry_Groups_SortDeterminism(t *testing.T) {
	r := NewFailureRegistry(100)
	// Two codes with the same count — should sort by code asc.
	for i := 0; i < 3; i++ {
		r.Record(fmt.Sprintf("/tmp/z%d.pdf", i), "ERR_Z_CODE", "msg", 1)
		r.Record(fmt.Sprintf("/tmp/a%d.pdf", i), "ERR_A_CODE", "msg", 1)
	}

	groups := r.Groups()
	if len(groups) != 2 {
		t.Fatalf("Groups(): got %d want 2", len(groups))
	}
	// When count equal, code ascending → ERR_A_CODE before ERR_Z_CODE
	codes := []string{groups[0].Code, groups[1].Code}
	if !sort.StringsAreSorted(codes) {
		t.Errorf("equal-count groups not sorted by code asc: %v", codes)
	}
}

// Groups: Label for known codes matches apperr.Error.Message.
func TestFailureRegistry_Groups_LabelKnownCode(t *testing.T) {
	r := NewFailureRegistry(10)
	r.Record("/tmp/foo.pdf", "ERR_EXTRACTION_FAILED", "text extraction from file failed", 1)

	groups := r.Groups()
	if len(groups) != 1 {
		t.Fatalf("Groups(): got %d want 1", len(groups))
	}
	// Label should be the apperr message for this code.
	want := "text extraction from file failed"
	if groups[0].Label != want {
		t.Errorf("Label: got %q want %q", groups[0].Label, want)
	}
}

// BenchmarkFailureRegistry_Record verifies O(1) amortized cost < 2µs/op (REQ-061).
func BenchmarkFailureRegistry_Record(b *testing.B) {
	const cap = 10_000
	r := NewFailureRegistry(cap)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Keep index within cap so we measure the fast path (map update or append).
		path := fmt.Sprintf("/tmp/file%d.pdf", i%cap)
		r.Record(path, "ERR_EXTRACTION_FAILED", "msg", 1)
	}
}
