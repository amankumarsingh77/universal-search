package embedder

import (
	"testing"
	"time"
)

// TestSetRatePerMinute_AppliesNewLimit verifies that SetRatePerMinute updates
// the maximum requests/minute and the new value gates subsequent Allow calls.
func TestSetRatePerMinute_AppliesNewLimit(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)
	if !rl.Allow() {
		t.Fatalf("expected first Allow to succeed")
	}
	if !rl.Allow() {
		t.Fatalf("expected second Allow to succeed")
	}
	if rl.Allow() {
		t.Fatalf("expected third Allow to be denied at limit 2")
	}
	rl.SetRatePerMinute(5)
	for i := range 3 {
		if !rl.Allow() {
			t.Fatalf("expected Allow #%d to succeed after raising limit to 5", i+1)
		}
	}
	if rl.Allow() {
		t.Fatalf("expected Allow to be denied after using all 5 tokens")
	}
}

// TestSetRatePerMinute_StatsReflectNewMax verifies that Stats reports the new
// configured max after SetRatePerMinute.
func TestSetRatePerMinute_StatsReflectNewMax(t *testing.T) {
	rl := NewRateLimiter(10, time.Minute)
	rl.SetRatePerMinute(42)
	_, max := rl.Stats()
	if max != 42 {
		t.Fatalf("Stats max = %d, want 42", max)
	}
}

// TestSetRatePerMinute_IgnoresNonPositive verifies that SetRatePerMinute is a
// no-op for zero or negative values, preserving the existing limit.
func TestSetRatePerMinute_IgnoresNonPositive(t *testing.T) {
	rl := NewRateLimiter(10, time.Minute)
	rl.SetRatePerMinute(0)
	rl.SetRatePerMinute(-5)
	_, max := rl.Stats()
	if max != 10 {
		t.Fatalf("Stats max = %d, want 10 (non-positive values must be ignored)", max)
	}
}
