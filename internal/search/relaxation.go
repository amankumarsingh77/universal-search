package search

import (
	"context"

	"universal-search/internal/query"
	"universal-search/internal/store"
)

// mustDropOrder defines the priority order for dropping Must clauses.
// Index 0 = drop first (most selective / least useful to keep).
var mustDropOrder = []query.FieldEnum{
	query.FieldModifiedAt,
	query.FieldSizeBytes,
	query.FieldPath,
	query.FieldFileType,
	query.FieldExtension,
}

// RelaxationLadder tries to find results by progressively dropping Must clauses
// in selectivity order. MustNot clauses are NEVER dropped (NLQ-092).
//
// Returns: results, human-readable description of the dropped filter (empty if
// no drop was needed), and any error.
func RelaxationLadder(
	ctx context.Context,
	planner *Planner,
	queryVec []float32,
	spec query.FilterSpec,
	k int,
) (results []store.SearchResult, droppedDesc string, err error) {
	logger := planner.logger
	current := copyFilterSpec(spec)
	logger.Debug("relaxation: starting ladder",
		"must_clauses", len(spec.Must),
		"must_not_clauses", len(spec.MustNot),
		"semantic_query", spec.SemanticQuery,
	)

	for {
		results, _, _, err = planner.Plan(queryVec, current, k)
		if err != nil {
			return nil, droppedDesc, err
		}
		if len(results) > 0 {
			logger.Debug("relaxation: found results", "results", len(results), "dropped_desc", droppedDesc)
			return results, droppedDesc, nil
		}

		// Find the next Must clause to drop (by priority order).
		idx := findMostSelectiveMustClause(current.Must)
		if idx < 0 {
			break // all Must clauses dropped, still no results
		}

		// Record the description of what we're dropping.
		droppedDesc = mustClauseDesc(current.Must[idx])
		logger.Debug("relaxation: dropping must clause",
			"field", current.Must[idx].Field,
			"description", droppedDesc,
			"remaining_must", len(current.Must)-1,
		)

		// Remove the clause.
		current.Must = removeMustClause(current.Must, idx)
	}

	// Final fallback: pure semantic (empty spec, preserve SemanticQuery and MustNot).
	if len(results) == 0 {
		logger.Debug("relaxation: all must clauses dropped, falling back to pure semantic",
			"semantic_query", spec.SemanticQuery,
			"must_not_preserved", len(spec.MustNot),
		)
		emptySpec := query.FilterSpec{
			SemanticQuery: spec.SemanticQuery,
			MustNot:       spec.MustNot, // MustNot is never dropped
		}
		results, _, _, err = planner.Plan(queryVec, emptySpec, k)
		if droppedDesc == "" {
			droppedDesc = "all filters"
		}
		logger.Debug("relaxation: semantic fallback complete", "results", len(results))
	}

	return results, droppedDesc, err
}

// findMostSelectiveMustClause returns the index of the Must clause to drop
// next, according to mustDropOrder. Returns -1 if no clause can be dropped.
func findMostSelectiveMustClause(must []query.Clause) int {
	for _, field := range mustDropOrder {
		for i, c := range must {
			if c.Field == field {
				return i
			}
		}
	}
	return -1
}

// mustClauseDesc returns a human-readable label for a Must clause being dropped.
func mustClauseDesc(c query.Clause) string {
	switch c.Field {
	case query.FieldModifiedAt:
		return "date filter"
	case query.FieldSizeBytes:
		return "size filter"
	case query.FieldPath:
		return "path filter"
	case query.FieldFileType:
		return "type filter"
	case query.FieldExtension:
		return "extension filter"
	default:
		return string(c.Field) + " filter"
	}
}

// removeMustClause returns a new slice with the element at index i removed.
func removeMustClause(must []query.Clause, i int) []query.Clause {
	out := make([]query.Clause, 0, len(must)-1)
	out = append(out, must[:i]...)
	out = append(out, must[i+1:]...)
	return out
}

// copyFilterSpec makes a shallow copy of a FilterSpec (slices are copied so
// modifications don't affect the original).
func copyFilterSpec(spec query.FilterSpec) query.FilterSpec {
	must := make([]query.Clause, len(spec.Must))
	copy(must, spec.Must)
	mustNot := make([]query.Clause, len(spec.MustNot))
	copy(mustNot, spec.MustNot)
	should := make([]query.Clause, len(spec.Should))
	copy(should, spec.Should)
	return query.FilterSpec{
		SemanticQuery: spec.SemanticQuery,
		Must:          must,
		MustNot:       mustNot,
		Should:        should,
		Source:        spec.Source,
	}
}
