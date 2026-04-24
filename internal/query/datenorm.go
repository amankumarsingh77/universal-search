package query

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// dateParser is the interface every stage in the chain must implement.
type dateParser interface {
	Parse(s string, now time.Time) (after, before time.Time, ok bool)
}

// dateChain is the ordered fallback chain. Declared as var so tests can inject
// alternative parsers to verify ordering or exercise specific stages in isolation.
var dateChain = []dateParser{
	handRulesParser{},
	anytimeParser{},
	dateParserParser{},
}

// NormalizeDate parses a date string to (after, before time.Time, ok bool).
// The chain is:
//  1. handRulesParser — exact phrases and regex rules with deterministic outputs.
//  2. anytimeParser — go-anytime with DefaultToPast for relative/fuzzy dates.
//  3. dateParserParser — go-dateparser with PreferredDateSource=Past for absolute dates.
//
// Returns (zero, zero, false) if no stage could parse the input.
func NormalizeDate(s string, now time.Time) (after, before time.Time, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	lower := strings.ToLower(s)
	for _, p := range dateChain {
		if a, b, k := p.Parse(lower, now); k {
			return a, b, true
		}
	}
	return
}

// ---------------------------------------------------------------------------
// ParseSize — unchanged from original; lives here because the file is still
// small enough (enforced by scripts/check-file-size.sh) and renaming adds churn.
// ---------------------------------------------------------------------------

// unitMultipliers maps size unit strings to byte multipliers.
var unitMultipliers = map[string]int64{
	"b":  1,
	"kb": 1024,
	"mb": 1024 * 1024,
	"gb": 1024 * 1024 * 1024,
	"tb": 1024 * 1024 * 1024 * 1024,
}

// sizeRe matches optional operator, number (int or float), and unit.
var sizeRe = regexp.MustCompile(`^(>=|<=|>|<|=)?(\d+(?:\.\d+)?)(b|kb|mb|gb|tb)$`)

// ParseSize parses a size string like ">10mb", "<=1GB", "500kb", "=5mb".
// Bare value (no operator) → gte.
// Returns (op, bytes, true) or ("", 0, false).
func ParseSize(s string) (op Op, bytes int64, ok bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return
	}

	m := sizeRe.FindStringSubmatch(s)
	if m == nil {
		return
	}

	opStr := m[1]
	numStr := m[2]
	unit := m[3]

	mult, mok := unitMultipliers[unit]
	if !mok {
		return
	}

	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return
	}

	// Saturate to MaxInt64 to prevent overflow.
	raw := num * float64(mult)
	var bytesVal int64
	if raw >= float64(math.MaxInt64) {
		bytesVal = math.MaxInt64
	} else {
		bytesVal = int64(raw)
	}

	switch opStr {
	case ">":
		op = OpGt
	case ">=":
		op = OpGte
	case "<":
		op = OpLt
	case "<=":
		op = OpLte
	case "=":
		op = OpEq
	default:
		op = OpGte // bare value
	}

	return op, bytesVal, true
}
