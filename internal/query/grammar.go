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

// nlKindPatterns maps natural language file type terms to file_type values.
var nlKindPatterns = map[string]string{
	"photos":     "image",
	"photo":      "image",
	"pictures":   "image",
	"picture":    "image",
	"images":     "image",
	"image":      "image",
	"screenshots": "image",
	"screenshot":  "image",
	"videos":     "video",
	"video":      "video",
	"movies":     "video",
	"movie":      "video",
	"audios":     "audio",
	"audio":      "audio",
	"music":      "audio",
	"sound":      "audio",
	"documents":  "document",
	"document":   "document",
	"docs":       "document",
	"doc":        "document",
	"pdfs":       "document",
	"pdf":        "document",
	"texts":      "text",
	"text":       "text",
	"codes":      "text",
	"code":       "text",
	"scripts":    "text",
	"script":     "text",
}

// temporalPatterns are temporal keywords that indicate date constraints.
var temporalPatterns = map[string]bool{
	"today": true,
	"yesterday": true,
	"last week": true,
	"last month": true,
	"last year": true,
	"this week": true,
	"this month": true,
	"this year": true,
	"next week": true,
	"next month": true,
	"next year": true,
	"past": true,
	"recent": true,
	"older": true,
	"newer": true,
	"since": true,
	"before": true,
	"after": true,
}

// parseNaturalLanguage attempts to parse common natural language patterns.
// Returns (spec, remainingSemanticText, ok) where ok=true if patterns were found.
func parseNaturalLanguage(input string, now time.Time) (FilterSpec, string, bool) {
	lower := strings.ToLower(input)
	
	// Pattern: "<file type> from <time period>" (e.g., "photos from last week")
	if kind, timePeriod, ok := extractKindAndTime(lower); ok {
		spec := FilterSpec{Source: SourceGrammar}
		
		// Add file type clause
		spec.Must = append(spec.Must, Clause{
			Field: FieldFileType,
			Op:    OpEq,
			Value: kind,
		})
		
		// Add time constraint if found
		if timePeriod != "" {
			if afterT, beforeT, ok := NormalizeDate(timePeriod, now); ok {
				spec.Must = append(spec.Must, Clause{
					Field: FieldModifiedAt,
					Op:    OpGte,
					Value: afterT.Unix(),
				})
				if !beforeT.Equal(now) {
					spec.Must = append(spec.Must, Clause{
						Field: FieldModifiedAt,
						Op:    OpLte,
						Value: beforeT.Unix(),
					})
				}
			}
		}
		
		// Extract remaining semantic text
		semantic := extractSemanticText(input, lower, kind, timePeriod)
		return spec, semantic, true
	}
	
	// Pattern: "<file type> that I took/on <date>" (e.g., "videos that I took on 12th march")
	if kind, specificDate, ok := extractKindAndDate(lower); ok {
		spec := FilterSpec{Source: SourceGrammar}
		
		// Add file type clause
		spec.Must = append(spec.Must, Clause{
			Field: FieldFileType,
			Op:    OpEq,
			Value: kind,
		})
		
		// Add specific date constraint
		if afterT, beforeT, ok := NormalizeDate(specificDate, now); ok {
			spec.Must = append(spec.Must, Clause{
				Field: FieldModifiedAt,
				Op:    OpGte,
				Value: afterT.Unix(),
			})
			spec.Must = append(spec.Must, Clause{
				Field: FieldModifiedAt,
				Op:    OpLte,
				Value: beforeT.Unix(),
			})
		}
		
		// Extract remaining semantic text
		semantic := extractSemanticText(input, lower, kind, specificDate)
		return spec, semantic, true
	}
	
	// Pattern: "<file type>" (simple file type filtering)
	if kind, ok := extractSimpleKind(lower); ok {
		spec := FilterSpec{Source: SourceGrammar}
		spec.Must = append(spec.Must, Clause{
			Field: FieldFileType,
			Op:    OpEq,
			Value: kind,
		})
		
		// Extract remaining semantic text
		semantic := extractSemanticText(input, lower, kind, "")
		return spec, semantic, true
	}
	
	return FilterSpec{}, "", false
}

// extractKindAndTime looks for patterns like "photos from last week"
func extractKindAndTime(lower string) (string, string, bool) {
	// Split into words and look for "from" as a separator
	words := strings.Fields(lower)
	for i := 0; i < len(words)-2; i++ {
		if words[i+1] == "from" {
			// Check if first word is a file type
			if kind, ok := nlKindPatterns[words[i]]; ok {
				// Check if the remaining part is a time period
				timePeriod := strings.Join(words[i+2:], " ")
				if temporalPatterns[timePeriod] {
					return kind, timePeriod, true
				}
			}
		}
	}
	return "", "", false
}

// extractKindAndDate looks for patterns like "videos that I took on 12th march"
func extractKindAndDate(lower string) (string, string, bool) {
	// Split into words and look for "on" as a separator
	words := strings.Fields(lower)
	for i := 0; i < len(words)-2; i++ {
		if words[i+1] == "on" {
			// Check if word before "on" could be a file type
			if kind, ok := nlKindPatterns[words[i]]; ok {
				// The rest after "on" should be the date
				datePart := strings.Join(words[i+2:], " ")
				return kind, datePart, true
			}
		}
	}
	return "", "", false
}

// extractSimpleKind looks for simple patterns like "photos" or "videos"
func extractSimpleKind(lower string) (string, bool) {
	words := strings.Fields(lower)
	if len(words) == 1 {
		if kind, ok := nlKindPatterns[words[0]]; ok {
			return kind, true
		}
	}
	return "", false
}

// extractSemanticText removes the parsed parts from the original input
func extractSemanticText(original, lower, kind, timePart string) string {
	// Remove the kind and time part from the original input
	// This is a simple implementation - could be improved for more complex patterns
	words := strings.Fields(lower)
	var remainingWords []string
	
	for _, word := range words {
		if word != kind && word != timePart {
			remainingWords = append(remainingWords, word)
		}
	}
	
	if len(remainingWords) == 0 {
		return ""
	}
	
	// Reconstruct from original to preserve case
	originalWords := strings.Fields(original)
	var result []string
	
	for _, origWord := range originalWords {
		keep := true
		lowerOrig := strings.ToLower(origWord)
		for _, remWord := range remainingWords {
			if lowerOrig == remWord {
				keep = true
				break
			}
		}
		if keep {
			result = append(result, origWord)
		}
	}
	
	return strings.Join(result, " ")
}

// Parse converts a user query string into a FilterSpec.
// It never panics and returns a best-effort result on malformed input.
func Parse(input string) (spec FilterSpec) {
	defer func() {
		if r := recover(); r != nil {
			// Return whatever we have so far.
		}
		spec.Source = SourceGrammar
	}()

	now := time.Now()
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

	spec.SemanticQuery = strings.TrimSpace(strings.Join(semanticParts, " "))
	return spec
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
		bare := p
		if strings.HasPrefix(bare, ".") {
			bare = bare[1:]
		}
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
