package query

import (
	"regexp"
	"strings"
)

// StructuredSignal reports which hard-filter fields the raw query mentions.
type StructuredSignal struct {
	Extension  bool
	FileType   bool
	SizeBytes  bool
	ModifiedAt bool
	Path       bool
}

// Any reports whether any structured field signal is present.
func (s StructuredSignal) Any() bool {
	return s.Extension || s.FileType || s.SizeBytes || s.ModifiedAt || s.Path
}

// Fields returns the names of the fields that are signaled, for use in
// validator error messages.
func (s StructuredSignal) Fields() []string {
	out := make([]string, 0, 5)
	if s.Extension {
		out = append(out, "extension")
	}
	if s.FileType {
		out = append(out, "file_type")
	}
	if s.SizeBytes {
		out = append(out, "size_bytes")
	}
	if s.ModifiedAt {
		out = append(out, "modified_at")
	}
	if s.Path {
		out = append(out, "path")
	}
	return out
}

var (
	// extension intent: literal `.xyz` token (1-6 alphanumeric chars after the dot)
	extensionRe = regexp.MustCompile(`\.[a-z0-9]{1,6}\b`)

	// size with unit: optional whitespace, KB/MB/GB/TB
	sizeUnitRe = regexp.MustCompile(`\b\d+\s*(kb|mb|gb|tb)\b`)

	// size adjectives
	sizeAdjRe = regexp.MustCompile(`\b(large|small|bigger|smaller|huge|tiny)\b`)

	// temporal individual tokens
	temporalTokenRe = regexp.MustCompile(`\b(today|yesterday|tomorrow|recent|recently|week|month|year|day)\b`)

	// "last <unit>" or "<n> <unit> ago"
	temporalPhraseRe = regexp.MustCompile(`\b(last\s+(week|month|year|day)|\d+\s+(days?|weeks?|months?|years?)\s+ago)\b`)

	// "in <CapitalizedWord>"
	pathInCapRe = regexp.MustCompile(`\bin\s+([A-Z][a-zA-Z0-9_-]+)`)

	// known root folder names; matched via word boundary, case-sensitive
	pathRootRe = regexp.MustCompile(`\b(Downloads|Desktop|Documents|Pictures|Music|Videos)\b`)
)

// DetectStructuredFields scans raw for explicit hints of any hard-filter field.
// Returns a StructuredSignal with one bool per field; a true value means the
// query mentions enough that the LLM is expected to emit at least one clause
// for that field. The function is pure and safe for concurrent use.
func DetectStructuredFields(raw string) StructuredSignal {
	if raw == "" {
		return StructuredSignal{}
	}
	lower := strings.ToLower(raw)
	sig := StructuredSignal{}

	if extensionRe.MatchString(lower) {
		sig.Extension = true
	}
	if hasKindWord(lower) {
		sig.FileType = true
	}
	if sizeUnitRe.MatchString(lower) || sizeAdjRe.MatchString(lower) {
		sig.SizeBytes = true
	}
	if temporalTokenRe.MatchString(lower) || temporalPhraseRe.MatchString(lower) {
		sig.ModifiedAt = true
	}
	// Path checks use the original-case raw to detect capitalized folder names.
	if pathInCapRe.MatchString(raw) || pathRootRe.MatchString(raw) {
		sig.Path = true
	}

	return sig
}

// hasKindWord reports whether lower contains any KnownKindValues key as a
// whole word. Reuses KnownKindValues from schema.go. Also matches the simple
// plural form ("videos" matches "video", "docs" matches "doc") since users
// commonly type plural NL queries.
func hasKindWord(lower string) bool {
	for kind := range KnownKindValues {
		if matchWholeWord(lower, kind) || matchWholeWord(lower, kind+"s") {
			return true
		}
	}
	return false
}

func matchWholeWord(lower, word string) bool {
	idx := 0
	for {
		pos := strings.Index(lower[idx:], word)
		if pos < 0 {
			return false
		}
		abs := idx + pos
		before := abs == 0 || !isWordChar(lower[abs-1])
		after := abs+len(word) == len(lower) || !isWordChar(lower[abs+len(word)])
		if before && after {
			return true
		}
		idx = abs + len(word)
	}
}

func isWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}
