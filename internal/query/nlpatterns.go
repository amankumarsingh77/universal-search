package query

import (
	"strings"
	"time"
)

// nlKindPatterns maps natural language file type terms to file_type values.
var nlKindPatterns = map[string]string{
	"photos":      "image",
	"photo":       "image",
	"pictures":    "image",
	"picture":     "image",
	"images":      "image",
	"image":       "image",
	"screenshots": "image",
	"screenshot":  "image",
	"videos":      "video",
	"video":       "video",
	"movies":      "video",
	"movie":       "video",
	"audios":      "audio",
	"audio":       "audio",
	"music":       "audio",
	"sound":       "audio",
	"documents":   "document",
	"document":    "document",
	"docs":        "document",
	"doc":         "document",
	"pdfs":        "document",
	"pdf":         "document",
	"texts":       "text",
	"text":        "text",
	"codes":       "text",
	"code":        "text",
	"scripts":     "text",
	"script":      "text",
}

// temporalPatternsSorted lists temporal patterns in descending length order
// for longest-match-first scanning. "before", "after", and "since" are
// intentionally absent — they only make sense paired with a following date,
// which is handled by tryTemporalAfterKind, not as standalone phrases.
var temporalPatternsSorted = []string{
	"last month", "this month", "next month",
	"last week", "this week", "next week",
	"last year", "this year", "next year",
	"today", "yesterday",
	"past", "recent", "older", "newer",
}

// nlFillerWords are common determiners/articles that can precede a kind word
// in a simple kind-only pattern (e.g., "my photos", "the documents").
var nlFillerWords = map[string]bool{
	"my": true, "the": true, "all": true, "a": true, "an": true,
	"some": true, "any": true, "our": true, "these": true, "those": true,
}

// parseNaturalLanguage attempts to parse common natural language patterns.
// It finds a kind word and optionally a temporal clue, but only when the
// pattern is unambiguous — kind words followed by temporal connectors, or
// standalone kind words with only filler words around them.
//
// If the input contains structured operator syntax (kind:, ext:, etc.),
// quoted phrases, or negation, this function returns false so the grammar
// parser can handle it.
func parseNaturalLanguage(input string, now time.Time) (FilterSpec, string, bool) {
	if hasStructuredSyntax(input) {
		return FilterSpec{}, "", false
	}

	lower := strings.ToLower(input)
	words := strings.Fields(lower)

	kindIdx, kindMapped := findKindWord(words)
	if kindIdx == -1 {
		return FilterSpec{}, "", false
	}

	remaining := words[kindIdx+1:]

	// Pattern 1: Kind + temporal context (e.g., "photos from last week",
	// "my videos yesterday", "documents on march 12th", "videos before 26th april").
	if len(remaining) > 0 {
		clauses, consumed, ok := tryTemporalAfterKind(remaining, now)
		if ok {
			excludeWords := []string{words[kindIdx]}
			excludeWords = append(excludeWords, consumed...)

			spec := FilterSpec{Source: SourceGrammar}
			spec.Must = append(spec.Must, Clause{
				Field: FieldFileType,
				Op:    OpEq,
				Value: kindMapped,
			})
			spec.Must = append(spec.Must, clauses...)

			semantic := extractSemanticText(input, excludeWords)
			return spec, semantic, true
		}
	}

	// Pattern 2: Standalone kind with only filler words (e.g., "photos",
	// "my photos", "all documents"). Do not match if there are content
	// words before or after the kind word.
	for i := 0; i < kindIdx; i++ {
		if !nlFillerWords[words[i]] {
			return FilterSpec{}, "", false
		}
	}
	for _, w := range remaining {
		if !nlFillerWords[w] {
			return FilterSpec{}, "", false
		}
	}

	excludeWords := []string{words[kindIdx]}
	for i := 0; i < kindIdx; i++ {
		excludeWords = append(excludeWords, words[i])
	}
	excludeWords = append(excludeWords, remaining...)

	spec := FilterSpec{Source: SourceGrammar}
	spec.Must = append(spec.Must, Clause{
		Field: FieldFileType,
		Op:    OpEq,
		Value: kindMapped,
	})

	semantic := extractSemanticText(input, excludeWords)
	return spec, semantic, true
}

// hasStructuredSyntax returns true if the input contains structured query syntax
// that should be handled by the grammar parser instead of NL pattern matching.
func hasStructuredSyntax(input string) bool {
	if strings.Contains(input, `"`) {
		return true
	}
	words := strings.Fields(input)
	for _, w := range words {
		lower := strings.ToLower(w)
		colonIdx := strings.IndexByte(lower, ':')
		if colonIdx > 0 {
			prefix := lower[:colonIdx]
			if recognizedOps[prefix] {
				return true
			}
		}
		if len(w) > 1 && w[0] == '-' {
			return true
		}
	}
	return false
}

