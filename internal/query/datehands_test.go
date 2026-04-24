package query

import (
	"testing"
	"time"
)

// testNow is a fixed reference time for all hand-rules tests.
// 2026-04-24 15:30:00 UTC (a Friday, Q2)
var testNow = time.Date(2026, 4, 24, 15, 30, 0, 0, time.UTC)

// -----------------------------------------------------------------------
// TestHandRulesExistingPhrases — existing phrases must match old behavior.
// -----------------------------------------------------------------------

func TestHandRulesExistingPhrases(t *testing.T) {
	h := handRulesParser{}

	t.Run("today", func(t *testing.T) {
		after, before, ok := h.Parse("today", testNow)
		if !ok {
			t.Fatal("expected ok=true")
		}
		wantAfter := time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC)
		if !after.Equal(wantAfter) {
			t.Errorf("after: got %v, want %v", after, wantAfter)
		}
		// before should equal now
		if !before.Equal(testNow) {
			t.Errorf("before: got %v, want %v (now)", before, testNow)
		}
	})

	t.Run("yesterday", func(t *testing.T) {
		after, before, ok := h.Parse("yesterday", testNow)
		if !ok {
			t.Fatal("expected ok=true")
		}
		wantAfter := time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)
		wantBefore := time.Date(2026, 4, 23, 23, 59, 59, 0, time.UTC)
		if !after.Equal(wantAfter) {
			t.Errorf("after: got %v, want %v", after, wantAfter)
		}
		if !before.Equal(wantBefore) {
			t.Errorf("before: got %v, want %v", before, wantBefore)
		}
	})

	t.Run("last week", func(t *testing.T) {
		after, before, ok := h.Parse("last week", testNow)
		if !ok {
			t.Fatal("expected ok=true")
		}
		wantAfter := time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC) // 7 days ago at 00:00
		if !after.Equal(wantAfter) {
			t.Errorf("after: got %v, want %v", after, wantAfter)
		}
		if !before.Equal(testNow) {
			t.Errorf("before: got %v, want now %v", before, testNow)
		}
	})

	t.Run("last month", func(t *testing.T) {
		after, before, ok := h.Parse("last month", testNow)
		if !ok {
			t.Fatal("expected ok=true")
		}
		// -1 month from Apr 24 = Mar 24
		wantAfter := time.Date(2026, 3, 24, 0, 0, 0, 0, time.UTC)
		if !after.Equal(wantAfter) {
			t.Errorf("after: got %v, want %v", after, wantAfter)
		}
		if !before.Equal(testNow) {
			t.Errorf("before: got %v, want now %v", before, testNow)
		}
	})

	t.Run("last year", func(t *testing.T) {
		after, before, ok := h.Parse("last year", testNow)
		if !ok {
			t.Fatal("expected ok=true")
		}
		wantAfter := time.Date(2025, 4, 24, 0, 0, 0, 0, time.UTC)
		if !after.Equal(wantAfter) {
			t.Errorf("after: got %v, want %v", after, wantAfter)
		}
		if !before.Equal(testNow) {
			t.Errorf("before: got %v, want now %v", before, testNow)
		}
	})
}

// -----------------------------------------------------------------------
// TestHandRulesThisMorning / Afternoon / Evening
// -----------------------------------------------------------------------

func TestHandRulesThisMorning(t *testing.T) {
	h := handRulesParser{}
	after, before, ok := h.Parse("this morning", testNow)
	if !ok {
		t.Fatal("expected ok=true")
	}
	wantAfter := time.Date(2026, 4, 24, 6, 0, 0, 0, time.UTC)
	wantBefore := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	if !after.Equal(wantAfter) {
		t.Errorf("after: got %v, want %v", after, wantAfter)
	}
	if !before.Equal(wantBefore) {
		t.Errorf("before: got %v, want %v", before, wantBefore)
	}
}

func TestHandRulesThisAfternoon(t *testing.T) {
	h := handRulesParser{}
	after, before, ok := h.Parse("this afternoon", testNow)
	if !ok {
		t.Fatal("expected ok=true")
	}
	wantAfter := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	wantBefore := time.Date(2026, 4, 24, 18, 0, 0, 0, time.UTC)
	if !after.Equal(wantAfter) {
		t.Errorf("after: got %v, want %v", after, wantAfter)
	}
	if !before.Equal(wantBefore) {
		t.Errorf("before: got %v, want %v", before, wantBefore)
	}
}

func TestHandRulesThisEvening(t *testing.T) {
	h := handRulesParser{}
	after, before, ok := h.Parse("this evening", testNow)
	if !ok {
		t.Fatal("expected ok=true")
	}
	wantAfter := time.Date(2026, 4, 24, 18, 0, 0, 0, time.UTC)
	wantBefore := time.Date(2026, 4, 24, 23, 59, 59, 0, time.UTC)
	if !after.Equal(wantAfter) {
		t.Errorf("after: got %v, want %v", after, wantAfter)
	}
	if !before.Equal(wantBefore) {
		t.Errorf("before: got %v, want %v", before, wantBefore)
	}
}

// -----------------------------------------------------------------------
// TestHandRulesPastCoupleOfMonths / PastFewMonths
// -----------------------------------------------------------------------

