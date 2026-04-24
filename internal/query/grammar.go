package query

import (
	"strings"
	"time"
	"unicode"
)

// recognizedOps is the whitelist of operator keywords that may appear before ':'.
var recognizedOps = map[string]bool{
	"kind":   true,
	"ext":    true,
	"size":   true,
	"before": true,
	"after":  true,
	"since":  true,
	"in":     true,
	"path":   true,
}

// nowFunc returns the reference time used by the grammar's NL date resolution.
// Overridable from tests via setNowForTest. Production always uses time.Now().
var nowFunc = func() time.Time { return time.Now() }

// Parse converts a user query string into a FilterSpec.
// It never panics and returns a best-effort result on malformed input.
func Parse(input string) (spec FilterSpec) {
	defer func() {
		if r := recover(); r != nil {
			// Return whatever we have so far.
		}
		spec.Source = SourceGrammar
	}()

	now := nowFunc()
	var semanticParts []string

	// First, try to parse natural language patterns
	if nlSpec, nlSemantic, ok := parseNaturalLanguage(input, now); ok {
		// If we successfully parsed natural language patterns, merge with remaining semantic text
		spec = nlSpec
		if nlSemantic != "" {
			spec.SemanticQuery = nlSemantic
		}
		spec.Source = SourceGrammar
		return spec
	}

	runes := []rune(input)
	pos := 0
	n := len(runes)

	for pos < n {
		// Skip leading whitespace
		for pos < n && unicode.IsSpace(runes[pos]) {
			pos++
		}
		if pos >= n {
			break
		}

		// Quoted phrase
		if runes[pos] == '"' {
			pos++ // consume opening quote
			start := pos
			for pos < n && runes[pos] != '"' {
				pos++
			}
			phrase := string(runes[start:pos])
			if pos < n {
				pos++ // consume closing quote
			}
			semanticParts = append(semanticParts, phrase)
			continue
		}

		// Negation: -term or -op:value
		if runes[pos] == '-' && pos+1 < n && !unicode.IsSpace(runes[pos+1]) {
			pos++ // consume '-'
			start := pos
			tok := readToken(runes, &pos)
			if tok == "" {
				continue
			}
			// Check if this is -op:value (e.g. -kind:image → MustNot file_type)
			colonIdx := strings.IndexByte(tok, ':')
			if colonIdx > 0 {
				keyword := strings.ToLower(tok[:colonIdx])
				if recognizedOps[keyword] {
					value := tok[colonIdx+1:]
					if value == "" {
						for pos < n && unicode.IsSpace(runes[pos]) {
							pos++
						}
						value = readOpValue(runes, &pos, now)
					}
					if clauses, _, handled := parseOperator(keyword, value, now); handled {
						spec.MustNot = append(spec.MustNot, clauses...)
						continue
					}
				}
			}
			// Plain -term → MustNot path contains
			_ = start
			spec.MustNot = append(spec.MustNot, Clause{
				Field: FieldPath,
				Op:    OpContains,
				Value: tok,
			})
			continue
		}

		// Read a token (up to whitespace)
		start := pos
		tok := readToken(runes, &pos)
		if tok == "" {
			continue
		}

		// Check if this looks like operator:value
		colonIdx := strings.IndexByte(tok, ':')
		if colonIdx > 0 {
			keyword := strings.ToLower(tok[:colonIdx])
			if recognizedOps[keyword] {
				value := tok[colonIdx+1:]
				// If value is empty or operator value needs multi-word support,
				// collect the full value (may be quoted or multi-word for date ops).
				if value == "" {
					for pos < n && unicode.IsSpace(runes[pos]) {
						pos++
					}
					value = readOpValue(runes, &pos, now)
				}
				if clause, extra, handled := parseOperator(keyword, value, now); handled {
					spec.Must = append(spec.Must, clause...)
					if extra != "" {
						semanticParts = append(semanticParts, extra)
					}
					continue
				}
			}
			// Not a recognized operator → free text
			semanticParts = append(semanticParts, string(runes[start:pos]))
			continue
		}

		// Plain free text token
		semanticParts = append(semanticParts, tok)
	}

	semanticText := strings.TrimSpace(strings.Join(semanticParts, " "))

	// Post-pass: scan semantic text for an embedded date phrase. If one is
	// found AND the spec doesn't already have a modified_at clause (from an
	// explicit operator like before:/after:), emit it and strip the matched
	// phrase from the semantic text.
	if semanticText != "" && !specHasModifiedAt(spec) {
		if after, before, matched, ok := scanForEmbeddedDate(semanticText, now); ok {
			spec.Must = append(spec.Must,
				Clause{Field: FieldModifiedAt, Op: OpGte, Value: after.Unix()},
				Clause{Field: FieldModifiedAt, Op: OpLte, Value: before.Unix()},
			)
			semanticText = removeMatchedPhrase(semanticText, matched)
		}
	}

	spec.SemanticQuery = semanticText
	return spec
}

