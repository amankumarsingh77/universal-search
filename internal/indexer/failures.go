package indexer

import (
	"container/list"
	"sort"
	"sync"
	"time"

	"findo/internal/apperr"
)

// FailureEntry records a single per-file terminal failure for the current indexing run.
type FailureEntry struct {
	Path         string
	Code         string
	Message      string
	Attempts     int
	LastFailedAt time.Time
}

// FailureGroup aggregates failures by error code for display.
type FailureGroup struct {
	Code        string
	Label       string   // apperr.Error.Message for the code, or the code itself for unknown codes (EDGE-012)
	Count       int
	SampleFiles []string // up to 3 paths
}

// codeLabel returns the human-readable message for a known apperr code, falling
// back to the raw code string for codes not in the vocabulary (EDGE-012).
func codeLabel(code string) string {
	// Walk the known errors to find a matching code.
	knownErrors := []*apperr.Error{
		apperr.ErrUnsupportedFormat,
		apperr.ErrExtractionFailed,
		apperr.ErrFileTooLarge,
		apperr.ErrFileUnreadable,
		apperr.ErrEmbedCountMismatch,
		apperr.ErrHnswAdd,
		apperr.ErrStoreWrite,
		apperr.ErrEmbedFailed,
		apperr.ErrRateLimited,
		apperr.ErrModelMismatch,
		apperr.ErrFolderDenied,
		apperr.ErrStoreLocked,
		apperr.ErrConfigInvalid,
		apperr.ErrMigrationFailed,
		apperr.ErrInternal,
	}
	for _, e := range knownErrors {
		if e.Code == code {
			return e.Message
		}
	}
	return code
}

// FailureRegistry is a concurrency-safe, bounded, in-memory registry of per-file
// terminal failures for the current indexing run (REQ-010..017, REQ-061).
//
// Internally uses a doubly-linked list for O(1) FIFO eviction and a map for
// O(1) dedup lookup, giving O(1) amortized Record.
type FailureRegistry struct {
	mu           sync.Mutex
	entries      map[string]*list.Element // path → list.Element containing *FailureEntry
	order        *list.List               // *FailureEntry values in insertion order
	cap          int
	droppedCount int
}

// NewFailureRegistry creates a registry that holds at most cap entries.
// When cap is exceeded the oldest entry is evicted (FIFO).
func NewFailureRegistry(cap int) *FailureRegistry {
	return &FailureRegistry{
		entries: make(map[string]*list.Element),
		order:   list.New(),
		cap:     cap,
	}
}

// Record stores or updates the failure entry for path.
//
// If path already has an entry it is updated in place (attempts, message, code,
// lastFailedAt) without changing its insertion-order position (REQ-012).
// If path is new and the registry is at capacity, the oldest entry is evicted
// first and droppedCount is incremented (REQ-015).
func (r *FailureRegistry) Record(path, code, message string, attempts int) {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()

	if elem, ok := r.entries[path]; ok {
		// Update in place — do NOT move position in the list (REQ-012).
		entry := elem.Value.(*FailureEntry)
		entry.Code = code
		entry.Message = message
		entry.Attempts = attempts
		entry.LastFailedAt = now
		return
	}

	// Evict the oldest entry if we are at capacity (REQ-015).
	if r.order.Len() >= r.cap {
		front := r.order.Front()
		if front != nil {
			evicted := front.Value.(*FailureEntry)
			delete(r.entries, evicted.Path)
			r.order.Remove(front)
			r.droppedCount++
		}
	}

	// Append new entry.
	entry := &FailureEntry{
		Path:         path,
		Code:         code,
		Message:      message,
		Attempts:     attempts,
		LastFailedAt: now,
	}
	elem := r.order.PushBack(entry)
	r.entries[path] = elem
}

// Reset clears all entries and resets droppedCount (REQ-013).
func (r *FailureRegistry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.entries = make(map[string]*list.Element)
	r.order.Init()
	r.droppedCount = 0
}

// Snapshot returns a copy of all entries in insertion order (REQ-011, REQ-014).
// The caller may freely mutate the returned slice and entries.
func (r *FailureRegistry) Snapshot() []FailureEntry {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]FailureEntry, 0, r.order.Len())
	for elem := r.order.Front(); elem != nil; elem = elem.Next() {
		e := elem.Value.(*FailureEntry)
		result = append(result, *e) // copy the struct
	}
	return result
}

// Groups aggregates failures by Code, sorted by count descending then code ascending
// for determinism (REQ-017). Each group carries up to 3 sample file paths.
func (r *FailureRegistry) Groups() []FailureGroup {
	r.mu.Lock()
	defer r.mu.Unlock()

	type groupState struct {
		count   int
		samples []string
		label   string
	}
	byCode := make(map[string]*groupState)

	for elem := r.order.Front(); elem != nil; elem = elem.Next() {
		e := elem.Value.(*FailureEntry)
		gs, ok := byCode[e.Code]
		if !ok {
			gs = &groupState{label: codeLabel(e.Code)}
			byCode[e.Code] = gs
		}
		gs.count++
		if len(gs.samples) < 3 {
			gs.samples = append(gs.samples, e.Path)
		}
	}

	groups := make([]FailureGroup, 0, len(byCode))
	for code, gs := range byCode {
		groups = append(groups, FailureGroup{
			Code:        code,
			Label:       gs.label,
			Count:       gs.count,
			SampleFiles: gs.samples,
		})
	}

	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return groups[i].Code < groups[j].Code
	})

	return groups
}

// Len returns the number of entries currently in the registry.
func (r *FailureRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.order.Len()
}

// DroppedCount returns the number of entries evicted since the last Reset.
func (r *FailureRegistry) DroppedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.droppedCount
}
