package query

import (
	"strconv"
	"strings"
	"time"
)

// resolveDateToUnix converts a date string to a Unix int64 for use in modified_at
// clauses. Handles ISO-8601/RFC3339 timestamps (from LLM) and NLP phrases like
// "last week" (from NormalizeDate). Returns (unix, true) or (0, false) if
// the string cannot be parsed.
func resolveDateToUnix(s string, op Op) (int64, bool) {
	// Try RFC3339 / ISO-8601 first (LLM typically emits these).
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Unix(), true
	}
	if t, err := time.Parse("2006-01-02T15:04:05Z", s); err == nil {
		return t.Unix(), true
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		if op == OpLte || op == OpLt {
			// End of day for upper bounds.
			t = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, t.Location())
		}
		return t.Unix(), true
	}

	// Fall back to NLP phrase resolution (e.g. "last week", "yesterday").
	now := time.Now()
	after, before, ok := NormalizeDate(s, now)
	if !ok {
		return 0, false
	}
	switch op {
	case OpGte, OpGt:
		return after.Unix(), true
	case OpLte, OpLt:
		return before.Unix(), true
	default:
		return after.Unix(), true
	}
}

// llmClauseToClause converts an llmClause to a Clause, validating the field.
// Returns (Clause{}, false) if the field is unknown (NLQ-034).
//
// Type coercions applied (LLM always returns string values in JSON schema):
//   - modified_at → int64 Unix seconds (via resolveDateToUnix)
//   - size_bytes  → int64 bytes (try plain integer, then ParseSize for "10mb" notation)
//   - extension with op=in_set → []string (comma-split, dot-prefixed)
func llmClauseToClause(lc llmClause) (Clause, bool) {
	field := FieldEnum(lc.Field)
	if !KnownFields[field] {
		return Clause{}, false
	}
	op := Op(lc.Op)

	var value any = lc.Value

	switch field {
	case FieldModifiedAt:
		// Resolve date strings to Unix int64.
		unix, resolved := resolveDateToUnix(lc.Value, op)
		if !resolved {
			return Clause{}, false
		}
		value = unix

	case FieldSizeBytes:
		// LLM may return a plain integer string ("10485760") or size notation ("10mb").
		// Try plain int64 first, then ParseSize.
		if n, err := strconv.ParseInt(strings.TrimSpace(lc.Value), 10, 64); err == nil {
			value = n
		} else {
			// Strip any operator prefix that ParseSize expects (e.g. "10mb" → op already known).
			_, bytes, ok := ParseSize(lc.Value)
			if !ok {
				return Clause{}, false
			}
			value = bytes
		}

	case FieldExtension:
		if op == OpInSet {
			// Comma-separated list: "jpg,png" or ".jpg,.png"
			parts := strings.Split(lc.Value, ",")
			exts := make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				if !strings.HasPrefix(p, ".") {
					p = "." + p
				}
				exts = append(exts, p)
			}
			if len(exts) == 0 {
				return Clause{}, false
			}
			value = exts
		}
		// Other ops (eq, contains) keep value as string.
	}

	return Clause{
		Field: field,
		Op:    op,
		Value: value,
		Boost: float32(lc.Boost),
	}, true
}
