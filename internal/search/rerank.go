package search

import (
	"sort"
	"time"

	"universal-search/internal/query"
	"universal-search/internal/store"
)

// Rerank applies should-boost and recency multiplier to results.
//
// final_score = cos_sim * product(matched_should_boosts) * recency_mult
//
// Where:
//   - cos_sim = 1 - Distance/2  (same formula as App.Search Score)
//   - should boost product: for each Should clause that matches the result's
//     file (file_type or extension), if Boost > 0 multiply into the product.
//   - recency_mult = 1.2 if spec has a Must clause on FieldModifiedAt AND the
//     file's ModifiedAt satisfies the clause bounds; else 1.0.
//
// Returns results sorted by FinalScore descending.
func Rerank(results []store.SearchResult, spec query.FilterSpec) []store.SearchResult {
	out := make([]store.SearchResult, len(results))
	copy(out, results)

	hasDateMust := hasModifiedAtMust(spec.Must)

	for i := range out {
		r := &out[i]
		cosSim := float32(1) - r.Distance/2

		// Compute should boost product.
		boostProduct := float32(1.0)
		for _, clause := range spec.Should {
			if clause.Boost > 0 && shouldClauseMatchesFile(clause, r.File) {
				boostProduct *= clause.Boost
			}
		}

		// Compute recency multiplier.
		recencyMult := float32(1.0)
		if hasDateMust && fileInDateMustRange(r.File, spec.Must) {
			recencyMult = 1.2
		}

		r.FinalScore = cosSim * boostProduct * recencyMult
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].FinalScore > out[j].FinalScore
	})

	return out
}

// hasModifiedAtMust reports whether any Must clause targets FieldModifiedAt.
func hasModifiedAtMust(must []query.Clause) bool {
	for _, c := range must {
		if c.Field == query.FieldModifiedAt {
			return true
		}
	}
	return false
}

// fileInDateMustRange returns true if the file's ModifiedAt satisfies all
// FieldModifiedAt Must clauses in the spec.
func fileInDateMustRange(file store.FileRecord, must []query.Clause) bool {
	for _, c := range must {
		if c.Field != query.FieldModifiedAt {
			continue
		}
		t, ok := c.Value.(time.Time)
		if !ok {
			continue
		}
		switch c.Op {
		case query.OpGte:
			if file.ModifiedAt.Before(t) {
				return false
			}
		case query.OpGt:
			if !file.ModifiedAt.After(t) {
				return false
			}
		case query.OpLte:
			if file.ModifiedAt.After(t) {
				return false
			}
		case query.OpLt:
			if !file.ModifiedAt.Before(t) {
				return false
			}
		case query.OpEq:
			if !file.ModifiedAt.Equal(t) {
				return false
			}
		}
	}
	return true
}

// shouldClauseMatchesFile returns true if the Should clause matches the file's
// file_type or extension field.
func shouldClauseMatchesFile(clause query.Clause, file store.FileRecord) bool {
	var fieldVal string
	switch clause.Field {
	case query.FieldFileType:
		fieldVal = file.FileType
	case query.FieldExtension:
		fieldVal = file.Extension
	default:
		return false
	}

	switch clause.Op {
	case query.OpEq:
		sv, ok := clause.Value.(string)
		if !ok {
			return false
		}
		return fieldVal == sv
	case query.OpContains:
		sv, ok := clause.Value.(string)
		if !ok {
			return false
		}
		return len(sv) > 0 && len(fieldVal) >= len(sv) && containsStr(fieldVal, sv)
	}
	return false
}

// containsStr is a simple substring check (used to avoid importing strings here).
func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
