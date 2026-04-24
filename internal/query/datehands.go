package query

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// handRulesParser handles well-known relative date phrases with exact,
// deterministic semantics. It must be tried before any third-party library
// so its outputs are bit-for-bit stable across library upgrades.
type handRulesParser struct{}

func (handRulesParser) Parse(lower string, now time.Time) (after, before time.Time, ok bool) {
	lower = strings.TrimSpace(lower)

	// Exact-phrase switch.
	if a, b, k := exactPhraseDate(lower, now); k {
		return a, b, k
	}

	// "past N units" and "last N units" / "in the last N units".
	if a, b, k := parseNUnitsAgo(lower, now); k {
		return a, b, true
	}

	// "next N units".
	if a, b, k := parseNextNUnits(lower, now); k {
		return a, b, true
	}

	// "the last N hours" / "in the last N hours" / "last N hours".
	if a, b, k := parseNHoursAgo(lower, now); k {
		return a, b, true
	}

	// Year-only: "2025", "2024", etc.
	if yearOnlyRe.MatchString(lower) {
		y, _ := strconv.Atoi(lower)
		start := time.Date(y, 1, 1, 0, 0, 0, 0, now.Location())
		end := time.Date(y, 12, 31, 23, 59, 59, 0, now.Location())
		return start, end, true
	}

	// Quarter: "Q1 2023", "Q3 2024" (case-insensitive).
	if a, b, k := parseQuarter(lower, now); k {
		return a, b, true
	}

	// "month-name YYYY" e.g. "january 2022", "may 2023".
	if a, b, k := parseMonthYear(lower, now); k {
		return a, b, true
	}

	// "YYYY-MM" e.g. "2023-08".
	if a, b, k := parseYearMonth(lower, now); k {
		return a, b, true
	}

	// "last Monday", "last Tuesday", etc. → that specific weekday, last week.
	if a, b, k := parseLastWeekday(lower, now); k {
		return a, b, true
	}

	// "MM-DD-YYYY" US hyphen format (go-dateparser misinterprets as DD-MM-YYYY).
	if a, b, k := parseUSHyphenDate(lower, now); k {
		return a, b, true
	}

	return
}

var usHyphenDateRe = regexp.MustCompile(`^(\d{1,2})-(\d{1,2})-(\d{4})$`)