// findKindWord returns the index and mapped value of the first NL kind word
// in the input. Returns (-1, "") if no kind word is found.
func findKindWord(words []string) (int, string) {
	for i, w := range words {
		if kind, ok := nlKindPatterns[w]; ok {
			return i, kind
		}
	}
	return -1, ""
}

// tryTemporalAfterKind looks for temporal/date patterns in the words following
// a kind word. It tries in order:
//  1. "from" + temporal keyword (e.g., "from last week")
//  2. "on" + date that NormalizeDate can parse (e.g., "on march 12th")
//  3. directional keyword + date (e.g., "before 26th april", "after march 1",
//     "since last month") emitting a one-sided modified_at clause
//  4. bare temporal keyword (e.g., "today", "last week")
//
// Returns (clauses, consumedWords, ok). Clauses are ready to be appended to
// the spec's Must slice.
func tryTemporalAfterKind(words []string, now time.Time) (clauses []Clause, consumed []string, ok bool) {
	if len(words) == 0 {
		return nil, nil, false
	}

	// Pattern 1: "from" + temporal keyword.
	if words[0] == "from" && len(words) >= 2 {
		rest := strings.Join(words[1:], " ")
		if tp := matchTemporalPattern(rest); tp != "" {
			tpWords := strings.Fields(tp)
			cw := make([]string, 0, 1+len(tpWords))
			cw = append(cw, "from")
			cw = append(cw, tpWords...)
			return rangeClauses(tp, now), cw, true
		}
	}

	// Pattern 2: "on" + date (only if NormalizeDate succeeds).
	if words[0] == "on" && len(words) >= 2 {
		datePart := strings.Join(words[1:], " ")
		if afterT, beforeT, dateOk := NormalizeDate(datePart, now); dateOk {
			cw := make([]string, len(words))
			copy(cw, words)
			cs := []Clause{
				{Field: FieldModifiedAt, Op: OpGte, Value: afterT.Unix()},
				{Field: FieldModifiedAt, Op: OpLte, Value: beforeT.Unix()},
			}
			return cs, cw, true
		}
	}

	// Pattern 3: directional keyword ("before"/"after"/"since") + date.
	// Greedy descending-length scan over words[1:] so trailing tokens like
	// "by aman" stay in the semantic text.
	if isDirectionalKeyword(words[0]) && len(words) >= 2 {
		for length := len(words) - 1; length >= 1; length-- {
			datePart := strings.Join(words[1:1+length], " ")
			afterT, _, dateOk := NormalizeDate(datePart, now)
			if !dateOk {
				continue
			}
			cw := make([]string, 0, 1+length)
			cw = append(cw, words[0])
			cw = append(cw, words[1:1+length]...)
			var op Op
			if words[0] == "before" {
				op = OpLt
			} else {
				op = OpGte
			}
			return []Clause{{Field: FieldModifiedAt, Op: op, Value: afterT.Unix()}}, cw, true
		}
		// No date matched after the keyword — decline so callers can fall through.
		return nil, nil, false
	}

	// Pattern 4: bare temporal keyword.
	rest := strings.Join(words, " ")
	if tp := matchTemporalPattern(rest); tp != "" {
		return rangeClauses(tp, now), strings.Fields(tp), true
	}

	return nil, nil, false
}

// isDirectionalKeyword reports whether w opens a one-sided date phrase.
func isDirectionalKeyword(w string) bool {
	return w == "before" || w == "after" || w == "since"
}

// rangeClauses normalizes a temporal phrase into modified_at range clauses,
// matching the bounds rule from addTemporalClauses (open upper bound when
// NormalizeDate returns now as the "before" boundary).
func rangeClauses(timePeriod string, now time.Time) []Clause {
	afterT, beforeT, ok := NormalizeDate(timePeriod, now)
	if !ok {
		return nil
	}
	out := []Clause{{Field: FieldModifiedAt, Op: OpGte, Value: afterT.Unix()}}
	if !beforeT.Equal(now) {
		out = append(out, Clause{Field: FieldModifiedAt, Op: OpLte, Value: beforeT.Unix()})
	}
	return out
}

// matchTemporalPattern finds the longest temporal pattern at the start of s.
// Returns the matched pattern or "" if no match.
func matchTemporalPattern(s string) string {
	for _, tp := range temporalPatternsSorted {
		if s == tp {
			return tp
		}
		if strings.HasPrefix(s, tp+" ") {
			return tp
		}
	}
	return ""
}

// extractSemanticText removes the given exclude words (compared case-insensitively)
// from the original input, preserving the case of remaining words.
// Returns the semantic query with matched words removed, or "" if nothing remains.
func extractSemanticText(original string, excludeWords []string) string {
	lowerExclude := make(map[string]bool, len(excludeWords))
	for _, w := range excludeWords {
		lowerExclude[w] = true
	}

	var result []string
	for _, word := range strings.Fields(original) {
		if !lowerExclude[strings.ToLower(word)] {
			result = append(result, word)
		}
	}

	if len(result) == 0 {
		return ""
	}
	return strings.Join(result, " ")
}
