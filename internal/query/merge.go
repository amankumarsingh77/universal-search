package query

import "fmt"

// ClauseKey identifies a clause for denylist matching.
type ClauseKey struct {
	Field FieldEnum
	Op    Op
	Value string
}

// clauseValueString converts a clause value to a string for denylist matching.
func clauseValueString(v any) string {
	return fmt.Sprintf("%v", v)
}

// Merge combines grammar and LLM FilterSpecs with grammar-wins policy.
//
// Must merging: for each (field, op) pair in LLM Must, if grammar already has a
// Must clause with the same field AND the same operator direction (gte/gt vs lte/lt),
// the LLM clause is dropped to avoid duplicate bounds. Complementary operators
// (e.g. grammar modified_at >= and LLM modified_at <=) are kept — they form a range.
//
// MustNot / Should: union of both sources, deduplicated by (field, op, value).
//
// ChipDenyList entries are dropped from the merged result.
// SemanticQuery comes from grammar (grammar wins).
// Source is set to SourceMerged.
func Merge(grammar, llm FilterSpec, chipDenyList []ClauseKey) FilterSpec {
	// Build set of (field, opDirection) pairs grammar has claimed in Must.
	// opDirection groups gte/gt together and lte/lt together so that
	// grammar's lower bound doesn't block LLM's upper bound (and vice versa).
	type fieldDir struct {
		field FieldEnum
		dir   string // "lower", "upper", "eq", "other"
	}
	opDir := func(op Op) string {
		switch op {
		case OpGte, OpGt:
			return "lower"
		case OpLte, OpLt:
			return "upper"
		case OpEq:
			return "eq"
		default:
			return "other"
		}
	}
	grammarMustDirs := make(map[fieldDir]bool)
	for _, c := range grammar.Must {
		grammarMustDirs[fieldDir{c.Field, opDir(c.Op)}] = true
	}

	// Build denylist lookup.
	denySet := make(map[ClauseKey]bool, len(chipDenyList))
	for _, k := range chipDenyList {
		denySet[k] = true
	}

	isDenied := func(c Clause) bool {
		k := ClauseKey{Field: c.Field, Op: c.Op, Value: clauseValueString(c.Value)}
		return denySet[k]
	}

	// clauseKey uniquely identifies a clause for deduplication.
	clauseKey := func(c Clause) ClauseKey {
		return ClauseKey{Field: c.Field, Op: c.Op, Value: clauseValueString(c.Value)}
	}

	// Start with grammar Must (filtered by denylist).
	must := make([]Clause, 0, len(grammar.Must)+len(llm.Must))
	for _, c := range grammar.Must {
		if !isDenied(c) {
			must = append(must, c)
		}
	}
	// Add LLM Must clauses that don't conflict with grammar's existing bounds.
	for _, c := range llm.Must {
		fd := fieldDir{c.Field, opDir(c.Op)}
		if grammarMustDirs[fd] {
			continue // same field+direction already provided by grammar
		}
		if !isDenied(c) {
			must = append(must, c)
		}
	}

	// MustNot: union of both, deduplicated by (field, op, value).
	mustNot := make([]Clause, 0, len(grammar.MustNot)+len(llm.MustNot))
	seenMustNot := make(map[ClauseKey]bool)
	for _, c := range grammar.MustNot {
		if !isDenied(c) {
			k := clauseKey(c)
			if !seenMustNot[k] {
				seenMustNot[k] = true
				mustNot = append(mustNot, c)
			}
		}
	}
	for _, c := range llm.MustNot {
		if !isDenied(c) {
			k := clauseKey(c)
			if !seenMustNot[k] {
				seenMustNot[k] = true
				mustNot = append(mustNot, c)
			}
		}
	}

	// Should: union of both, deduplicated by (field, op, value).
	should := make([]Clause, 0, len(grammar.Should)+len(llm.Should))
	seenShould := make(map[ClauseKey]bool)
	for _, c := range grammar.Should {
		if !isDenied(c) {
			k := clauseKey(c)
			if !seenShould[k] {
				seenShould[k] = true
				should = append(should, c)
			}
		}
	}
	for _, c := range llm.Should {
		if !isDenied(c) {
			k := clauseKey(c)
			if !seenShould[k] {
				seenShould[k] = true
				should = append(should, c)
			}
		}
	}

	// SemanticQuery: prefer grammar's if non-empty, else LLM's.
	semantic := grammar.SemanticQuery
	if semantic == "" {
		semantic = llm.SemanticQuery
	}

	return FilterSpec{
		SemanticQuery: semantic,
		Must:          must,
		MustNot:       mustNot,
		Should:        should,
		Source:        SourceMerged,
	}
}
