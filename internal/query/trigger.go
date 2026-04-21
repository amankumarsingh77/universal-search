package query

import (
	"regexp"
	"strings"
)

var (
	temporalRe = regexp.MustCompile(`\b(yesterday|today|last|past|recent|older|newer|since|before|after)\b`)
	negationRe = regexp.MustCompile(`\b(not|no|without|except|exclude)\b`)
	fileTypeRe = regexp.MustCompile(`\b(photos?|pictures?|screenshots?|videos?|docs?|documents?|code|scripts?)\b`)
)

// Trigger decides whether to route a query to the LLM parser.
type Trigger struct {
	MinTokens int
	MaxChars  int
}

// DefaultTrigger returns the historical thresholds (6 tokens, 500 chars).
func DefaultTrigger() Trigger {
	return Trigger{MinTokens: 6, MaxChars: 500}
}

// ShouldInvokeLLM returns true if the residual free-text warrants LLM parsing.
// Hard cap: input > t.MaxChars → always false.
func (t Trigger) ShouldInvokeLLM(residual string) bool {
	maxChars := t.MaxChars
	if maxChars <= 0 {
		maxChars = DefaultTrigger().MaxChars
	}
	minTokens := t.MinTokens
	if minTokens <= 0 {
		minTokens = DefaultTrigger().MinTokens
	}
	if len(residual) > maxChars {
		return false
	}
	lower := strings.ToLower(residual)
	if temporalRe.MatchString(lower) {
		return true
	}
	if negationRe.MatchString(lower) {
		return true
	}
	if fileTypeRe.MatchString(lower) {
		return true
	}
	// Token count > minTokens.
	tokens := strings.Fields(residual)
	if len(tokens) > minTokens {
		return true
	}
	// Structured-field detector: catches short queries with bare extensions,
	// size patterns, and root folder names that the existing checks miss.
	if DetectStructuredFields(residual).Any() {
		return true
	}
	return false
}

// ShouldInvokeLLM preserves the package-level API by delegating to
// DefaultTrigger; used by call sites that have not yet adopted the Trigger
// type.
func ShouldInvokeLLM(residual string) bool {
	return DefaultTrigger().ShouldInvokeLLM(residual)
}
