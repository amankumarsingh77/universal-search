package main

import (
	"log/slog"
	"testing"

	"universal-search/internal/store"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	s, err := store.NewStore(":memory:", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &App{store: s, logger: slog.Default()}
}

func TestSeedDefaultIgnorePatterns_SeedsOnEmptyDB(t *testing.T) {
	a := newTestApp(t)
	a.seedDefaultIgnorePatterns()

	patterns, err := a.GetIgnoredFolders()
	if err != nil {
		t.Fatalf("GetIgnoredFolders returned error: %v", err)
	}
	if len(patterns) != len(defaultIgnorePatterns) {
		t.Errorf("expected %d patterns, got %d", len(defaultIgnorePatterns), len(patterns))
	}
}

func TestSeedDefaultIgnorePatterns_SkipsIfPatternsExist(t *testing.T) {
	a := newTestApp(t)

	// Add one pattern before seeding.
	if err := a.store.AddExcludedPattern("mypattern"); err != nil {
		t.Fatal(err)
	}

	a.seedDefaultIgnorePatterns()

	patterns, err := a.GetIgnoredFolders()
	if err != nil {
		t.Fatalf("GetIgnoredFolders returned error: %v", err)
	}
	if len(patterns) != 1 {
		t.Errorf("expected 1 pattern (seeding should be skipped), got %d", len(patterns))
	}
}

func TestAddIgnoredFolder_EmptyPattern_ReturnsError(t *testing.T) {
	a := newTestApp(t)

	if err := a.AddIgnoredFolder(""); err == nil {
		t.Error("expected error for empty pattern, got nil")
	}

	if err := a.AddIgnoredFolder("  "); err == nil {
		t.Error("expected error for whitespace-only pattern, got nil")
	}
}

func TestAddIgnoredFolder_DuplicatePattern_Succeeds(t *testing.T) {
	a := newTestApp(t)

	if err := a.AddIgnoredFolder("node_modules"); err != nil {
		t.Fatalf("first add failed: %v", err)
	}
	if err := a.AddIgnoredFolder("node_modules"); err != nil {
		t.Fatalf("second add (duplicate) failed: %v", err)
	}

	patterns, err := a.GetIgnoredFolders()
	if err != nil {
		t.Fatalf("GetIgnoredFolders returned error: %v", err)
	}
	if len(patterns) != 1 {
		t.Errorf("expected 1 pattern after duplicate add, got %d", len(patterns))
	}
}

func TestRemoveIgnoredFolder_NonExistent_ReturnsNil(t *testing.T) {
	a := newTestApp(t)

	if err := a.RemoveIgnoredFolder("nonexistent"); err != nil {
		t.Errorf("expected nil for removing nonexistent pattern, got: %v", err)
	}
}
