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

// temporalPatterns are temporal keywords that indicate date constraints.
var temporalPatterns = map[string]bool{
	"today":      true,
	"yesterday":  true,
	"last week":  true,
	"last month": true,
	"last year":  true,
	"this week":  true,
	"this month": true,
	"this year":  true,
	"next week":  true,
	"next month": true,
	"next year":  true,
	"past":       true,
	"recent":     true,
	"older":      true,
	"newer":      true,
	"since":      true,
	"before":     true,
	"after":      true,
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