// parseUSHyphenDate matches "MM-DD-YYYY" (US format with hyphens). If the first
// pair is a valid month (1-12) but not a valid day-of-month-first interpretation,
// it treats it as MM-DD.
func parseUSHyphenDate(lower string, now time.Time) (after, before time.Time, ok bool) {
	m := usHyphenDateRe.FindStringSubmatch(lower)
	if m == nil {
		return
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	y, _ := strconv.Atoi(m[3])
	if a < 1 || a > 12 || b < 1 || b > 31 || y < 1000 {
		return
	}
	t := time.Date(y, time.Month(a), b, 0, 0, 0, 0, now.Location())
	// Validate date rolled over (e.g. Feb 30 → Mar 2). If so, reject.
	if t.Month() != time.Month(a) || t.Day() != b {
		return
	}
	return startOfDay(t), endOfDay(t), true
}

var weekdayNames = map[string]time.Weekday{
	"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday,
	"wednesday": time.Wednesday, "thursday": time.Thursday,
	"friday": time.Friday, "saturday": time.Saturday,
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday,
	"wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}
var lastWeekdayRe = regexp.MustCompile(`^last\s+(sunday|monday|tuesday|wednesday|thursday|friday|saturday|sun|mon|tue|wed|thu|fri|sat)$`)

// parseLastWeekday maps "last <weekday>" to the full day (00:00–23:59:59) of
// that weekday in the previous week.
func parseLastWeekday(lower string, now time.Time) (after, before time.Time, ok bool) {
	m := lastWeekdayRe.FindStringSubmatch(lower)
	if m == nil {
		return
	}
	wd, okw := weekdayNames[m[1]]
	if !okw {
		return
	}
	// How many days back: if today is Wed (3), "last Monday" = 9 days back
	// (last week's Monday), "last Tuesday" = 8 days back. Always go back into
	// the calendar week before the current one.
	today := int(now.Weekday())
	target := int(wd)
	diff := today - target
	if diff <= 0 {
		diff += 7
	}
	diff += 7 // previous week
	// But if today is Wed (3) and target is Monday (1) diff=2, +7=9 → last Monday (good).
	// If today is Wed (3) and target is Friday (5): diff = 3-5 = -2 → +7=5, +7=12 (good — two Fridays ago).
	d := now.AddDate(0, 0, -diff)
	return startOfDay(d), endOfDay(d), true
}

// exactPhraseDate resolves exact-match relative phrases.
// Returned windows are in now.Location() unless noted.
func exactPhraseDate(lower string, now time.Time) (after, before time.Time, ok bool) {
	switch lower {
	case "today":
		return startOfDay(now), now, true

	case "yesterday":
		y := now.AddDate(0, 0, -1)
		return startOfDay(y), endOfDay(y), true

	case "last night":
		// 18:00 yesterday → 05:59:59 today
		y := now.AddDate(0, 0, -1)
		start := time.Date(y.Year(), y.Month(), y.Day(), 18, 0, 0, 0, now.Location())
		end := time.Date(now.Year(), now.Month(), now.Day(), 5, 59, 59, 0, now.Location())
		return start, end, true

	case "tomorrow":
		t := now.AddDate(0, 0, 1)
		return startOfDay(t), endOfDay(t), true

	case "this week", "current week", "this week's downloads":
		return startOfWeek(now), now, true

	case "last week", "sometime last week", "this past week", "the past week",
		"past week", "the last week":
		return startOfDay(now.AddDate(0, 0, -7)), now, true

	case "next week":
		start := endOfWeek(now).Add(time.Second) // Monday 00:00
		end := start.AddDate(0, 0, 7).Add(-time.Second)
		return start, end, true

	case "this month", "current month", "sometime this month",
		"this past month", "the past month":
		return startOfMonth(now), now, true

	case "last month", "sometime last month":
		return startOfDay(now.AddDate(0, -1, 0)), now, true

	case "last fiscal year", "last financial year":
		return startOfDay(now.AddDate(-1, 0, 0)), now, true

	case "next month", "this upcoming month":
		start := startOfMonth(now).AddDate(0, 1, 0)
		end := startOfMonth(now).AddDate(0, 2, 0).Add(-time.Second)
		return start, end, true

	case "this year":
		return startOfYear(now), now, true

	case "last year":
		return startOfDay(now.AddDate(-1, 0, 0)), now, true

	case "next year":
		start := startOfYear(now).AddDate(1, 0, 0)
		end := startOfYear(now).AddDate(2, 0, 0).Add(-time.Second)
		return start, end, true

	case "sometime this week":
		return startOfWeek(now), now, true

	case "this morning", "earlier this morning":
		return atHour(now, 6), atHour(now, 12), true

	case "this afternoon":
		return atHour(now, 12), atHour(now, 18), true

	case "this evening", "tonight":
		return atHour(now, 18), atHour(now, 23).Add(time.Hour - time.Second), true

	case "earlier today":
		return startOfDay(now), now, true

	case "past couple of months", "past couple months", "couple of months ago",
		"a couple of months ago", "a couple months ago",
		"in the last couple of months", "in the past couple of months",
		"last couple of months", "the past couple of months":
		return startOfDay(now.AddDate(0, -2, 0)), now, true

	case "past few months", "a few months ago", "a few months back",
		"in the last few months", "last few months":
		return startOfDay(now.AddDate(0, -3, 0)), now, true

	case "past couple of weeks", "a couple of weeks ago", "a couple weeks ago",
		"in the last couple of weeks", "last couple of weeks":
		return startOfDay(now.AddDate(0, 0, -14)), now, true

	case "a couple of days ago", "a couple days ago",
		"in the last couple of days", "last couple of days",
		"past couple of days":
		return startOfDay(now.AddDate(0, 0, -2)), now, true

	case "a few days ago", "a few days back", "in the last few days",
		"in the past few days", "last few days", "past few days":
		return startOfDay(now.AddDate(0, 0, -3)), now, true

	case "a few weeks ago", "a few weeks back", "in the last few weeks",
		"in the past few weeks", "last few weeks", "past few weeks":
		return startOfDay(now.AddDate(0, 0, -21)), now, true

	case "a week ago", "one week ago":
		return startOfDay(now.AddDate(0, 0, -7)), now, true

	case "a week or so back", "a week or so ago",
		"in the past week or so", "about a week ago":
		return startOfDay(now.AddDate(0, 0, -7)), now, true

	case "recently", "a little while ago", "not too long ago",
		"just recently", "as of recently", "created recently":
		return startOfDay(now.AddDate(0, 0, -7)), now, true

	case "last quarter":
		start, end := lastQuarterBounds(now)
		return start, end, true

	case "end of last quarter":
		_, end := lastQuarterBounds(now)
		return startOfDay(end), endOfDay(end), true

	case "this quarter", "current quarter":
		start, _ := currentQuarterBounds(now)
		return start, now, true

	case "last summer":
		// Previous calendar year's June–August (meteorological summer)
		y := now.Year() - 1
		start := time.Date(y, 6, 1, 0, 0, 0, 0, now.Location())
		end := time.Date(y, 8, 31, 23, 59, 59, 0, now.Location())
		return start, end, true

	case "last winter":
		// Previous Dec–Feb (winter spans year-end)
		y := now.Year() - 1
		start := time.Date(y-1, 12, 1, 0, 0, 0, 0, now.Location())
		end := time.Date(y, 2, 28, 23, 59, 59, 0, now.Location())
		// Adjust for leap years.
		if time.Date(y, 2, 29, 0, 0, 0, 0, now.Location()).Month() == time.February {
			end = time.Date(y, 2, 29, 23, 59, 59, 0, now.Location())
		}
		return start, end, true

	case "last spring":
		y := now.Year() - 1
		start := time.Date(y, 3, 1, 0, 0, 0, 0, now.Location())
		end := time.Date(y, 5, 31, 23, 59, 59, 0, now.Location())
		return start, end, true

	case "last fall", "last autumn":
		y := now.Year() - 1
		start := time.Date(y, 9, 1, 0, 0, 0, 0, now.Location())
		end := time.Date(y, 11, 30, 23, 59, 59, 0, now.Location())
		return start, end, true

	case "early this month", "earlier this month":
		// first 10 days of current month
		s := startOfMonth(now)
		return s, time.Date(s.Year(), s.Month(), 10, 23, 59, 59, 0, now.Location()), true

	case "late this month":
		// last 10 days of current month
		nextMonthStart := startOfMonth(now).AddDate(0, 1, 0)
		start := nextMonthStart.AddDate(0, 0, -10)
		return start, nextMonthStart.Add(-time.Second), true

	case "early last month":
		s := startOfMonth(now).AddDate(0, -1, 0)
		return s, time.Date(s.Year(), s.Month(), 10, 23, 59, 59, 0, now.Location()), true

	case "late last month":
		// last 10 days of previous month
		thisMonthStart := startOfMonth(now)
		start := thisMonthStart.AddDate(0, 0, -10)
		return start, thisMonthStart.Add(-time.Second), true
	}

	// "since yesterday", "since last week", etc. — open-ended: [X, now].
	if strings.HasPrefix(lower, "since ") {
		rest := strings.TrimSpace(strings.TrimPrefix(lower, "since "))
		if a, _, k := exactPhraseDate(rest, now); k {
			return a, now, true
		}
	}

	return
}

// startOfDay returns midnight (00:00:00) on the same calendar day as t.
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// endOfDay returns the last second (23:59:59) of the same calendar day as t.
func endOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, t.Location())
}

