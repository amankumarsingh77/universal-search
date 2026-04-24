package query

import (
	"time"

	anytime "github.com/ijt/go-anytime"
	dps "github.com/markusmobius/go-dateparser"
)

// anytimeParser wraps github.com/ijt/go-anytime with the DefaultToPast option.
// It first tries ParseRange (for phrases like "last week" if they slip through),
// then falls back to Parse for point-in-time expressions, producing a full-day
// window in that case.
type anytimeParser struct{}

func (anytimeParser) Parse(lower string, now time.Time) (after, before time.Time, ok bool) {
	if r, err := anytime.ParseRange(lower, now, anytime.DefaultToPast); err == nil {
		return r.Start(), r.End(), true
	}
	if t, err := anytime.Parse(lower, now, anytime.DefaultToPast); err == nil {
		return startOfDay(t), endOfDay(t), true
	}
	return
}

// dateParserParser wraps github.com/markusmobius/go-dateparser with
// PreferredDateSource=Past so ambiguous month/year names resolve to the past.
type dateParserParser struct{}

func (dateParserParser) Parse(lower string, now time.Time) (after, before time.Time, ok bool) {
	cfg := &dps.Configuration{
		CurrentTime:         now,
		PreferredDateSource: dps.Past,
		StrictParsing:       false,
	}
	dt, err := dps.Parse(cfg, lower)
	if err != nil || dt.IsZero() {
		return
	}
	t := dt.Time
	return startOfDay(t), endOfDay(t), true
}
