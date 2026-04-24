package query

import (
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Scorer — computes per-case and aggregate scores.
// ---------------------------------------------------------------------------

// scoreCase returns a float in [0,1] representing how well got matches want.
func scoreCase(got FilterSpec, want goldenExpected) float64 {
	total := 0.0
	for comp, weight := range componentWeights {
		var s float64
		if comp == "semantic" {
			s = scoreSemantic(got.SemanticQuery, want.Semantic)
		} else {
			field := fieldForComponent[comp]
			s = scoreComponent(comp, clausesForField(got, field), clausesForField2(want, field))
		}
		total += weight * s
	}
	return total
}

// scoreComponent returns 0 or 1 comparing got vs. want clauses for one field.
func scoreComponent(_ string, got []Clause, want []goldenClause) float64 {
	if len(got) == 0 && len(want) == 0 {
		return 1.0
	}
	if len(got) == 0 || len(want) == 0 || len(got) != len(want) {
		return 0.0
	}
	matched := make([]bool, len(got))
	for _, w := range want {
		found := false
		for i, g := range got {
			if matched[i] || string(g.Op) != w.Op {
				continue
			}
			if clauseValueMatches(g, w) {
				matched[i] = true
				found = true
				break
			}
		}
		if !found {
			return 0.0
		}
	}
	return 1.0
}

// clauseValueMatches compares a Clause value to a goldenClause value using
// type-appropriate rules: unix ±1s, int exact, string case-fold, string_list set.
func clauseValueMatches(got Clause, want goldenClause) bool {
	switch want.ValueKind {
	case "unix":
		wantVal, ok := toInt64(want.Value)
		if !ok {
			return false
		}
		gotVal, ok := toInt64(got.Value)
		if !ok {
			return false
		}
		return int64Abs(gotVal-wantVal) <= 1
	case "int":
		wantVal, ok := toInt64(want.Value)
		if !ok {
			return false
		}
		gotVal, ok := toInt64(got.Value)
		if !ok {
			return false
		}
		return gotVal == wantVal
	case "string":
		wantStr, ok := want.Value.(string)
		if !ok {
			return false
		}
		gotStr, ok := got.Value.(string)
		if !ok {
			return false
		}
		return strings.EqualFold(strings.TrimSpace(gotStr), strings.TrimSpace(wantStr))
	case "string_list":
		wantList, ok := toStringList(want.Value)
		if !ok {
			return false
		}
		gotList, ok := got.Value.([]string)
		if !ok {
			return false
		}
		return stringSetEqual(gotList, wantList)
	}
	return false
}

func scoreSemantic(got, want string) float64 {
	if strings.EqualFold(strings.TrimSpace(got), strings.TrimSpace(want)) {
		return 1.0
	}
	return 0.0
}

// clausesForField returns all Must+MustNot+Should clauses for a field.
func clausesForField(spec FilterSpec, field FieldEnum) []Clause {
	var out []Clause
	for _, c := range spec.Must {
		if c.Field == field {
			out = append(out, c)
		}
	}
	for _, c := range spec.MustNot {
		if c.Field == field {
			out = append(out, c)
		}
	}
	for _, c := range spec.Should {
		if c.Field == field {
			out = append(out, c)
		}
	}
	return out
}

// clausesForField2 returns all must+must_not+should goldenClauses for a field.
func clausesForField2(want goldenExpected, field FieldEnum) []goldenClause {
	var out []goldenClause
	fieldStr := string(field)
	for _, c := range want.Must {
		if c.Field == fieldStr {
			out = append(out, c)
		}
	}
	for _, c := range want.MustNot {
		if c.Field == fieldStr {
			out = append(out, c)
		}
	}
	for _, c := range want.Should {
		if c.Field == fieldStr {
			out = append(out, c)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Value conversion utilities
// ---------------------------------------------------------------------------

func toInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case int:
		return int64(x), true
	case float64:
		return int64(x), true
	case int32:
		return int64(x), true
	}
	return 0, false
}

func int64Abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func toStringList(v any) ([]string, bool) {
	switch x := v.(type) {
	case []string:
		return x, true
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

func stringSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]int, len(a))
	for _, s := range a {
		set[strings.ToLower(s)]++
	}
	for _, s := range b {
		set[strings.ToLower(s)]--
		if set[strings.ToLower(s)] < 0 {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Aggregation helpers
// ---------------------------------------------------------------------------

// topNWorst returns at most n caseScores sorted by score ascending.
func topNWorst(scores []caseScore, n int) []caseScore {
	sorted := make([]caseScore, len(scores))
	copy(sorted, scores)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].score < sorted[j].score
	})
	if n > len(sorted) {
		n = len(sorted)
	}
	return sorted[:n]
}

// categoryScores computes per-tag-category averages.
// A "category" is the tag prefix before ':', e.g. "kind" from "kind:image".
func categoryScores(cases []goldenCase, scores []float64) map[string]float64 {
	sums := make(map[string]float64)
	counts := make(map[string]int)
	for i, c := range cases {
		for _, tag := range c.Tags {
			cat := tag
			if idx := strings.Index(tag, ":"); idx >= 0 {
				cat = tag[:idx]
			}
			sums[cat] += scores[i]
			counts[cat]++
		}
	}
	out := make(map[string]float64, len(sums))
	for cat, sum := range sums {
		out[cat] = sum / float64(counts[cat])
	}
	return out
}

// overallMean computes the simple mean of scores.
func overallMean(scores []float64) float64 {
	if len(scores) == 0 {
		return 0
	}
	sum := 0.0
	for _, s := range scores {
		sum += s
	}
	return sum / float64(len(scores))
}