// atHour returns the given hour (0-23) on the same calendar day as t.
func atHour(t time.Time, h int) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), h, 0, 0, 0, t.Location())
}

// startOfWeek returns 00:00 Monday of the ISO week containing t.
func startOfWeek(t time.Time) time.Time {
	// Weekday: Sunday=0..Saturday=6. ISO Monday=1.
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7 // treat Sunday as day 7
	}
	monday := t.AddDate(0, 0, -(wd - 1))
	return startOfDay(monday)
}

// endOfWeek returns 23:59:59 Sunday of the ISO week containing t.
func endOfWeek(t time.Time) time.Time {
	return startOfWeek(t).AddDate(0, 0, 7).Add(-time.Second)
}

// startOfMonth returns 00:00 on the first of t's month.
func startOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

// startOfYear returns 00:00 on Jan 1 of t's year.
func startOfYear(t time.Time) time.Time {
	return time.Date(t.Year(), 1, 1, 0, 0, 0, 0, t.Location())
}

// lastQuarterBounds returns [firstDay 00:00, lastDay 23:59:59] of the calendar
// quarter immediately before the quarter containing now.
func lastQuarterBounds(now time.Time) (time.Time, time.Time) {
	q := (int(now.Month())-1)/3 + 1 // 1..4
	lastQ := q - 1
	year := now.Year()
	if lastQ == 0 {
		lastQ = 4
		year--
	}
	startMonth := time.Month((lastQ-1)*3 + 1)
	start := time.Date(year, startMonth, 1, 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 3, 0).AddDate(0, 0, -1)
	return start, endOfDay(end)
}