// specHasModifiedAt returns true iff spec.Must already contains a modified_at
// clause (from an explicit before:/after: operator or from parseNaturalLanguage).
func specHasModifiedAt(spec FilterSpec) bool {
	for _, c := range spec.Must {
		if c.Field == FieldModifiedAt {
			return true
		}
	}
	return false
}

// removeMatchedPhrase strips the first occurrence of the matched date phrase
// from the semantic text and also strips nearby filler connectives (from, on,
// at, in, of, during, dated, created, modified) that precede it, along with
// possessive "'s" suffixes.
func removeMatchedPhrase(text, phrase string) string {
	lowerText := strings.ToLower(text)
	idx := strings.Index(lowerText, phrase)
	if idx < 0 {
		return strings.TrimSpace(text)
	}
	before := text[:idx]
	after := text[idx+len(phrase):]

	// Strip one or more trailing filler connective words from `before`.
	trimmedBefore := strings.TrimRight(before, " ")
	// Try longest multi-word fillers first, then loop single-word ones.
	multiWord := []string{
		"created in the", "created in", "created on", "uploaded in", "uploaded on",
		"modified in the", "modified in", "modified on",
		"in the past", "in the last", "within the last", "within the past",
		"within the", "in the", "within",
	}
	singleWord := []string{
		"from", "on", "at", "during", "dated", "in", "of", "for", "about",
		"around", "just", "created", "modified", "uploaded",
	}
	for _, f := range multiWord {
		if strings.HasSuffix(strings.ToLower(trimmedBefore), " "+f) ||
			strings.EqualFold(trimmedBefore, f) {
			trimmedBefore = trimmedBefore[:len(trimmedBefore)-len(f)]
			trimmedBefore = strings.TrimRight(trimmedBefore, " ")
			break
		}
	}
	// Strip up to 3 more trailing single-word fillers.
	for i := 0; i < 3; i++ {
		stripped := false
		for _, filler := range singleWord {
			if strings.HasSuffix(strings.ToLower(trimmedBefore), " "+filler) ||
				strings.EqualFold(trimmedBefore, filler) {
				trimmedBefore = trimmedBefore[:len(trimmedBefore)-len(filler)]
				trimmedBefore = strings.TrimRight(trimmedBefore, " ")
				stripped = true
				break
			}
		}
		if !stripped {
			break
		}
	}

	// Strip a leading possessive "'s" from `after`.
	after = strings.TrimLeft(after, " ")
	if strings.HasPrefix(after, "'s ") || after == "'s" {
		after = strings.TrimPrefix(after, "'s")
		after = strings.TrimLeft(after, " ")
	}

	combined := strings.TrimSpace(trimmedBefore + " " + after)
	// Collapse double spaces.
	for strings.Contains(combined, "  ") {
		combined = strings.ReplaceAll(combined, "  ", " ")
	}
	return combined
}

// readToken reads runes until whitespace or end-of-input.
func readToken(runes []rune, pos *int) string {
	start := *pos
	for *pos < len(runes) && !unicode.IsSpace(runes[*pos]) {
		(*pos)++
	}
	return string(runes[start:*pos])
}

// twoWordDatePrefixes are the first words of recognized two-word relative date phrases.
var twoWordDatePrefixes = map[string]bool{
	"last": true,
	"past": true,
	"this": true,
	"next": true,
}

