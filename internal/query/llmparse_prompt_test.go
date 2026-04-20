package query

import (
	"strings"
	"testing"
	"time"
)

// TestSystemPrompt_ContainsCriticalInstruction asserts REQ-017 — the prompt
// includes the explicit "never leave Must empty" instruction that the LLM is
// expected to follow.
func TestSystemPrompt_ContainsCriticalInstruction(t *testing.T) {
	t.Parallel()
	prompt := buildSystemPrompt(time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC))
	// Check for the key fragments — the full sentence wraps across lines in
	// the source, so substring match would fail on the literal newline.
	for _, fragment := range []string{
		"Never leave Must empty",
		"structured signal is",
		"present.",
	} {
		if !strings.Contains(prompt, fragment) {
			t.Errorf("prompt missing critical-instruction fragment %q", fragment)
		}
	}
}

// TestSystemPrompt_ContainsAllRequiredFewShots asserts REQ-016 — the prompt
// includes one example of each documented failure category. Looking for
// substring markers rather than exact strings so future prompt edits remain
// flexible without breaking tests.
func TestSystemPrompt_ContainsAllRequiredFewShots(t *testing.T) {
	t.Parallel()
	prompt := buildSystemPrompt(time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC))

	// Each marker is a substring that uniquely identifies a category-specific
	// example. Failing on any one means a category is missing.
	required := map[string]string{
		"bare extension example":   "all .py files",
		"negation example":         "aren't",
		"multi-constraint example": "large videos from last month",
		"bare temporal example":    "files modified yesterday",
		"path example":             "in my Pictures folder",
	}
	for label, marker := range required {
		if !strings.Contains(prompt, marker) {
			t.Errorf("prompt missing %s (expected substring %q)", label, marker)
		}
	}
}

// TestSystemPrompt_ResolvesDatePlaceholders asserts the prompt's date
// placeholders are resolved against the injected `now`, not time.Now() at
// runtime.
func TestSystemPrompt_ResolvesDatePlaceholders(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	prompt := buildSystemPrompt(now)

	// today
	if !strings.Contains(prompt, "Today is 2026-04-20") {
		t.Errorf("expected 'Today is 2026-04-20' in prompt")
	}
	// yesterday start (RFC3339)
	if !strings.Contains(prompt, "2026-04-19T00:00:00Z") {
		t.Errorf("expected yesterday start (2026-04-19T00:00:00Z) in prompt")
	}
	// last-month start
	if !strings.Contains(prompt, "2026-03-20T00:00:00Z") {
		t.Errorf("expected last-month start (2026-03-20T00:00:00Z) in prompt")
	}
}

// TestSystemPrompt_NoUnresolvedPlaceholders asserts no %s placeholders remain
// after buildSystemPrompt runs.
func TestSystemPrompt_NoUnresolvedPlaceholders(t *testing.T) {
	t.Parallel()
	prompt := buildSystemPrompt(time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC))
	if strings.Contains(prompt, "%s") || strings.Contains(prompt, "%!s") {
		t.Fatalf("prompt has unresolved format placeholders:\n%s", prompt)
	}
}
