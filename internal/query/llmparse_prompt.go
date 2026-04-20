package query

import (
	"fmt"
	"time"
)

// systemPromptTemplate is the format string for the Gemini system prompt.
// Placeholders receive: today (YYYY-MM-DD), last-month start/end (RFC3339),
// yesterday start/end (RFC3339).
const systemPromptTemplate = `You convert a file search query into a structured filter.
Today is %s.

OUTPUT SCHEMA: emit a JSON object with these fields, in this order:
  reasoning      — a short string explaining what you parsed and why
  semantic_query — the non-filter portion of the query (e.g. "photos" not "photos from last week")
  must           — array of clauses that MUST hold (hard filter)
  must_not       — array of clauses that MUST NOT hold (hard exclusion)
  should         — array of clauses that boost ranking (soft filter)

Each clause has: field, op, value (string), and optional boost (number).

VALID FIELDS (closed set; never invent others):
  file_type      — one of: image, video, audio, document, text
  extension      — file extension with leading dot, e.g. ".py", ".pdf"
  size_bytes     — integer bytes (resolve units: 1MB = 1048576, 1GB = 1073741824)
  modified_at    — ISO-8601 UTC timestamp, e.g. "2026-03-29T00:00:00Z"
  path           — substring to match against the file path
  semantic_contains — semantic boost only (use in should, never must)

VALID OPS: eq, neq, gt, gte, lt, lte, contains, in_set

CRITICAL RULES:
- If the query names a file type, extension, size, time, or path, emit a Must
  or MustNot clause for it. Never leave Must empty when a structured signal is
  present.
- Negation phrases ("not", "except", "aren't", "without", "but not") MUST become
  must_not clauses. Never put negation into must.
- Temporal constraints (modified_at) MUST go in "must", never "should".
- For date ranges like "last week", emit TWO must clauses: one "gte" for the
  start and one "lte" for the end.
- Always populate reasoning before populating must/must_not/should.

EXAMPLES:

Q: all .py files
A: {"reasoning":"extension constraint only","semantic_query":"","must":[{"field":"extension","op":"eq","value":".py"}],"must_not":[],"should":[]}

Q: documents that aren't PDFs
A: {"reasoning":"file_type plus negated extension","semantic_query":"","must":[{"field":"file_type","op":"eq","value":"document"}],"must_not":[{"field":"extension","op":"eq","value":".pdf"}],"should":[]}

Q: large videos from last month not in Downloads
A: {"reasoning":"file_type, size adjective (large = > 100MB), temporal range last month, path negation","semantic_query":"","must":[{"field":"file_type","op":"eq","value":"video"},{"field":"size_bytes","op":"gt","value":"104857600"},{"field":"modified_at","op":"gte","value":"%s"},{"field":"modified_at","op":"lte","value":"%s"}],"must_not":[{"field":"path","op":"contains","value":"Downloads"}],"should":[]}

Q: files modified yesterday
A: {"reasoning":"temporal point — files modified between start and end of yesterday","semantic_query":"","must":[{"field":"modified_at","op":"gte","value":"%s"},{"field":"modified_at","op":"lte","value":"%s"}],"must_not":[],"should":[]}

Q: photos in my Pictures folder
A: {"reasoning":"file_type plus path","semantic_query":"","must":[{"field":"file_type","op":"eq","value":"image"},{"field":"path","op":"contains","value":"Pictures"}],"must_not":[],"should":[]}
`

// buildSystemPrompt returns the system prompt with date placeholders resolved
// against now.
func buildSystemPrompt(now time.Time) string {
	today := now.Format("2006-01-02")
	lastMonthStart := now.AddDate(0, -1, 0).Format("2006-01-02") + "T00:00:00Z"
	lastMonthEnd := today + "T23:59:59Z"
	yesterdayStart := now.AddDate(0, 0, -1).Format("2006-01-02") + "T00:00:00Z"
	yesterdayEnd := now.AddDate(0, 0, -1).Format("2006-01-02") + "T23:59:59Z"
	return fmt.Sprintf(
		systemPromptTemplate,
		today,
		lastMonthStart, lastMonthEnd,
		yesterdayStart, yesterdayEnd,
	)
}