// readOpValue reads an operator value, supporting:
//   - Quoted strings: "last week"
//   - Two-word relative date phrases: last week, last month, past 3 days
//   - Single words otherwise
func readOpValue(runes []rune, pos *int, now time.Time) string {
	n := len(runes)
	if *pos >= n {
		return ""
	}

	// Quoted value
	if runes[*pos] == '"' {
		(*pos)++ // consume opening quote
		start := *pos
		for *pos < n && runes[*pos] != '"' {
			(*pos)++
		}
		val := string(runes[start:*pos])
		if *pos < n {
			(*pos)++ // consume closing quote
		}
		return val
	}

	// Read first word
	first := readToken(runes, pos)
	if first == "" {
		return ""
	}

	// Peek for a second word if the first is a known two-word date prefix
	lower := strings.ToLower(first)
	if twoWordDatePrefixes[lower] {
		// Save position, try to read second word
		savedPos := *pos
		for *pos < n && unicode.IsSpace(runes[*pos]) {
			(*pos)++
		}
		second := readToken(runes, pos)
		if second != "" {
			candidate := first + " " + second
			// Validate that the two-word phrase parses as a date
			if _, _, ok := NormalizeDate(candidate, now); ok {
				return candidate
			}
		}
		// Not a valid two-word date — restore position, return just first word
		*pos = savedPos
	}

	return first
}

// parseOperator processes a recognized keyword + value pair.
// Returns clauses, any residual semantic text, and whether it was handled.
func parseOperator(keyword, value string, now time.Time) (clauses []Clause, residual string, handled bool) {
	switch keyword {
	case "kind":
		return parseKind(value)
	case "ext":
		return parseExt(value)
	case "size":
		return parseSize(value)
	case "before":
		return parseDateOp("before", value, now)
	case "after", "since":
		return parseDateOp("after", value, now)
	case "path", "in":
		return parsePath(value)
	}
	return nil, "", false
}

func parseKind(value string) ([]Clause, string, bool) {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return nil, "", false
	}
	// Direct lookup
	if canonical, ok := KnownKindValues[lower]; ok {
		return []Clause{{Field: FieldFileType, Op: OpEq, Value: canonical}}, "", true
	}
	// Typo correction
	if canonical, ok := CorrectKind(lower); ok {
		return []Clause{{Field: FieldFileType, Op: OpEq, Value: canonical}}, "", true
	}
	// Unknown kind value → fall through as semantic
	return nil, "kind:" + value, false
}

func parseExt(value string) ([]Clause, string, bool) {
	if value == "" {
		return nil, "", false
	}
	parts := strings.Split(value, ",")
	exts := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Strip leading dot for typo correction lookup.
		bare := strings.TrimPrefix(p, ".")
		// Typo correction: try to find closest known extension.
		if corrected, ok := CorrectExtension(bare); ok {
			exts = append(exts, "."+corrected)
		} else {
			// Unknown extension — keep as-is with leading dot.
			if !strings.HasPrefix(p, ".") {
				p = "." + p
			}
			exts = append(exts, p)
		}
	}
	if len(exts) == 0 {
		return nil, "", false
	}
	return []Clause{{Field: FieldExtension, Op: OpInSet, Value: exts}}, "", true
}

func parseSize(value string) ([]Clause, string, bool) {
	op, bytes, ok := ParseSize(value)
	if !ok {
		return nil, "size:" + value, false
	}
	return []Clause{{Field: FieldSizeBytes, Op: op, Value: bytes}}, "", true
}

func parseDateOp(direction, value string, now time.Time) ([]Clause, string, bool) {
	if value == "" {
		return nil, "", false
	}
	afterT, _, ok := NormalizeDate(value, now)
	if !ok {
		return nil, direction + ":" + value, false
	}

	var clauses []Clause
	switch direction {
	case "before":
		// modified_at < start-of-day (afterT is the start-of-day boundary)
		clauses = append(clauses, Clause{Field: FieldModifiedAt, Op: OpLt, Value: afterT})
	case "after":
		// modified_at >= start-of-day (afterT is the start-of-day boundary, includes the full day)
		clauses = append(clauses, Clause{Field: FieldModifiedAt, Op: OpGte, Value: afterT})
	}
	return clauses, "", true
}

func parsePath(value string) ([]Clause, string, bool) {
	if value == "" {
		return nil, "", false
	}
	return []Clause{{Field: FieldPath, Op: OpContains, Value: value}}, "", true
}
