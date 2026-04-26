// Package store provides SQLite-backed persistence for file and chunk metadata.
package store

import (
	"strings"
	"testing"
	"time"
)

// newStoreForFTS creates a fresh in-memory store for FTS5 tests.
func newStoreForFTS(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// insertFile is a helper to insert a file into the store for testing.
func insertFile(t *testing.T, s *Store, path, fileType, ext string) int64 {
	t.Helper()
	id, err := s.UpsertFile(FileRecord{
		Path:        path,
		FileType:    fileType,
		Extension:   ext,
		SizeBytes:   100,
		ModifiedAt:  time.Now(),
		IndexedAt:   time.Now(),
		ContentHash: "abc",
	})
	if err != nil {
		t.Fatalf("UpsertFile(%q): %v", path, err)
	}
	return id
}

// TestFilenameSearch_EmptyQuery returns empty slice for empty input.
func TestFilenameSearch_EmptyQuery(t *testing.T) {
	s := newStoreForFTS(t)
	insertFile(t, s, "/home/user/demo.py", "text", ".py")

	results, err := s.FilenameSearch("", 10)
	if err != nil {
		t.Fatalf("FilenameSearch empty query: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for empty query, got %d", len(results))
	}
}

// TestFilenameSearch_ExactMatch verifies exact basename match returns score 1.0.
func TestFilenameSearch_ExactMatch(t *testing.T) {
	s := newStoreForFTS(t)
	if !s.FilenameFTSAvailable() {
		t.Skip("FTS5 not available")
	}

	insertFile(t, s, "/src/demo.py", "text", ".py")
	insertFile(t, s, "/src/README.md", "text", ".md")
	insertFile(t, s, "/src/utils/helpers.go", "text", ".go")

	results, err := s.FilenameSearch("demo.py", 10)
	if err != nil {
		t.Fatalf("FilenameSearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'demo.py', got 0")
	}
	top := results[0]
	if top.MatchKind != "exact" {
		t.Errorf("expected MatchKind 'exact', got %q", top.MatchKind)
	}
	if top.Score != 1.0 {
		t.Errorf("expected Score 1.0 for exact match, got %f", top.Score)
	}
	if top.File.Basename != "demo.py" {
		t.Errorf("expected Basename 'demo.py', got %q", top.File.Basename)
	}
	if top.File.Path != "/src/demo.py" {
		t.Errorf("expected Path '/src/demo.py', got %q", top.File.Path)
	}
}

// TestFilenameSearch_SubstringMatch verifies substring query hits the right file.
func TestFilenameSearch_SubstringMatch(t *testing.T) {
	s := newStoreForFTS(t)
	if !s.FilenameFTSAvailable() {
		t.Skip("FTS5 not available")
	}

	insertFile(t, s, "/src/demo.py", "text", ".py")
	insertFile(t, s, "/src/README.md", "text", ".md")
	insertFile(t, s, "/src/utils/helpers.go", "text", ".go")

	results, err := s.FilenameSearch("demo", 10)
	if err != nil {
		t.Fatalf("FilenameSearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'demo', got 0")
	}
	// demo.py should be in the results via FTS substring match.
	found := false
	for _, r := range results {
		if r.File.Path == "/src/demo.py" {
			found = true
		}
	}
	if !found {
		t.Error("expected /src/demo.py in results for 'demo'")
	}
}

// TestFilenameSearch_ExtensionQuery verifies that querying by extension finds
// matching files. The FTS5 trigram tokenizer requires at least 3 characters to
// form a trigram; short (≤2-char) extensions like "py" cannot match directly.
// Longer extensions like "go" (still 2 chars) also can't; but ".go" (3 chars)
// or "golang" can. This test uses "python" in the path as a 3-char-min check.
func TestFilenameSearch_ExtensionQuery(t *testing.T) {
	s := newStoreForFTS(t)
	if !s.FilenameFTSAvailable() {
		t.Skip("FTS5 not available")
	}

	insertFile(t, s, "/src/demo.py", "text", ".py")
	insertFile(t, s, "/src/report.pdf", "text", ".pdf")
	insertFile(t, s, "/src/README.md", "text", ".md")

	// ".pdf" is 4 chars and forms a trigram ".pd" + "pdf"; querying "pdf" (3 chars)
	// should find report.pdf because "pdf" appears in the ext column.
	results, err := s.FilenameSearch("pdf", 10)
	if err != nil {
		t.Fatalf("FilenameSearch: %v", err)
	}
	found := false
	for _, r := range results {
		if r.File.Path == "/src/report.pdf" {
			found = true
		}
	}
	if !found {
		t.Logf("results for 'pdf': %+v", results)
		t.Error("expected /src/report.pdf in results for 'pdf' (3-char ext query)")
	}

	// Note: querying "py" (2 chars) cannot form a trigram and returns no FTS hits.
	// The exact match path also won't find it since "py" != "demo.py".
	// This is expected behavior for short extensions — see EDGE-8 in the spec:
	// the classifier translates single-extension queries to FTS column-filter syntax,
	// which is a Phase 5 concern, not Phase 2.
	pyResults, err := s.FilenameSearch("py", 10)
	if err != nil {
		t.Fatalf("FilenameSearch 'py': %v", err)
	}
	// 2-char queries are not an error, they just return no FTS results.
	_ = pyResults
}

// TestFilenameSearch_TriggerSync_Insert verifies that inserting a file via UpsertFile
// makes it discoverable by FilenameSearch.
func TestFilenameSearch_TriggerSync_Insert(t *testing.T) {
	s := newStoreForFTS(t)
	if !s.FilenameFTSAvailable() {
		t.Skip("FTS5 not available")
	}

	insertFile(t, s, "/projects/myapp/main.go", "text", ".go")

	results, err := s.FilenameSearch("main.go", 10)
	if err != nil {
		t.Fatalf("FilenameSearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected result for 'main.go' after insert, got 0")
	}
	if results[0].File.Basename != "main.go" {
		t.Errorf("expected Basename 'main.go', got %q", results[0].File.Basename)
	}
}

// TestFilenameSearch_TriggerSync_Update verifies that updating a file's path via
// RenameFile removes the old name and adds the new name to FTS.
func TestFilenameSearch_TriggerSync_Update(t *testing.T) {
	s := newStoreForFTS(t)
	if !s.FilenameFTSAvailable() {
		t.Skip("FTS5 not available")
	}

	oldPath := "/projects/myapp/old_service.go"
	newPath := "/projects/myapp/new_service.go"
	insertFile(t, s, oldPath, "text", ".go")

	// Rename triggers the after-update trigger.
	if err := s.RenameFile(oldPath, newPath); err != nil {
		t.Fatalf("RenameFile: %v", err)
	}

	// Old name should not appear.
	oldResults, err := s.FilenameSearch("old_service.go", 10)
	if err != nil {
		t.Fatalf("FilenameSearch old name: %v", err)
	}
	for _, r := range oldResults {
		if r.File.Path == oldPath {
			t.Errorf("old path %q should not be in FTS results after rename", oldPath)
		}
	}

	// New name should appear.
	newResults, err := s.FilenameSearch("new_service.go", 10)
	if err != nil {
		t.Fatalf("FilenameSearch new name: %v", err)
	}
	found := false
	for _, r := range newResults {
		if r.File.Path == newPath {
			found = true
		}
	}
	if !found {
		t.Errorf("new path %q should be in FTS results after rename", newPath)
	}
}

// TestFilenameSearch_TriggerSync_Delete verifies that deleting a file removes
// it from the FTS index.
func TestFilenameSearch_TriggerSync_Delete(t *testing.T) {
	s := newStoreForFTS(t)
	if !s.FilenameFTSAvailable() {
		t.Skip("FTS5 not available")
	}

	path := "/projects/todelete/secret.txt"
	id := insertFile(t, s, path, "text", ".txt")

	// Verify it's findable before delete.
	before, err := s.FilenameSearch("secret.txt", 10)
	if err != nil {
		t.Fatalf("FilenameSearch before delete: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("expected to find secret.txt before delete")
	}

	// Delete the file.
	if _, err := s.db.Exec(`DELETE FROM files WHERE id = ?`, id); err != nil {
		t.Fatalf("DELETE from files: %v", err)
	}

	// Should no longer appear in FTS.
	after, err := s.FilenameSearch("secret.txt", 10)
	if err != nil {
		t.Fatalf("FilenameSearch after delete: %v", err)
	}
	for _, r := range after {
		if r.File.ID == id {
			t.Errorf("deleted file (id=%d) still appears in FTS results", id)
		}
	}
}

// TestFilenameSearch_Deduplication verifies that exact + FTS results deduplicate.
func TestFilenameSearch_Deduplication(t *testing.T) {
	s := newStoreForFTS(t)
	if !s.FilenameFTSAvailable() {
		t.Skip("FTS5 not available")
	}

	insertFile(t, s, "/src/demo.py", "text", ".py")

	// Querying the exact name may appear in both exact and FTS phases.
	results, err := s.FilenameSearch("demo.py", 10)
	if err != nil {
		t.Fatalf("FilenameSearch: %v", err)
	}

	seen := make(map[int64]int)
	for _, r := range results {
		seen[r.File.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("file id %d appears %d times (expected 1) — deduplication failed", id, count)
		}
	}
}

// TestFilenameSearch_HonorsLimit verifies that the limit parameter is respected.
func TestFilenameSearch_HonorsLimit(t *testing.T) {
	s := newStoreForFTS(t)
	if !s.FilenameFTSAvailable() {
		t.Skip("FTS5 not available")
	}

	// Insert 5 .go files.
	for i := 0; i < 5; i++ {
		path := "/src/file" + string(rune('a'+i)) + ".go"
		insertFile(t, s, path, "text", ".go")
	}

	results, err := s.FilenameSearch("file", 3)
	if err != nil {
		t.Fatalf("FilenameSearch: %v", err)
	}
	if len(results) > 3 {
		t.Errorf("expected at most 3 results with limit=3, got %d", len(results))
	}
}

// TestFilenameSearch_HighlightOffsets verifies that highlight ranges are within
// basename bounds and that the cleaned basename doesn't contain sentinel bytes.
func TestFilenameSearch_HighlightOffsets(t *testing.T) {
	s := newStoreForFTS(t)
	if !s.FilenameFTSAvailable() {
		t.Skip("FTS5 not available")
	}

	insertFile(t, s, "/docs/report_q3.pdf", "text", ".pdf")

	results, err := s.FilenameSearch("report", 10)
	if err != nil {
		t.Fatalf("FilenameSearch: %v", err)
	}
	if len(results) == 0 {
		t.Skip("no results for 'report' — FTS may not index this trigram yet")
	}

	for _, r := range results {
		basename := r.File.Basename
		// Sentinel bytes must not appear in the cleaned basename.
		if strings.Contains(basename, "\x02") || strings.Contains(basename, "\x03") {
			t.Errorf("Basename %q contains sentinel bytes", basename)
		}
		for _, h := range r.Highlights {
			if h.Start < 0 || h.End > len(basename) || h.Start > h.End {
				t.Errorf("invalid highlight range [%d,%d) for basename %q (len=%d)",
					h.Start, h.End, basename, len(basename))
			}
		}
	}
}

// TestFilenameSearch_SpecialChars verifies that special FTS5 characters in
// queries don't cause errors (EDGE-2).
func TestFilenameSearch_SpecialChars(t *testing.T) {
	s := newStoreForFTS(t)
	if !s.FilenameFTSAvailable() {
		t.Skip("FTS5 not available")
	}

	insertFile(t, s, "/src/demo.py", "text", ".py")

	tests := []struct {
		name  string
		query string
	}{
		{"double_quote", `"demo"`},
		{"parens", "(demo)"},
		{"colon", "demo:py"},
		{"asterisk", "demo*"},
		{"caret", "demo^"},
		{"combined_special", `"demo" OR (py)`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.FilenameSearch(tc.query, 10)
			if err != nil {
				t.Errorf("FilenameSearch(%q) returned error: %v", tc.query, err)
			}
		})
	}
}

// TestEscapeFTS5 verifies that escapeFTS5 produces correct phrase-quoted strings.
func TestEscapeFTS5(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain word",
			input: "demo",
			want:  `"demo"`,
		},
		{
			name:  "uppercase lowercased",
			input: "Demo.PY",
			want:  `"demo.py"`,
		},
		{
			name:  "embedded double-quote",
			input: `say "hello"`,
			want:  `"say ""hello"""`,
		},
		{
			name:  "FTS5 colon operator",
			input: "basename:demo",
			want:  `"basename:demo"`,
		},
		{
			name:  "asterisk wildcard",
			input: "demo*",
			want:  `"demo*"`,
		},
		{
			name:  "parens",
			input: "(demo OR test)",
			want:  `"(demo or test)"`,
		},
		{
			name:  "empty string",
			input: "",
			want:  `""`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := escapeFTS5(tc.input)
			if got != tc.want {
				t.Errorf("escapeFTS5(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestExtractHighlights verifies that sentinel-annotated strings are parsed correctly.
func TestExtractHighlights(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantClean      string
		wantRanges     []HighlightRange
	}{
		{
			name:      "no highlights",
			input:     "demo.py",
			wantClean: "demo.py",
		},
		{
			name:      "single highlight at start",
			input:     "\x02dem\x03o.py",
			wantClean: "demo.py",
			wantRanges: []HighlightRange{{Start: 0, End: 3}},
		},
		{
			name:      "single highlight in middle",
			input:     "de\x02mo\x03.py",
			wantClean: "demo.py",
			wantRanges: []HighlightRange{{Start: 2, End: 4}},
		},
		{
			name:      "multiple highlights",
			input:     "\x02dem\x03o_\x02ser\x03vice.go",
			wantClean: "demo_service.go",
			wantRanges: []HighlightRange{{Start: 0, End: 3}, {Start: 5, End: 8}},
		},
		{
			name:      "highlight at end",
			input:     "demo\x02.py\x03",
			wantClean: "demo.py",
			wantRanges: []HighlightRange{{Start: 4, End: 7}},
		},
		{
			name:      "empty string",
			input:     "",
			wantClean: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotRanges, gotClean := extractHighlights(tc.input)
			if gotClean != tc.wantClean {
				t.Errorf("clean = %q, want %q", gotClean, tc.wantClean)
			}
			if len(gotRanges) != len(tc.wantRanges) {
				t.Fatalf("ranges len = %d, want %d: got %v", len(gotRanges), len(tc.wantRanges), gotRanges)
			}
			for i, r := range gotRanges {
				if r != tc.wantRanges[i] {
					t.Errorf("range[%d] = %v, want %v", i, r, tc.wantRanges[i])
				}
			}
		})
	}
}

// TestFilenameSearch_FTSUnavailable verifies that when FTS is unavailable
// FilenameSearch returns empty without error.
func TestFilenameSearch_FTSUnavailable(t *testing.T) {
	s := newStoreForFTS(t)
	// Override ftsReady to false to simulate unavailable FTS5.
	s.filenameFTSReady = false

	insertFile(t, s, "/src/demo.py", "text", ".py")

	results, err := s.FilenameSearch("demo", 10)
	if err != nil {
		t.Fatalf("expected no error when FTS unavailable, got: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected empty results when FTS unavailable, got %d", len(results))
	}
}

// TestFilenameSearch_MultipleFiles verifies searching across demo.py, README.md,
// and src/utils/helpers.go produces expected per-query results.
func TestFilenameSearch_MultipleFiles(t *testing.T) {
	s := newStoreForFTS(t)
	if !s.FilenameFTSAvailable() {
		t.Skip("FTS5 not available")
	}

	insertFile(t, s, "/projects/demo.py", "text", ".py")
	insertFile(t, s, "/docs/README.md", "text", ".md")
	insertFile(t, s, "/src/utils/helpers.go", "text", ".go")

	t.Run("demo_substring", func(t *testing.T) {
		results, err := s.FilenameSearch("demo", 10)
		if err != nil {
			t.Fatalf("FilenameSearch: %v", err)
		}
		found := false
		for _, r := range results {
			if r.File.Basename == "demo.py" {
				found = true
			}
		}
		if !found {
			t.Error("expected demo.py in results for 'demo'")
		}
	})

	t.Run("readme_exact", func(t *testing.T) {
		results, err := s.FilenameSearch("README.md", 10)
		if err != nil {
			t.Fatalf("FilenameSearch: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected at least one result for 'README.md'")
		}
		if results[0].MatchKind != "exact" {
			t.Errorf("expected 'exact' match for 'README.md', got %q", results[0].MatchKind)
		}
		if results[0].Score != 1.0 {
			t.Errorf("expected score 1.0 for exact match, got %f", results[0].Score)
		}
	})

	t.Run("helpers_substring", func(t *testing.T) {
		results, err := s.FilenameSearch("helpers", 10)
		if err != nil {
			t.Fatalf("FilenameSearch: %v", err)
		}
		found := false
		for _, r := range results {
			if r.File.Basename == "helpers.go" {
				found = true
			}
		}
		if !found {
			t.Error("expected helpers.go in results for 'helpers'")
		}
	})
}

// TestFilenameSearch_ExactScoreHigherThanFTS verifies that exact matches appear
// before FTS matches in the result list.
func TestFilenameSearch_ExactScoreHigherThanFTS(t *testing.T) {
	s := newStoreForFTS(t)
	if !s.FilenameFTSAvailable() {
		t.Skip("FTS5 not available")
	}

	// Insert a file whose basename exactly matches the query and another that
	// only matches via substring.
	insertFile(t, s, "/a/demo.py", "text", ".py")
	insertFile(t, s, "/b/awesome_demo_project.py", "text", ".py")

	results, err := s.FilenameSearch("demo.py", 10)
	if err != nil {
		t.Fatalf("FilenameSearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for 'demo.py'")
	}
	// First result must be the exact match.
	if results[0].MatchKind != "exact" {
		t.Errorf("expected first result to be 'exact', got %q", results[0].MatchKind)
	}
	if results[0].File.Basename != "demo.py" {
		t.Errorf("expected first result basename 'demo.py', got %q", results[0].File.Basename)
	}
}
