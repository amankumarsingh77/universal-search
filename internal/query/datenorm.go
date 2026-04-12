package query

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/araddon/dateparse"
	"github.com/olebedev/when"
	"github.com/olebedev/when/rules/common"
	"github.com/olebedev/when/rules/en"
)

// whenParser is the English + common rules NLP parser, initialised once.
var whenParser *when.Parser

func init() {
	whenParser = when.New(nil)
	whenParser.Add(en.All...)
	whenParser.Add(common.All...)
}

// NormalizeDate parses a date string to (after, before time.Time, ok bool).
// Tries olebedev/when first for relative phrases, then araddon/dateparse for absolutes.
// "yesterday" → full calendar day (00:00 to 23:59:59).
// "last week" → 7 days ago 00:00 to now.
// Returns (zero, zero, false) if unparseable.
func NormalizeDate(s string, now time.Time) (after, before time.Time, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}

	lower := strings.ToLower(s)

	// -- Special-case well-known relative phrases before delegating --

	switch lower {
	case "yesterday":
		yest := now.AddDate(0, 0, -1)
		start := time.Date(yest.Year(), yest.Month(), yest.Day(), 0, 0, 0, 0, now.Location())
		end := time.Date(yest.Year(), yest.Month(), yest.Day(), 23, 59, 59, 0, now.Location())
		return start, end, true

	case "today":
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return start, now, true

	case "last week":
		start := now.AddDate(0, 0, -7)
		start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, now.Location())
		return start, now, true

	case "last month":
		start := now.AddDate(0, -1, 0)
		start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, now.Location())
		return start, now, true

	case "last year":
		start := now.AddDate(-1, 0, 0)
		start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, now.Location())
		return start, now, true
	}

	// "past N days/weeks/months"
	if t, b, parsed := parsePastN(lower, now); parsed {
		return t, b, true
	}

	// -- Try araddon/dateparse first for structured absolute date formats --
	// Only attempt if the string looks like a structured date (contains digit+separator).
	if looksLikeStructuredDate(s) {
		if t, err := dateparse.ParseAny(s); err == nil {
			start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
			end := time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, now.Location())
			return start, end, true
		}
	}

	// -- Try olebedev/when (NLP relative dates) --
	if result, err := whenParser.Parse(s, now); err == nil && result != nil {
		t := result.Time
		// If the result falls in the future but input is past-tense phrasing, clamp
		if t.After(now) {
			t = now
		}
		// Normalize to start-of-day for consistency with other branches.
		start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
		return start, now, true
	}

	// -- Try araddon/dateparse for any remaining formats --
	if t, err := dateparse.ParseAny(s); err == nil {
		start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
		end := time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, now.Location())
		return start, end, true
	}

	// -- Year-only: "2025" --
	if yearOnlyRe.MatchString(s) {
		year, _ := strconv.Atoi(s)
		start := time.Date(year, 1, 1, 0, 0, 0, 0, now.Location())
		end := time.Date(year, 12, 31, 23, 59, 59, 0, now.Location())
		return start, end, true
	}

	return
}

var yearOnlyRe = regexp.MustCompile(`^\d{4}$`)

// structuredDateRe matches strings with digits and common date separators
// (ISO, slashes, dots) — indicating a structured date format.
var structuredDateRe = regexp.MustCompile(`\d.*[-/.].*\d|\d{8}`)

// looksLikeStructuredDate returns true if s looks like a machine-formatted date.
func looksLikeStructuredDate(s string) bool {
	return structuredDateRe.MatchString(s)
}

var pastNRe = regexp.MustCompile(`^past\s+(\d+)\s+(day|days|week|weeks|month|months|year|years)$`)

func parsePastN(s string, now time.Time) (after, before time.Time, ok bool) {
	m := pastNRe.FindStringSubmatch(s)
	if m == nil {
		return
	}
	n, _ := strconv.Atoi(m[1])
	unit := m[2]
	var start time.Time
	switch {
	case strings.HasPrefix(unit, "day"):
		start = now.AddDate(0, 0, -n)
	case strings.HasPrefix(unit, "week"):
		start = now.AddDate(0, 0, -7*n)
	case strings.HasPrefix(unit, "month"):
		start = now.AddDate(0, -n, 0)
	case strings.HasPrefix(unit, "year"):
		start = now.AddDate(-n, 0, 0)
	default:
		return
	}
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, now.Location())
	return start, now, true
}

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