func TestHandRulesPastCoupleOfMonths(t *testing.T) {
	h := handRulesParser{}

	for _, phrase := range []string{"past couple of months", "past couple months"} {
		t.Run(phrase, func(t *testing.T) {
			after, before, ok := h.Parse(phrase, testNow)
			if !ok {
				t.Fatalf("expected ok=true for %q", phrase)
			}
			// -2 months from Apr 24 = Feb 24 at 00:00
			wantAfter := time.Date(2026, 2, 24, 0, 0, 0, 0, time.UTC)
			if !after.Equal(wantAfter) {
				t.Errorf("after: got %v, want %v", after, wantAfter)
			}
			if !before.Equal(testNow) {
				t.Errorf("before: got %v, want now %v", before, testNow)
			}
		})
	}
}

func TestHandRulesPastFewMonths(t *testing.T) {
	h := handRulesParser{}
	after, before, ok := h.Parse("past few months", testNow)
	if !ok {
		t.Fatal("expected ok=true")
	}
	// -3 months from Apr 24 = Jan 24 at 00:00
	wantAfter := time.Date(2026, 1, 24, 0, 0, 0, 0, time.UTC)
	if !after.Equal(wantAfter) {
		t.Errorf("after: got %v, want %v", after, wantAfter)
	}
	if !before.Equal(testNow) {
		t.Errorf("before: got %v, want now %v", before, testNow)
	}
}

// -----------------------------------------------------------------------
// TestHandRulesLastQuarter / EndOfLastQuarter
// -----------------------------------------------------------------------

func TestHandRulesLastQuarter(t *testing.T) {
	h := handRulesParser{}
	// testNow is April 2026 → Q2 → last quarter = Q1 (Jan–Mar)
	after, before, ok := h.Parse("last quarter", testNow)
	if !ok {
		t.Fatal("expected ok=true")
	}
	// Q1 2026: Jan 1 00:00 to Mar 31 23:59:59
	wantAfter := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	wantBefore := time.Date(2026, 3, 31, 23, 59, 59, 0, time.UTC)
	if !after.Equal(wantAfter) {
		t.Errorf("after: got %v, want %v", after, wantAfter)
	}
	if !before.Equal(wantBefore) {
		t.Errorf("before: got %v, want %v", before, wantBefore)
	}
}

func TestHandRulesLastQuarterWrapYear(t *testing.T) {
	h := handRulesParser{}
	// Jan 2026 → Q1 → last quarter = Q4 of 2025 (Oct–Dec)
	nowQ1 := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	after, before, ok := h.Parse("last quarter", nowQ1)
	if !ok {
		t.Fatal("expected ok=true")
	}
	wantAfter := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)
	wantBefore := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
	if !after.Equal(wantAfter) {
		t.Errorf("after: got %v, want %v", after, wantAfter)
	}
	if !before.Equal(wantBefore) {
		t.Errorf("before: got %v, want %v", before, wantBefore)
	}
}

func TestHandRulesEndOfLastQuarter(t *testing.T) {
	h := handRulesParser{}
	// testNow is April 2026 → Q2 → last quarter = Q1 → end = Mar 31
	after, before, ok := h.Parse("end of last quarter", testNow)
	if !ok {
		t.Fatal("expected ok=true")
	}
	wantAfter := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	wantBefore := time.Date(2026, 3, 31, 23, 59, 59, 0, time.UTC)
	if !after.Equal(wantAfter) {
		t.Errorf("after: got %v, want %v", after, wantAfter)
	}
	if !before.Equal(wantBefore) {
		t.Errorf("before: got %v, want %v", before, wantBefore)
	}
}

// -----------------------------------------------------------------------
// TestHandRulesPastN — regex-driven past N units
// -----------------------------------------------------------------------

func TestHandRulesPastN(t *testing.T) {
	h := handRulesParser{}

	cases := []struct {
		phrase    string
		wantAfter time.Time
	}{
		{"past 3 days", time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)},
		{"past 2 weeks", time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)},
		{"past 1 month", time.Date(2026, 3, 24, 0, 0, 0, 0, time.UTC)},
		{"past 5 years", time.Date(2021, 4, 24, 0, 0, 0, 0, time.UTC)},
	}

	for _, tc := range cases {
		t.Run(tc.phrase, func(t *testing.T) {
			after, before, ok := h.Parse(tc.phrase, testNow)
			if !ok {
				t.Fatalf("expected ok=true for %q", tc.phrase)
			}
			if !after.Equal(tc.wantAfter) {
				t.Errorf("after: got %v, want %v", after, tc.wantAfter)
			}
			if !before.Equal(testNow) {
				t.Errorf("before: got %v, want now %v", before, testNow)
			}
		})
	}
}

// -----------------------------------------------------------------------
// TestHandRulesYearOnly
// -----------------------------------------------------------------------

func TestHandRulesYearOnly(t *testing.T) {
	h := handRulesParser{}
	after, before, ok := h.Parse("2025", testNow)
	if !ok {
		t.Fatal("expected ok=true for year-only '2025'")
	}
	wantAfter := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	wantBefore := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
	if !after.Equal(wantAfter) {
		t.Errorf("after: got %v, want %v", after, wantAfter)
	}
	if !before.Equal(wantBefore) {
		t.Errorf("before: got %v, want %v", before, wantBefore)
	}
}

// -----------------------------------------------------------------------
// TestHandRulesNoMatch
// -----------------------------------------------------------------------

func TestHandRulesNoMatch(t *testing.T) {
	h := handRulesParser{}
	_, _, ok := h.Parse("december", testNow)
	if ok {
		t.Error("expected ok=false for 'december' (should fall through to anytime)")
	}
	_, _, ok = h.Parse("", testNow)
	if ok {
		t.Error("expected ok=false for empty string")
	}
}
