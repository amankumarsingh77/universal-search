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

// generateContentFn is a function type that matches genai.Models.GenerateContent,
// used as a testable seam in LLMParser.
type generateContentFn func(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error)

// LLMParser parses a query using Gemini Flash-Lite with tool-call mode.
type LLMParser struct {
	client   *genai.Client
	limiter  interface{ Allow() bool }
	model    string
	logger   *slog.Logger
	generate generateContentFn // testable seam; defaults to client.Models.GenerateContent
}

// NewLLMParser creates an LLMParser with the given client and rate limiter.
func NewLLMParser(client *genai.Client, limiter interface{ Allow() bool }) *LLMParser {
	p := &LLMParser{
		client:  client,
		limiter: limiter,
		model:   llmModelName,
		logger:  slog.Default().WithGroup("llmparser"),
	}
	p.generate = func(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
		return client.Models.GenerateContent(ctx, model, contents, config)
	}
	return p
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
	Reasoning     string      `json:"reasoning"`
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

// buildResponseSchema returns the schema for the LLM response (used as FunctionDeclaration.Parameters).
func buildResponseSchema() *genai.Schema {
	cs := clauseSchema()
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"reasoning":      {Type: genai.TypeString},
			"semantic_query": {Type: genai.TypeString},
			"must":           {Type: genai.TypeArray, Items: cs},
			"must_not":       {Type: genai.TypeArray, Items: cs},
			"should":         {Type: genai.TypeArray, Items: cs},
		},
	}
}

// buildToolCallConfig returns a GenerateContentConfig that forces a single
// tool call to emit_filters.
func buildToolCallConfig(systemPrompt string) *genai.GenerateContentConfig {
	return &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: systemPrompt}}},
		Tools: []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name:        "emit_filters",
				Description: "Emit the structured filter for the user's file search query.",
				Parameters:  buildResponseSchema(),
			}},
		}},
		ToolConfig: &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: genai.FunctionCallingConfigModeAny,
			},
		},
	}
}

// validateLLMResponse returns a non-nil error iff DetectStructuredFields(raw).Any()
// AND len(spec.Must) == 0 AND len(spec.MustNot) == 0.
func validateLLMResponse(raw string, spec FilterSpec) error {
	sig := DetectStructuredFields(raw)
	if !sig.Any() {
		return nil
	}
	if len(spec.Must) > 0 || len(spec.MustNot) > 0 {
		return nil
	}
	return fmt.Errorf("query mentions %s but the response has no must/must_not clauses for them; emit at least one clause for the detected fields",
		strings.Join(sig.Fields(), ", "))
}

// convertLLMResponseToSpec converts an llmResponse struct into a FilterSpec.
// Reasoning is automatically discarded (FilterSpec has no such field).
func convertLLMResponseToSpec(llmResp llmResponse) FilterSpec {
	spec := FilterSpec{
		SemanticQuery: strings.TrimSpace(llmResp.SemanticQuery),
		Source:        SourceLLM,
	}
	for _, c := range llmResp.Must {
		if clause, ok := llmClauseToClause(c); ok {
			spec.Must = append(spec.Must, clause)
		}
	}
	for _, c := range llmResp.MustNot {
		if clause, ok := llmClauseToClause(c); ok {
			spec.MustNot = append(spec.MustNot, clause)
		}
	}
	for _, c := range llmResp.Should {
		if clause, ok := llmClauseToClause(c); ok {
			spec.Should = append(spec.Should, clause)
		}
	}
	return spec
}

// decodeToolCallResponse extracts a FilterSpec from a GenerateContentResponse
// that contains a function call to emit_filters.
func decodeToolCallResponse(resp *genai.GenerateContentResponse) (FilterSpec, error) {
	if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return FilterSpec{}, fmt.Errorf("no candidates in response")
	}
	for _, part := range resp.Candidates[0].Content.Parts {
		if part.FunctionCall == nil {
			continue
		}
		argsJSON, err := json.Marshal(part.FunctionCall.Args)
		if err != nil {
			return FilterSpec{}, fmt.Errorf("marshal function call args: %w", err)
		}
		var llmResp llmResponse
		if err := json.Unmarshal(argsJSON, &llmResp); err != nil {
			return FilterSpec{}, fmt.Errorf("unmarshal function call args: %w", err)
		}
		return convertLLMResponseToSpec(llmResp), nil
	}
	return FilterSpec{}, fmt.Errorf("no function call in response")
}

// parseWithRetry calls Gemini with tool-call mode, validates the response, and
// retries up to 2 times (3 total attempts) appending the validator error as
// a user-role turn. On transport error on the first call, returns grammarSpec.
// On exhausted retries, returns the last response (never errors out).
func (p *LLMParser) parseWithRetry(ctx context.Context, query string, grammarSpec FilterSpec) (FilterSpec, error) {
	const maxRetries = 2
	systemPrompt := buildSystemPrompt(time.Now())
	config := buildToolCallConfig(systemPrompt)

	var lastSpec FilterSpec
	contents := []*genai.Content{
		{Role: "user", Parts: []*genai.Part{{Text: query}}},
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err := p.generate(ctx, p.model, contents, config)
		if err != nil {
			if attempt == 0 {
				return grammarSpec, nil
			}
			return lastSpec, nil
		}

		spec, decodeErr := decodeToolCallResponse(resp)
		if decodeErr != nil {
			p.logger.Debug("decode tool-call response failed", "error", decodeErr, "attempt", attempt)
			if attempt == maxRetries {
				return lastSpec, nil
			}
			if resp != nil && len(resp.Candidates) > 0 && resp.Candidates[0].Content != nil {
				contents = append(contents, resp.Candidates[0].Content)
			}
			contents = append(contents, &genai.Content{
				Role:  "user",
				Parts: []*genai.Part{{Text: "Your previous response could not be parsed: " + decodeErr.Error() + ". Try again."}},
			})
			continue
		}

		lastSpec = spec
		if err := validateLLMResponse(query, spec); err == nil {
			return spec, nil
		} else {
			p.logger.Debug("validator failed", "error", err, "attempt", attempt)
			if attempt == maxRetries {
				return spec, nil
			}
			if resp != nil && len(resp.Candidates) > 0 && resp.Candidates[0].Content != nil {
				contents = append(contents, resp.Candidates[0].Content)
			}
			contents = append(contents, &genai.Content{
				Role:  "user",
				Parts: []*genai.Part{{Text: err.Error()}},
			})
		}
	}
	return lastSpec, nil
}

// Parse invokes Gemini with tool-call mode.
// If rate-limited, timed out, or errored, returns grammarSpec unchanged.
func (p *LLMParser) Parse(ctx context.Context, query string, grammarSpec FilterSpec) (FilterSpec, error) {
	if !p.limiter.Allow() {
		return grammarSpec, nil
	}
	return p.parseWithRetry(ctx, query, grammarSpec)
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