// currentQuarterBounds returns [firstDay 00:00, lastDay 23:59:59] of the
// calendar quarter containing now.
func currentQuarterBounds(now time.Time) (time.Time, time.Time) {
	q := (int(now.Month())-1)/3 + 1
	startMonth := time.Month((q-1)*3 + 1)
	start := time.Date(now.Year(), startMonth, 1, 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 3, 0).AddDate(0, 0, -1)
	return start, endOfDay(end)
}

var (
	pastNRe       = regexp.MustCompile(`^(?:within |within the |in |in the |the )?(?:past|last)\s+(\d+|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)\s+(day|days|week|weeks|month|months|year|years)(?: ago)?$`)
	nUnitsAgoRe   = regexp.MustCompile(`^(?:from )?(\d+|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)\s+(day|days|week|weeks|month|months|year|years)\s+ago$`)
	nUnitsAgoBack = regexp.MustCompile(`^(?:from )?(\d+|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)\s+(day|days|week|weeks|month|months|year|years)\s+back$`)
	nHoursAgoRe   = regexp.MustCompile(`^(?:within |within the |in |in the |the )?(?:past|last)\s+(\d+)\s+(hour|hours)(?: ago)?$`)
	yearOnlyRe    = regexp.MustCompile(`^\d{4}$`)
	quarterRe     = regexp.MustCompile(`^q([1-4])\s+(\d{4})$`)
	yearMonthRe   = regexp.MustCompile(`^(\d{4})-(\d{1,2})$`)
	monthYearRe   = regexp.MustCompile(`^(january|february|march|april|may|june|july|august|september|october|november|december|jan|feb|mar|apr|jun|jul|aug|sep|sept|oct|nov|dec)\s+(\d{4})$`)
)

var wordNumbers = map[string]int{
	"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6,
	"seven": 7, "eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12,
}

// atoiOrWord converts a decimal string OR a word-number ("two", "three", etc.)
// to an int. Returns 0 if unparseable.
func atoiOrWord(s string) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	if n, ok := wordNumbers[strings.ToLower(s)]; ok {
		return n
	}
	return 0
}

