// Package store provides SQLite-backed persistence for file and chunk metadata.
package store

import (
	"testing"
	"time"
)

// TestFilenameGlob_BareStarOrderedByModifiedAtDesc verifies EDGE-7: a bare `*`
// glob returns files ordered by modified_at DESC so the most-recently-modified
// files appear first regardless of insertion order.
func TestFilenameGlob_BareStarOrderedByModifiedAtDesc(t *testing.T) {
	s, err := NewStore(":memory:", testLogger)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	now := time.Now()
	paths := []struct {
		path       string
		modifiedAt time.Time
	}{
		{"/home/user/oldest.txt", now.Add(-3 * 24 * time.Hour)},
		{"/home/user/newest.txt", now},
		{"/home/user/middle.txt", now.Add(-1 * 24 * time.Hour)},
	}
	for _, p := range paths {
		_, err := s.UpsertFile(FileRecord{
			Path:       p.path,
			FileType:   "text",
			Extension:  ".txt",
			SizeBytes:  100,
			ModifiedAt: p.modifiedAt,
			IndexedAt:  now,
		})
		if err != nil {
			t.Fatalf("UpsertFile(%q): %v", p.path, err)
		}
	}

	results, err := s.FilenameGlob("*", 10)
	if err != nil {
		t.Fatalf("FilenameGlob(*): %v", err)
	}
	if len(results) < 3 {
		t.Fatalf("expected at least 3 results, got %d", len(results))
	}

	// Verify descending order by modified_at.
	for i := 1; i < len(results); i++ {
		if results[i].ModifiedAt.After(results[i-1].ModifiedAt) {
			t.Errorf("EDGE-7: result[%d] (%s, modified %v) is newer than result[%d] (%s, modified %v) — not DESC order",
				i, results[i].Path, results[i].ModifiedAt,
				i-1, results[i-1].Path, results[i-1].ModifiedAt,
			)
		}
	}
	// First result must be the newest.
	if results[0].Path != "/home/user/newest.txt" {
		t.Errorf("EDGE-7: first result should be newest.txt, got %q", results[0].Path)
	}
}

func TestFilenameGlob_StarExtension(t *testing.T) {
	s := newStoreForFTS(t)
	insertFile(t, s, "/home/user/main.py", "text", ".py")
	insertFile(t, s, "/home/user/util.py", "text", ".py")
	insertFile(t, s, "/home/user/main.go", "text", ".go")

	results, err := s.FilenameGlob("*.py", 10)
	if err != nil {
		t.Fatalf("FilenameGlob: %v", err)
	}
	if len(results) < 2 {
		t.Errorf("got %d results, want at least 2 .py files", len(results))
	}
	for _, f := range results {
		if f.Extension != ".py" {
			t.Errorf("unexpected extension %q in glob result for *.py", f.Extension)
		}
	}
}

func TestFilenameGlob_QuestionMark(t *testing.T) {
	s := newStoreForFTS(t)
	insertFile(t, s, "/home/user/file1.go", "text", ".go")
	insertFile(t, s, "/home/user/file2.go", "text", ".go")
	insertFile(t, s, "/home/user/file12.go", "text", ".go") // should NOT match file?.go

	results, err := s.FilenameGlob("/home/user/file?.go", 10)
	if err != nil {
		t.Fatalf("FilenameGlob: %v", err)
	}
	// file1.go and file2.go should match; file12.go should not.
	for _, f := range results {
		if f.Basename == "file12.go" {
			t.Errorf("file12.go should not match file?.go (? matches exactly one char)")
		}
	}
}

func TestFilenameGlob_EmptyPattern(t *testing.T) {
	s := newStoreForFTS(t)
	insertFile(t, s, "/home/user/main.py", "text", ".py")

	results, err := s.FilenameGlob("", 10)
	if err != nil {
		t.Fatalf("FilenameGlob empty: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty pattern, got %d", len(results))
	}
}

func TestFilenameGlob_RespectsLimit(t *testing.T) {
	s := newStoreForFTS(t)
	for i := 0; i < 10; i++ {
		insertFile(t, s, "/home/user/demo_file.go", "text", ".go")
		// Use unique paths by including index.
		insertFile(t, s, "/home/user/"+string(rune('a'+i))+".go", "text", ".go")
	}

	results, err := s.FilenameGlob("*.go", 3)
	if err != nil {
		t.Fatalf("FilenameGlob: %v", err)
	}
	if len(results) > 3 {
		t.Errorf("got %d results, want <= 3 (limit)", len(results))
	}
}

func TestGlobToLike(t *testing.T) {
	tests := []struct {
		name    string
		glob    string
		want    string
	}{
		{"star wildcard", "*.py", "%.py"},
		{"question wildcard", "file?.go", "file_.go"},
		{"literal percent", "%exact%.txt", `\%exact\%.txt`},
		{"literal underscore", "_file_.go", `\_file\_.go`},
		{"combined wildcards", "src/*.go", "src/%.go"},
		{"no wildcards", "demo.py", "demo.py"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := globToLike(tt.glob)
			if got != tt.want {
				t.Errorf("globToLike(%q) = %q, want %q", tt.glob, got, tt.want)
			}
		})
	}
}

func TestIsGlob(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"*.py", true},
		{"file?.go", true},
		{"demo.go", false},
		{"", false},
		{"path/to/file", false},
	}

	for _, tt := range tests {
		got := isGlob(tt.pattern)
		if got != tt.want {
			t.Errorf("isGlob(%q) = %v, want %v", tt.pattern, got, tt.want)
		}
	}
}
