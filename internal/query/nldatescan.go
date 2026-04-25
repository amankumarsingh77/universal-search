package query

import (
	"regexp"
	"strings"
	"time"
)

// scanForEmbeddedDate scans the tokens that make up the query's semantic/free
// text, looking for the longest contiguous substring that NormalizeDate can
// resolve. On a hit, it returns the resolved window, the matched substring (so
// callers can remove it from the semantic text), and true.
//
// The scan prefers longer matches (more tokens) over shorter, and preserves
// existing behavior for queries that parseNaturalLanguage already handled.
func scanForEmbeddedDate(text string, now time.Time) (after, before time.Time, matched string, ok bool) {
	if text == "" {
		return
	}

	// Drop leading filler words that are never part of a date match.
	lower := strings.ToLower(text)
	tokens := strings.Fields(lower)
	if len(tokens) == 0 {
		return
	}

	// Token-window scan: try every contiguous [i:j] window from longest to
	// shortest. First match wins. O(n^2) in token count but n is tiny.
	maxLen := len(tokens)
	if maxLen > 8 {
		maxLen = 8 // cap: no date phrase in our corpus is longer than ~7 tokens
	}

	for length := maxLen; length >= 1; length-- {
		for start := 0; start+length <= len(tokens); start++ {
			rawPhrase := strings.Join(tokens[start:start+length], " ")
			phrase := trimDateEdgeNoise(rawPhrase)
			if phrase == "" {
				continue
			}
			// Reject phrases that start or end with a filler connective token —
			// those indicate we're slurping a boundary word into the date.
			if hasBoundaryFiller(phrase) {
				continue
			}
			// Skip phrases that are purely common-word noise with no date hint.
			if !looksLikeDate(phrase) {
				continue
			}
			if a, b, k := NormalizeDate(phrase, now); k {
				return a, b, phrase, true
			}
		}
	}
	return
}

// hasBoundaryFiller returns true if phrase starts or ends with a word that can
// never be part of a date expression — indicating the window grabbed a
// connective. These phrases should be rejected so the scanner falls back to a
// shorter, cleaner window.
var boundaryFillers = map[string]bool{
	"from": true, "in": true, "on": true, "at": true, "of": true,
	"the": true, "for": true, "with": true, "by": true, "to": true,
	"and": true, "or": true, "is": true, "was": true, "are": true,
	"be": true, "do": true, "not": true, "my": true, "me": true,
	"all": true, "any": true, "some": true, "no": true,
	"folder": true, "file": true, "files": true,
	"within": true, "about": true, "around": true, "just": true,
	"created": true, "modified": true, "uploaded": true, "dated": true,
}

func hasBoundaryFiller(phrase string) bool {
	words := strings.Fields(phrase)
	if len(words) == 0 {
		return false
	}
	if boundaryFillers[words[0]] {
		return true
	}
	if boundaryFillers[words[len(words)-1]] {
		return true
	}
	return false
}

// trimDateEdgeNoise strips leading/trailing filler words that can't legitimately
// be part of a date expression (prepositions we consumed blindly, possessives).
func trimDateEdgeNoise(s string) string {
	// Trailing punctuation.
	s = strings.TrimRight(s, ".,;:!?")
	// Leading connective words we can safely drop.
	for _, p := range []string{"from ", "on ", "at ", "of ", "in ", "during ", "dated ", "created ", "modified ", "created in ", "created on "} {
		s = strings.TrimPrefix(s, p)
	}
	// Trailing 's from possessives ("last week's" → "last week").
	s = strings.TrimSuffix(s, "'s")
	s = strings.TrimSuffix(s, "s'")
	return strings.TrimSpace(s)
}

// dateHintRe matches any phrase that contains a plausible date token. Used as a
// cheap filter before the expensive NormalizeDate chain.
var dateHintRe = regexp.MustCompile(
	`(?i)(^|\W)(` +
		`today|yesterday|tomorrow|tonight|recently|` +
		`this (?:morning|afternoon|evening|week|month|year|quarter)|` +
		`last (?:night|week|month|year|quarter|summer|winter|spring|fall|autumn|few|couple|\d)|` +
		`next (?:week|month|year|\d)|` +
		`next\s+\d+\s+(?:day|week|month|year)|` +
		`past|since|recent|recently|` +
		`a (?:few|couple|week|month|day)|` +
		`(?:early|late|earlier|later) (?:this|last)|` +
		`(?:in |within )?(?:the )?(?:last|past)\s+\d+|` +
		`\d+\s+(?:day|week|month|year|hour)s?\s+ago|` +
		`ago|back|while ago|` +
		`current (?:week|month|year|quarter)|` +
		`q[1-4] \d{4}|` +
		`\d{4}-\d{1,2}(?:-\d{1,2})?|` +
		`\d{1,2}[-/]\d{1,2}[-/]\d{2,4}|` +
		`(?:january|february|march|april|may|june|july|august|september|october|november|december|jan|feb|mar|apr|jun|jul|aug|sep|sept|oct|nov|dec)\b|` +
		`^\d{4}$|` +
		`\b\d{4}\b` +
		`)(\W|$)`,
)

func looksLikeDate(phrase string) bool {
	return dateHintRe.MatchString(phrase)
}