var monthNameToNum = map[string]time.Month{
	"january": 1, "jan": 1,
	"february": 2, "feb": 2,
	"march": 3, "mar": 3,
	"april": 4, "apr": 4,
	"may":  5,
	"june": 6, "jun": 6,
	"july": 7, "jul": 7,
	"august": 8, "aug": 8,
	"september": 9, "sep": 9, "sept": 9,
	"october": 10, "oct": 10,
	"november": 11, "nov": 11,
	"december": 12, "dec": 12,
}

var nextNRe = regexp.MustCompile(`^(?:the |in the )?next\s+(\d+|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)\s+(day|days|week|weeks|month|months|year|years)$`)

// parseNextNUnits handles "next N days/weeks/..." → [now, now + N*unit].
func parseNextNUnits(lower string, now time.Time) (after, before time.Time, ok bool) {
	m := nextNRe.FindStringSubmatch(lower)
	if m == nil {
		return
	}
	n := atoiOrWord(m[1])
	if n <= 0 {
		return
	}
	unit := m[2]
	var end time.Time
	switch {
	case strings.HasPrefix(unit, "day"):
		end = now.AddDate(0, 0, n)
	case strings.HasPrefix(unit, "week"):
		end = now.AddDate(0, 0, 7*n)
	case strings.HasPrefix(unit, "month"):
		end = now.AddDate(0, n, 0)
	case strings.HasPrefix(unit, "year"):
		end = now.AddDate(n, 0, 0)
	default:
		return
	}
	return now, endOfDay(end), true
}

// parseNUnitsAgo handles "past/last N <unit>" forms. Accepts "one", "two" etc.
// as well as decimal integers.
func parseNUnitsAgo(lower string, now time.Time) (after, before time.Time, ok bool) {
	m := pastNRe.FindStringSubmatch(lower)
	if m == nil {
		m = nUnitsAgoRe.FindStringSubmatch(lower)
	}
	if m == nil {
		m = nUnitsAgoBack.FindStringSubmatch(lower)
	}
	if m == nil {
		return
	}
	n := atoiOrWord(m[1])
	if n <= 0 {
		return
	}
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
	return startOfDay(start), now, true
}

// parseNHoursAgo handles "in the last N hours" — returns hour-precision bounds.
func parseNHoursAgo(lower string, now time.Time) (after, before time.Time, ok bool) {
	m := nHoursAgoRe.FindStringSubmatch(lower)
	if m == nil {
		return
	}
	n, _ := strconv.Atoi(m[1])
	return now.Add(-time.Duration(n) * time.Hour), now, true
}

// parseQuarter handles "Q1 2023", "Q3 2024".
func parseQuarter(lower string, now time.Time) (after, before time.Time, ok bool) {
	m := quarterRe.FindStringSubmatch(lower)
	if m == nil {
		return
	}
	q, _ := strconv.Atoi(m[1])
	y, _ := strconv.Atoi(m[2])
	startMonth := time.Month((q-1)*3 + 1)
	start := time.Date(y, startMonth, 1, 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 3, 0).AddDate(0, 0, -1)
	return start, endOfDay(end), true
}

// parseMonthYear handles "January 2022", "may 2023", "Sept 2024".
func parseMonthYear(lower string, now time.Time) (after, before time.Time, ok bool) {
	m := monthYearRe.FindStringSubmatch(lower)
	if m == nil {
		return
	}
	mn, okm := monthNameToNum[m[1]]
	if !okm {
		return
	}
	y, _ := strconv.Atoi(m[2])
	start := time.Date(y, mn, 1, 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 1, 0).Add(-time.Second)
	return start, end, true
}

// parseYearMonth handles "2023-08".
func parseYearMonth(lower string, now time.Time) (after, before time.Time, ok bool) {
	m := yearMonthRe.FindStringSubmatch(lower)
	if m == nil {
		return
	}
	y, _ := strconv.Atoi(m[1])
	mn, _ := strconv.Atoi(m[2])
	if mn < 1 || mn > 12 {
		return
	}
	start := time.Date(y, time.Month(mn), 1, 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 1, 0).Add(-time.Second)
	return start, end, true
}
