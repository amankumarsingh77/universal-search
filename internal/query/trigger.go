package query

import (
	"regexp"
	"strings"
)

var (
	temporalRe  = regexp.MustCompile(`\b(yesterday|today|last|past|recent|older|newer|since|before|after)\b`)
	negationRe  = regexp.MustCompile(`\b(not|no|without|except|exclude)\b`)
	fileTypeRe  = regexp.MustCompile(`\b(photos?|pictures?|screenshots?|videos?|docs?|documents?|code|scripts?)\b`)
)

// ShouldInvokeLLM returns true if the residual free-text warrants LLM parsing.
// Hard cap: input > 500 chars → always false.
func ShouldInvokeLLM(residual string) bool {
	if len(residual) > 500 {
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
	// Token count > 6
	tokens := strings.Fields(residual)
	if len(tokens) > 6 {
		return true
	}
	return false
}
