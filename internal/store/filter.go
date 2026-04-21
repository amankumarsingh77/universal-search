package store

import (
	"fmt"
	"strings"
)

// FieldEnum names a filterable column in the files table.
type FieldEnum string

const (
	FieldFileType   FieldEnum = "file_type"
	FieldExtension  FieldEnum = "extension"
	FieldSizeBytes  FieldEnum = "size_bytes"
	FieldModifiedAt FieldEnum = "modified_at"
	FieldPath       FieldEnum = "path"
)

// Op names a comparison operator for a filter clause.
type Op string

const (
	OpEq       Op = "eq"
	OpGt       Op = "gt"
	OpGte      Op = "gte"
	OpLt       Op = "lt"
	OpLte      Op = "lte"
	OpContains Op = "contains"
	OpInSet    Op = "in_set"
)

// Clause is a single filter predicate.
type Clause struct {
	Field FieldEnum
	Op    Op
	Value any
	Boost float32
}

// FilterSpec describes a structured query with required, excluded, and optional clauses.
type FilterSpec struct {
	SemanticQuery string
	Must          []Clause
	MustNot       []Clause
	Should        []Clause
}

// buildWhereClause compiles Must and MustNot clauses into a SQL WHERE fragment.
// It returns the SQL string (without the leading "WHERE") and the bound arguments.
// Returns "", nil when there are no clauses.
func buildWhereClause(must, mustNot []Clause) (string, []any, error) {
	var parts []string
	var args []any

	for _, c := range must {
		sql, cArgs, err := clauseToSQL(c, false)
		if err != nil {
			return "", nil, err
		}
		parts = append(parts, sql)
		args = append(args, cArgs...)
	}

	for _, c := range mustNot {
		sql, cArgs, err := clauseToSQL(c, true)
		if err != nil {
			return "", nil, err
		}
		parts = append(parts, sql)
		args = append(args, cArgs...)
	}

	if len(parts) == 0 {
		return "", nil, nil
	}
	return strings.Join(parts, " AND "), args, nil
}

// isUnixTimestamp reports whether v is an integer type (int, int64, etc.).
func isUnixTimestamp(v any) bool {
	switch v.(type) {
	case int, int32, int64, uint, uint32, uint64:
		return true
	}
	return false
}

// rhsExpr builds the RHS expression and arguments for a comparison operator.
// For the modified_at field, Unix integer timestamps are wrapped in datetime(?, 'unixepoch')
// so that they compare correctly against the ISO-8601 text values stored by the driver.
func rhsExpr(field FieldEnum, op Op, value any) (rhs string, args []any) {
	if field == FieldModifiedAt && isUnixTimestamp(value) {
		return "datetime(?, 'unixepoch')", []any{value}
	}
	return "?", []any{value}
}

// clauseToSQL converts a single Clause into a SQL predicate string plus arguments.
// negate=true wraps the predicate in NOT (...).
func clauseToSQL(c Clause, negate bool) (string, []any, error) {
	col := string(c.Field)

	var expr string
	var args []any

	switch c.Op {
	case OpEq:
		rhs, rArgs := rhsExpr(c.Field, c.Op, c.Value)
		expr = fmt.Sprintf("%s = %s", col, rhs)
		args = rArgs
	case OpGt:
		rhs, rArgs := rhsExpr(c.Field, c.Op, c.Value)
		expr = fmt.Sprintf("%s > %s", col, rhs)
		args = rArgs
	case OpGte:
		rhs, rArgs := rhsExpr(c.Field, c.Op, c.Value)
		expr = fmt.Sprintf("%s >= %s", col, rhs)
		args = rArgs
	case OpLt:
		rhs, rArgs := rhsExpr(c.Field, c.Op, c.Value)
		expr = fmt.Sprintf("%s < %s", col, rhs)
		args = rArgs
	case OpLte:
		rhs, rArgs := rhsExpr(c.Field, c.Op, c.Value)
		expr = fmt.Sprintf("%s <= %s", col, rhs)
		args = rArgs
	case OpContains:
		expr = fmt.Sprintf("%s LIKE '%%' || ? || '%%' ESCAPE '\\'", col)
		args = []any{escapeLike(fmt.Sprint(c.Value))}
	case OpInSet:
		vals, ok := c.Value.([]any)
		if !ok {
			// Try to convert []string
			if strVals, ok2 := c.Value.([]string); ok2 {
				vals = make([]any, len(strVals))
				for i, v := range strVals {
					vals[i] = v
				}
			} else {
				return "", nil, fmt.Errorf("OpInSet value must be []any or []string, got %T", c.Value)
			}
		}
		if len(vals) == 0 {
			return "1=0", nil, nil // empty set matches nothing
		}
		placeholders := make([]string, len(vals))
		for i := range vals {
			placeholders[i] = "?"
		}
		expr = fmt.Sprintf("%s IN (%s)", col, strings.Join(placeholders, ","))
		args = vals
	default:
		return "", nil, fmt.Errorf("unknown op: %s", c.Op)
	}

	if negate {
		expr = "NOT (" + expr + ")"
	}
	return expr, args, nil
}

// escapeLike escapes LIKE special characters (%, _) with a backslash so that
// user-supplied substrings are treated as literals, not wildcards.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
