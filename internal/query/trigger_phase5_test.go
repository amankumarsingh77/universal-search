package query

import "testing"

// REF-044: a Trigger configured with MinTokens=100 does NOT fire on a 10-token
// query that would otherwise fire with the default threshold of 6.
func TestTrigger_MinTokensFromConfig(t *testing.T) {
	q := "alpha beta gamma delta epsilon zeta eta theta iota kappa" // 10 tokens

	if !DefaultTrigger().ShouldInvokeLLM(q) {
		t.Fatal("default trigger should fire on 10-token query")
	}
	large := Trigger{MinTokens: 100, MaxChars: 500}
	if large.ShouldInvokeLLM(q) {
		t.Fatal("MinTokens=100 trigger should NOT fire on 10-token query")
	}
}

// MaxChars cap is also honoured.
func TestTrigger_MaxCharsFromConfig(t *testing.T) {
	q := "photos yesterday"
	tight := Trigger{MinTokens: 6, MaxChars: 5}
	if tight.ShouldInvokeLLM(q) {
		t.Fatal("query longer than MaxChars should not fire")
	}
}
