package query

import (
	"sync"
	"testing"
)

// TestDetectStructuredFields is a table-driven test covering REQ-001 through
// REQ-007 and edge cases EDGE-001, EDGE-003, EDGE-004, EDGE-013, EDGE-014.
func TestDetectStructuredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  StructuredSignal
		// REQ/EDGE reference for documentation
		req string
	}{
		// --- REQ-001: all-false for queries with no structured intent.
		// Note: "photos" and "videos" etc. ARE structured intent (they map to
		// file_type), so a truly semantic-only query must avoid those words.
		{
			name:  "semantic only query returns all false",
			input: "sunset over the ocean",
			want:  StructuredSignal{},
			req:   "REQ-001",
		},

		// --- REQ-002: bare extension detection ---
		{
			name:  "bare .py extension",
			input: "all .py files",
			want:  StructuredSignal{Extension: true},
			req:   "REQ-002",
		},
		{
			// "any .pdf" mentions both an extension token AND "pdf" which is in
			// KnownKindValues — both signals fire, which is correct.
			name:  "bare .pdf extension also triggers FileType via pdf synonym",
			input: "any .pdf",
			want:  StructuredSignal{Extension: true, FileType: true},
			req:   "REQ-002, REQ-003",
		},
		{
			name:  "bare .go extension mid-sentence",
			input: "foo.go bar",
			want:  StructuredSignal{Extension: true},
			req:   "REQ-002",
		},

		// --- REQ-003: kind synonyms (case-insensitive, whole-word) ---
		{
			name:  "video as kind word",
			input: "video taken on the mountain",
			want:  StructuredSignal{FileType: true},
			req:   "REQ-003",
		},
		{
			name:  "PHOTOS uppercase as kind word",
			input: "PHOTOS of dogs",
			want:  StructuredSignal{FileType: true},
			req:   "REQ-003",
		},
		{
			name:  "pdf as kind synonym",
			input: "any pdf",
			want:  StructuredSignal{FileType: true},
			req:   "REQ-003",
		},

		// --- REQ-004: size with unit ---
		{
			name:  "100MB no space",
			input: "100MB",
			want:  StructuredSignal{SizeBytes: true},
			req:   "REQ-004",
		},
		{
			name:  "1 gb with space",
			input: "over 1 gb",
			want:  StructuredSignal{SizeBytes: true},
			req:   "REQ-004",
		},
		{
			name:  "5 tb size",
			input: "5 tb",
			want:  StructuredSignal{SizeBytes: true},
			req:   "REQ-004",
		},
		{
			name:  "number with non-size unit returns false",
			input: "100 monkeys",
			want:  StructuredSignal{},
			req:   "REQ-004, EDGE-004",
		},

		// --- REQ-005: size adjectives ---
		{
			name:  "large adjective",
			input: "large videos",
			want:  StructuredSignal{FileType: true, SizeBytes: true},
			req:   "REQ-005",
		},
		{
			name:  "smaller adjective",
			input: "smaller files",
			want:  StructuredSignal{SizeBytes: true},
			req:   "REQ-005",
		},
		{
			name:  "huge adjective",
			input: "huge documents",
			want:  StructuredSignal{FileType: true, SizeBytes: true},
			req:   "REQ-005",
		},
		{
			name:  "tiny adjective with images kind word",
			input: "tiny images",
			want:  StructuredSignal{SizeBytes: true, FileType: true},
			req:   "REQ-005",
		},

		// --- REQ-006: temporal tokens ---
		{
			name:  "last week phrase",
			input: "files from last week",
			want:  StructuredSignal{ModifiedAt: true},
			req:   "REQ-006",
		},
		{
			name:  "3 days ago phrase",
			input: "3 days ago",
			want:  StructuredSignal{ModifiedAt: true},
			req:   "REQ-006",
		},
		{
			name:  "recent token",
			input: "recent docs",
			want:  StructuredSignal{FileType: true, ModifiedAt: true},
			req:   "REQ-006",
		},
		{
			name:  "yesterday token",
			input: "files modified yesterday",
			want:  StructuredSignal{ModifiedAt: true},
			req:   "REQ-006",
		},
		{
			name:  "today token",
			input: "created today",
			want:  StructuredSignal{ModifiedAt: true},
			req:   "REQ-006",
		},
		{
			name:  "2 months ago phrase",
			input: "2 months ago",
			want:  StructuredSignal{ModifiedAt: true},
			req:   "REQ-006",
		},

		// --- REQ-007: path detection ---
		{
			name:  "files in Downloads",
			input: "files in Downloads",
			want:  StructuredSignal{Path: true},
			req:   "REQ-007",
		},
		{
			name:  "in MyProject capitalized word",
			input: "in MyProject",
			want:  StructuredSignal{Path: true},
			req:   "REQ-007",
		},
		{
			name:  "Desktop known root folder",
			input: "Desktop pdf",
			want:  StructuredSignal{Path: true, FileType: true},
			req:   "REQ-007",
		},
		{
			// "Documents" is both a known root folder name AND matches
			// "document" (plural) in KnownKindValues — both signals fire.
			name:  "Documents known root folder also triggers FileType via document",
			input: "search Documents",
			want:  StructuredSignal{Path: true, FileType: true},
			req:   "REQ-007",
		},
		{
			// "Pictures" is a known root folder AND matches "picture" plural
			// in KnownKindValues — both signals fire.
			name:  "Pictures known root folder also triggers FileType via picture",
			input: "Pictures from vacation",
			want:  StructuredSignal{Path: true, FileType: true},
			req:   "REQ-007",
		},

		// --- EDGE-001: empty string ---
		{
			name:  "empty string returns all false",
			input: "",
			want:  StructuredSignal{},
			req:   "EDGE-001",
		},

		// --- EDGE-003: word-boundary check for kind words ---
		{
			name:  "videogame does not trigger FileType",
			input: "videogame",
			want:  StructuredSignal{},
			req:   "EDGE-003",
		},
		{
			// VideoGame is a single token (no word boundary inside) so video/game
			// kind word does not fire. Pictures matches the path root AND the
			// "picture" plural in KnownKindValues, so FileType also fires —
			// which is the intended behavior of the plural-aware detector.
			name:  "VideoGame Pictures: VideoGame does not trigger FileType, Pictures does",
			input: "VideoGame Pictures",
			want:  StructuredSignal{Path: true, FileType: true},
			req:   "EDGE-003",
		},

		// --- EDGE-004: number with non-size unit ---
		{
			name:  "100 monkeys does not trigger SizeBytes",
			input: "100 monkeys",
			want:  StructuredSignal{},
			req:   "EDGE-004",
		},

		// --- EDGE-014: combined signals ---
		{
			name:  "yesterday and .py files trigger both ModifiedAt and Extension",
			input: "yesterday's .py files",
			want:  StructuredSignal{Extension: true, ModifiedAt: true},
			req:   "EDGE-014",
		},
		{
			name:  "temporal and kind combined",
			input: "videos from last month",
			want:  StructuredSignal{FileType: true, ModifiedAt: true},
			req:   "EDGE-014",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := DetectStructuredFields(tt.input)
			if got != tt.want {
				t.Errorf("[%s] DetectStructuredFields(%q)\n  got  %+v\n  want %+v",
					tt.req, tt.input, got, tt.want)
			}
		})
	}
}

