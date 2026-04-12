package query

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"google.golang.org/genai"
)

const llmModelName = "gemini-2.5-flash-lite"

// LLMParser parses a query using Gemini Flash-Lite with structured output.
type LLMParser struct {
	client  *genai.Client
	limiter interface{ Allow() bool }
	model   string
	logger  *slog.Logger
}

// NewLLMParser creates an LLMParser with the given client and rate limiter.
func NewLLMParser(client *genai.Client, limiter interface{ Allow() bool }) *LLMParser {
	return &LLMParser{
		client:  client,
		limiter: limiter,
		model:   llmModelName,
		logger:  slog.Default().WithGroup("llmparser"),
	}
}

// llmClause is the JSON representation of a clause returned by the LLM.
type llmClause struct {
	Field string  `json:"field"`
	Op    string  `json:"op"`
	Value string  `json:"value"`
	Boost float64 `json:"boost"`
}

// llmResponse is the JSON schema the LLM must fill.
type llmResponse struct {
	SemanticQuery string      `json:"semantic_query"`
	Must          []llmClause `json:"must"`
	MustNot       []llmClause `json:"must_not"`
	Should        []llmClause `json:"should"`
}

// fieldEnumValues lists valid field enum strings for the response schema.
var fieldEnumValues = []string{
	string(FieldFileType),
	string(FieldExtension),
	string(FieldSizeBytes),
	string(FieldModifiedAt),
	string(FieldPath),
	string(FieldSemanticContains),
}

// opEnumValues lists valid op enum strings for the response schema.
var opEnumValues = []string{
	string(OpEq), string(OpNeq), string(OpGt), string(OpGte),
	string(OpLt), string(OpLte), string(OpContains), string(OpInSet),
}

// clauseSchema returns the genai.Schema for a single clause object.
func clauseSchema() *genai.Schema {
	return &genai.Schema{
		Type:     genai.TypeObject,
		Required: []string{"field", "op", "value"},
		Properties: map[string]*genai.Schema{
			"field": {
				Type: genai.TypeString,
				Enum: fieldEnumValues,
			},
			"op": {
				Type: genai.TypeString,
				Enum: opEnumValues,
			},
			"value": {Type: genai.TypeString},
			"boost": {Type: genai.TypeNumber},
		},
	}
}

// buildResponseSchema returns the schema for the LLM response.
func buildResponseSchema() *genai.Schema {
	cs := clauseSchema()
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"semantic_query": {Type: genai.TypeString},
			"must":           {Type: genai.TypeArray, Items: cs},
			"must_not":       {Type: genai.TypeArray, Items: cs},
			"should":         {Type: genai.TypeArray, Items: cs},
		},
	}
}

// Parse invokes Gemini with a structured response schema.
// If rate-limited, timed out, or errored, returns grammarSpec unchanged.
// Passes "Today is YYYY-MM-DD" in system prompt.
func (p *LLMParser) Parse(ctx context.Context, query string, grammarSpec FilterSpec) (FilterSpec, error) {
	if !p.limiter.Allow() {
		return grammarSpec, nil
	}

	today := time.Now().Format("2006-01-02")
	systemPrompt := fmt.Sprintf(
		`You convert a file search query into a structured filter. Today is %s.
Return JSON matching the schema. Only use the fields listed (file_type, extension,
size_bytes, modified_at, path, semantic_contains). Never invent fields.

Rules:
- Temporal constraints (modified_at) MUST go in "must", never "should".
- For date ranges like "last week", emit TWO must clauses: one "gte" for the start and one "lte" for the end.
- File type / extension hints go in "should" with boost=1.5.
- Set semantic_query to the non-filter portion of the query (e.g. "photos" not "photos from last week").
- Use ISO-8601 UTC timestamps for modified_at values (e.g. "2026-03-29T00:00:00Z").`,
		today,
	)

	config := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: systemPrompt}}},
		ResponseMIMEType:  "application/json",
		ResponseSchema:    buildResponseSchema(),
	}

	resp, err := p.client.Models.GenerateContent(
		ctx,
		p.model,
		genai.Text(query),
		config,
	)
	if err != nil {
		// Timeout or other error: return grammar spec unchanged.
		return grammarSpec, nil
	}

	text := resp.Text()
	if text == "" {
		return grammarSpec, nil
	}

	var llmResp llmResponse
	if err := json.Unmarshal([]byte(text), &llmResp); err != nil {
		p.logger.Debug("llm response unmarshal failed", "error", err, "raw", text)
		return grammarSpec, nil
	}

	p.logger.Debug("llm raw response",
		"raw_text", text,
		"semantic_query", llmResp.SemanticQuery,
		"must_count", len(llmResp.Must),
		"must_not_count", len(llmResp.MustNot),
		"should_count", len(llmResp.Should),
	)
	for i, c := range llmResp.Must {
		p.logger.Debug("llm must clause", "index", i, "field", c.Field, "op", c.Op, "value", c.Value)
	}

	llmSpec := FilterSpec{
		SemanticQuery: strings.TrimSpace(llmResp.SemanticQuery),
		Source:        SourceLLM,
	}

	// Convert and validate clauses.
	for _, c := range llmResp.Must {
		if clause, ok := llmClauseToClause(c); ok {
			llmSpec.Must = append(llmSpec.Must, clause)
		} else {
			p.logger.Debug("llm must clause dropped", "field", c.Field, "op", c.Op, "value", c.Value)
		}
	}
	for _, c := range llmResp.MustNot {
		if clause, ok := llmClauseToClause(c); ok {
			llmSpec.MustNot = append(llmSpec.MustNot, clause)
		} else {
			p.logger.Debug("llm must_not clause dropped", "field", c.Field, "op", c.Op, "value", c.Value)
		}
	}
	for _, c := range llmResp.Should {
		if clause, ok := llmClauseToClause(c); ok {
			llmSpec.Should = append(llmSpec.Should, clause)
		} else {
			p.logger.Debug("llm should clause dropped", "field", c.Field, "op", c.Op, "value", c.Value)
		}
	}

	return llmSpec, nil
}

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
