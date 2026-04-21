package search

import (
	"context"

	"universal-search/internal/query"
	"universal-search/internal/store"
)

// LadderConfig holds the relaxation ladder settings.
type LadderConfig struct {
	Enabled   bool
	DropOrder []query.FieldEnum
}

// DefaultLadderConfig returns the historical drop order with relaxation enabled.
func DefaultLadderConfig() LadderConfig {
	return LadderConfig{
		Enabled: true,
		DropOrder: []query.FieldEnum{
			query.FieldModifiedAt,
			query.FieldSizeBytes,
			query.FieldPath,
			query.FieldFileType,
			query.FieldExtension,
		},
	}
}

// ParseDropOrder converts a list of raw field strings (from TOML) to a
// FieldEnum slice, skipping entries that don't match any known field.
func ParseDropOrder(raw []string) []query.FieldEnum {
	if len(raw) == 0 {
		return DefaultLadderConfig().DropOrder
	}
	out := make([]query.FieldEnum, 0, len(raw))
	for _, s := range raw {
		f := query.FieldEnum(s)
		if query.KnownFields[f] {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return DefaultLadderConfig().DropOrder
	}
	return out
}

// Ladder progressively drops Must clauses until results are found.
type Ladder struct {
	enabled   bool
	dropOrder []query.FieldEnum
}

// NewLadder builds a Ladder from cfg. When cfg.DropOrder is nil the historical
// default order is used. Enabled is honoured exactly as supplied — callers
// that want defaults should pass DefaultLadderConfig().
func NewLadder(cfg LadderConfig) *Ladder {
	dropOrder := cfg.DropOrder
	if dropOrder == nil {
		dropOrder = DefaultLadderConfig().DropOrder
	}
	return &Ladder{enabled: cfg.Enabled, dropOrder: dropOrder}
}

// RelaxationLadder tries to find results by progressively dropping Must clauses
// in selectivity order. MustNot clauses are NEVER dropped (NLQ-092).
//
// Returns: results, human-readable description of the dropped filter (empty if
// no drop was needed), and any error.
func (l *Ladder) RelaxationLadder(
	ctx context.Context,
	planner *Planner,
	queryVec []float32,
	spec query.FilterSpec,
	k int,
) (results []store.SearchResult, droppedDesc string, err error) {
	logger := planner.logger
	if !l.enabled {
		// Relaxation disabled — run the planner once and return whatever it
		// finds without dropping any clauses.
		results, _, _, err = planner.Plan(queryVec, spec, k)
		return results, "", err
	}
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
		idx := findMostSelectiveMustClause(current.Must, l.dropOrder)
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
// next, according to the supplied drop order. Returns -1 if no clause can be
// dropped.
func findMostSelectiveMustClause(must []query.Clause, dropOrder []query.FieldEnum) int {
	for _, field := range dropOrder {
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

// RelaxationLadder is a package-level helper that constructs a default Ladder
// and delegates. Retained so existing tests compile unchanged.
func RelaxationLadder(
	ctx context.Context,
	planner *Planner,
	queryVec []float32,
	spec query.FilterSpec,
	k int,
) ([]store.SearchResult, string, error) {
	return NewLadder(DefaultLadderConfig()).RelaxationLadder(ctx, planner, queryVec, spec, k)
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