// TestDetectStructuredFields_AnyMethod verifies the Any() convenience method.
func TestDetectStructuredFields_AnyMethod(t *testing.T) {
	t.Parallel()

	if (StructuredSignal{}).Any() {
		t.Error("empty StructuredSignal.Any() should be false")
	}
	if !(StructuredSignal{Extension: true}).Any() {
		t.Error("StructuredSignal{Extension: true}.Any() should be true")
	}
}

// TestDetectStructuredFields_FieldsMethod verifies the Fields() method returns
// the correct names for signaled fields.
func TestDetectStructuredFields_FieldsMethod(t *testing.T) {
	t.Parallel()

	sig := StructuredSignal{Extension: true, ModifiedAt: true}
	fields := sig.Fields()
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d: %v", len(fields), fields)
	}
	if fields[0] != "extension" {
		t.Errorf("expected fields[0]=\"extension\", got %q", fields[0])
	}
	if fields[1] != "modified_at" {
		t.Errorf("expected fields[1]=\"modified_at\", got %q", fields[1])
	}
}

// TestDetectStructuredFields_Concurrent verifies the function is safe for
// concurrent use (EDGE-013).
func TestDetectStructuredFields_Concurrent(t *testing.T) {
	t.Parallel()

	const goroutines = 50
	inputs := []string{
		"all .py files",
		"videos from last week",
		"100MB documents",
		"sunset photos",
		"",
		"in Downloads",
		"large .pdf files from yesterday",
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			input := inputs[i%len(inputs)]
			// Just ensure no panic / race condition.
			_ = DetectStructuredFields(input)
		}()
	}
	wg.Wait()
}
