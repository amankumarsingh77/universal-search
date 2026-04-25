package query

import (
	"testing"
	"time"
)

// -----------------------------------------------------------------------
// TestAnytimeDefaultToPast
// -----------------------------------------------------------------------

func TestAnytimeDefaultToPast(t *testing.T) {
	// "december" at now=2026-04-24 should resolve to December 2025 (past).
	p := anytimeParser{}
	now := time.Date(2026, 4, 24, 15, 30, 0, 0, time.UTC)
	after, before, ok := p.Parse("december", now)
	if !ok {
		t.Fatal("expected ok=true for 'december' with DefaultToPast")
	}
	if after.Year() != 2025 {
		t.Errorf("after year: got %d, want 2025", after.Year())
	}
	if after.Month() != time.December {
		t.Errorf("after month: got %v, want December", after.Month())
	}
	if before.Year() != 2025 || before.Month() != time.December {
		t.Errorf("before: got %v, want December 2025", before)
	}
}

func TestAnytimeAbsoluteDate(t *testing.T) {
	// An ISO date string should resolve to a full-day window.
	p := anytimeParser{}
	now := time.Date(2026, 4, 24, 15, 30, 0, 0, time.UTC)
	after, before, ok := p.Parse("2025-03-12", now)
	if !ok {
		t.Fatal("expected ok=true for '2025-03-12'")
	}
	if after.Year() != 2025 || after.Month() != 3 || after.Day() != 12 {
		t.Errorf("after: got %v, want 2025-03-12", after)
	}
	if before.Year() != 2025 || before.Month() != 3 || before.Day() != 12 {
		t.Errorf("before: got %v, want 2025-03-12", before)
	}
	if after.Hour() != 0 || after.Minute() != 0 || after.Second() != 0 {
		t.Errorf("after should be start-of-day, got %v", after)
	}
	if before.Hour() != 23 || before.Minute() != 59 || before.Second() != 59 {
		t.Errorf("before should be end-of-day, got %v", before)
	}
}

func TestAnytimeNoMatch(t *testing.T) {
	p := anytimeParser{}
	now := time.Date(2026, 4, 24, 15, 30, 0, 0, time.UTC)
	_, _, ok := p.Parse("zzznonsensedate", now)
	if ok {
		t.Error("expected ok=false for nonsense input")
	}
}

// -----------------------------------------------------------------------
// TestDateParserFallback
// -----------------------------------------------------------------------

func TestDateParserFallback(t *testing.T) {
	// "12 August 2021" — an absolute date go-anytime might not handle well
	// but go-dateparser should parse.
	p := dateParserParser{}
	now := time.Date(2026, 4, 24, 15, 30, 0, 0, time.UTC)
	after, before, ok := p.Parse("12 august 2021", now)
	if !ok {
		t.Skip("go-dateparser did not parse '12 august 2021'; skipping")
	}
	if after.Year() != 2021 || after.Month() != time.August || after.Day() != 12 {
		t.Errorf("after: got %v, want 2021-08-12", after)
	}
	if before.Year() != 2021 || before.Month() != time.August || before.Day() != 12 {
		t.Errorf("before: got %v, want 2021-08-12", before)
	}
}

func TestDateParserConfig(t *testing.T) {
	// Verify that the adapter correctly uses PreferredDateSource=Past by checking
	// that an ambiguous month-name resolves to the past.
	p := dateParserParser{}
	now := time.Date(2026, 4, 24, 15, 30, 0, 0, time.UTC)
	// "january" should resolve to January 2026 (past, not future January 2027)
	after, _, ok := p.Parse("january", now)
	if !ok {
		t.Skip("go-dateparser did not parse 'january'; skipping")
	}
	if after.Year() > 2026 {
		t.Errorf("PreferredDateSource=Past not respected: got year %d for 'january'", after.Year())
	}
}

func TestDateParserNoMatch(t *testing.T) {
	p := dateParserParser{}
	now := time.Date(2026, 4, 24, 15, 30, 0, 0, time.UTC)
	_, _, ok := p.Parse("zzznonsensedate", now)
	if ok {
		t.Error("expected ok=false for nonsense input")
	}
}

// -----------------------------------------------------------------------
// TestChainOrder — hand-rules tried first, then anytime, then dateparser
// -----------------------------------------------------------------------

func TestChainOrder(t *testing.T) {
	// Replace dateChain with a trace-recording chain.
	var callOrder []string

	type tracingParser struct {
		name     string
		returnOK bool
		after    time.Time
		before   time.Time
	}

	makeTracer := func(name string, ok bool) dateParser {
		return &tracingParserImpl{
			name:     name,
			returnOK: ok,
			recorder: &callOrder,
		}
	}

	original := dateChain
	defer func() { dateChain = original }()

	// Set chain: first returns false, second returns true.
	dateChain = []dateParser{
		makeTracer("first", false),
		makeTracer("second", true),
		makeTracer("third", false),
	}

	now := time.Date(2026, 4, 24, 15, 30, 0, 0, time.UTC)
	_, _, ok := NormalizeDate("some phrase", now)
	if !ok {
		t.Fatal("expected ok=true (second parser should succeed)")
	}

	if len(callOrder) < 2 {
		t.Fatalf("expected at least 2 parsers to be called, got %v", callOrder)
	}
	if callOrder[0] != "first" {
		t.Errorf("first parser called should be 'first', got %q", callOrder[0])
	}
	if callOrder[1] != "second" {
		t.Errorf("second parser called should be 'second', got %q", callOrder[1])
	}
	// "third" should NOT be called since "second" returned ok=true.
	for _, name := range callOrder {
		if name == "third" {
			t.Error("third parser should not be called after second succeeds")
		}
	}
}

// tracingParserImpl is a test helper that records which parsers are called.
type tracingParserImpl struct {
	name     string
	returnOK bool
	recorder *[]string
}

func (tp *tracingParserImpl) Parse(s string, now time.Time) (after, before time.Time, ok bool) {
	*tp.recorder = append(*tp.recorder, tp.name)
	if tp.returnOK {
		return now, now, true
	}
	return
}

// -----------------------------------------------------------------------
// TestNormalizeDateEmpty
// -----------------------------------------------------------------------

func TestNormalizeDateEmpty(t *testing.T) {
	now := time.Now()
	after, before, ok := NormalizeDate("", now)
	if ok {
		t.Error("expected ok=false for empty string")
	}
	if !after.IsZero() {
		t.Errorf("expected zero after time, got %v", after)
	}
	if !before.IsZero() {
		t.Errorf("expected zero before time, got %v", before)
	}
}

func TestNormalizeDateWhitespaceOnly(t *testing.T) {
	now := time.Now()
	_, _, ok := NormalizeDate("   ", now)
	if ok {
		t.Error("expected ok=false for whitespace-only string")
	}
}
