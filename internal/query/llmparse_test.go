package query

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"google.golang.org/genai"
)

// --- validateLLMResponse tests ---

// REQ-010: structured signal + empty Must + empty MustNot → non-nil error; error mentions detected field name
func TestValidateLLMResponse_StructuredSignalEmptyClauses(t *testing.T) {
	// ".pdf" triggers Extension signal
	raw := "find all .pdf files"
	spec := FilterSpec{}

	err := validateLLMResponse(raw, spec)
	if err == nil {
		t.Fatal("expected non-nil error for structured signal with no clauses")
	}
	if !containsStr(err.Error(), "extension") {
		t.Errorf("error should mention 'extension', got: %q", err.Error())
	}
}

// EDGE-008: structured signal + non-empty Must → nil error
func TestValidateLLMResponse_StructuredSignalWithMust(t *testing.T) {
	raw := "find all .pdf files"
	spec := FilterSpec{
		Must: []Clause{
			{Field: FieldExtension, Op: OpEq, Value: ".pdf"},
		},
	}
	if err := validateLLMResponse(raw, spec); err != nil {
		t.Fatalf("expected nil error when Must is non-empty, got: %v", err)
	}
}

// EDGE-015: structured signal + empty Must + non-empty MustNot → nil error
func TestValidateLLMResponse_StructuredSignalWithMustNot(t *testing.T) {
	raw := "find all .pdf files"
	spec := FilterSpec{
		MustNot: []Clause{
			{Field: FieldExtension, Op: OpEq, Value: ".pdf"},
		},
	}
	if err := validateLLMResponse(raw, spec); err != nil {
		t.Fatalf("expected nil error when MustNot is non-empty, got: %v", err)
	}
}

// No structured signal + empty everything → nil error (free-text query)
func TestValidateLLMResponse_FreeTextQuery(t *testing.T) {
	raw := "sunset over ocean"
	spec := FilterSpec{}
	if err := validateLLMResponse(raw, spec); err != nil {
		t.Fatalf("expected nil error for free-text query, got: %v", err)
	}
}

// --- decodeToolCallResponse tests ---

func TestDecodeToolCallResponse_NilResponse(t *testing.T) {
	_, err := decodeToolCallResponse(nil)
	if err == nil {
		t.Fatal("expected error for nil response")
	}
}

func TestDecodeToolCallResponse_NoCandidates(t *testing.T) {
	resp := &genai.GenerateContentResponse{}
	_, err := decodeToolCallResponse(resp)
	if err == nil {
		t.Fatal("expected error for response with no candidates")
	}
}

func TestDecodeToolCallResponse_CandidateNoFunctionCall(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{Text: "some text"},
					},
				},
			},
		},
	}
	_, err := decodeToolCallResponse(resp)
	if err == nil {
		t.Fatal("expected error when no FunctionCall in parts")
	}
}

func TestDecodeToolCallResponse_ValidFunctionCall(t *testing.T) {
	args := map[string]any{
		"semantic_query": "photos",
		"reasoning":      "",
		"must": []any{
			map[string]any{
				"field": "file_type",
				"op":    "eq",
				"value": "image",
			},
		},
		"must_not": []any{},
		"should":   []any{},
	}
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{FunctionCall: &genai.FunctionCall{Name: "emit_filters", Args: args}},
					},
				},
			},
		},
	}
	spec, err := decodeToolCallResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.SemanticQuery != "photos" {
		t.Errorf("expected SemanticQuery='photos', got %q", spec.SemanticQuery)
	}
	if len(spec.Must) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(spec.Must))
	}
	if spec.Must[0].Field != FieldFileType {
		t.Errorf("expected field file_type, got %q", spec.Must[0].Field)
	}
}

// --- parseWithRetry tests ---

// buildFakeParser constructs an LLMParser with generate injected for testing.
func buildFakeParser(generate generateContentFn) *LLMParser {
	p := &LLMParser{
		limiter:  alwaysAllowLimiter{},
		model:    llmModelName,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		generate: generate,
	}
	return p
}

// REQ-011: stub returns valid response with empty Must+MustNot every time; assert 3 calls and returns last response.
func TestParseWithRetry_ExactlyThreeAttempts(t *testing.T) {
	callCount := 0
	args := map[string]any{
		"semantic_query": "some query",
		"reasoning":      "test",
		"must":           []any{},
		"must_not":       []any{},
		"should":         []any{},
	}
	// Use a query with a structured signal (.pdf) so validator fires.
	query := "find .pdf files"
	generate := func(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
		callCount++
		return &genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{
					Content: &genai.Content{
						Parts: []*genai.Part{
							{FunctionCall: &genai.FunctionCall{Name: "emit_filters", Args: args}},
						},
					},
				},
			},
		}, nil
	}

	p := buildFakeParser(generate)
	grammarSpec := FilterSpec{SemanticQuery: "grammar fallback"}
	spec, err := p.parseWithRetry(context.Background(), query, grammarSpec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 3 {
		t.Errorf("expected exactly 3 calls, got %d", callCount)
	}
	// Should return last response (not grammarSpec), even if validator failed.
	if spec.SemanticQuery != "some query" {
		t.Errorf("expected last response semantic_query='some query', got %q", spec.SemanticQuery)
	}
}

// REQ-013: reasoning discarded — returned FilterSpec.SemanticQuery is correctly populated, no leak.
func TestParseWithRetry_ReasoningDiscarded(t *testing.T) {
	args := map[string]any{
		"reasoning":      "this is detailed reasoning that must not appear in output",
		"semantic_query": "sunset photos",
		"must": []any{
			map[string]any{"field": "file_type", "op": "eq", "value": "image"},
		},
		"must_not": []any{},
		"should":   []any{},
	}
	generate := func(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
		return &genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{Content: &genai.Content{Parts: []*genai.Part{
					{FunctionCall: &genai.FunctionCall{Name: "emit_filters", Args: args}},
				}}},
			},
		}, nil
	}

	p := buildFakeParser(generate)
	spec, err := p.parseWithRetry(context.Background(), "sunset photos", FilterSpec{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.SemanticQuery != "sunset photos" {
		t.Errorf("expected semantic_query='sunset photos', got %q", spec.SemanticQuery)
	}
	// FilterSpec has no Reasoning field, so just ensure Must was parsed.
	if len(spec.Must) != 1 {
		t.Errorf("expected 1 must clause, got %d", len(spec.Must))
	}
}

// REQ-014a: transport error on first call → return grammarSpec unchanged, no error.
func TestParseWithRetry_TransportErrorFirstCall(t *testing.T) {
	generate := func(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
		return nil, errors.New("connection refused")
	}

	grammarSpec := FilterSpec{SemanticQuery: "grammar fallback", Source: SourceGrammar}
	p := buildFakeParser(generate)
	spec, err := p.parseWithRetry(context.Background(), "some query", grammarSpec)
	if err != nil {
		t.Fatalf("expected no error on first-call transport error, got: %v", err)
	}
	if spec.SemanticQuery != "grammar fallback" {
		t.Errorf("expected grammarSpec back, got SemanticQuery=%q", spec.SemanticQuery)
	}
}

// REQ-014b: transport error mid-loop → return last successful spec.
func TestParseWithRetry_TransportErrorMidLoop(t *testing.T) {
	callCount := 0
	// Use a structured signal query so validator fires.
	// But give a good Must on first call so validator passes → returns on first call.
	query := "find .pdf files"
	goodArgs := map[string]any{
		"semantic_query": "first good response",
		"reasoning":      "",
		"must": []any{
			map[string]any{"field": "extension", "op": "eq", "value": ".pdf"},
		},
		"must_not": []any{},
		"should":   []any{},
	}
	generate := func(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
		callCount++
		if callCount == 1 {
			return &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{Content: &genai.Content{Parts: []*genai.Part{
						{FunctionCall: &genai.FunctionCall{Name: "emit_filters", Args: goodArgs}},
					}}},
				},
			}, nil
		}
		return nil, errors.New("connection refused on retry")
	}

	grammarSpec := FilterSpec{SemanticQuery: "grammar fallback"}
	p := buildFakeParser(generate)
	spec, err := p.parseWithRetry(context.Background(), query, grammarSpec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// First call succeeded with Must non-empty → validator passes → returns immediately.
	if spec.SemanticQuery != "first good response" {
		t.Errorf("expected 'first good response', got %q", spec.SemanticQuery)
	}
}

// Happy path: stub returns response with non-empty Must on first call → exactly 1 call.
func TestParseWithRetry_HappyPath_SingleCall(t *testing.T) {
	callCount := 0
	// Structured signal query: ".pdf" → Extension signal.
	query := "find .pdf files"
	args := map[string]any{
		"semantic_query": "pdf files",
		"reasoning":      "",
		"must": []any{
			map[string]any{"field": "extension", "op": "eq", "value": ".pdf"},
		},
		"must_not": []any{},
		"should":   []any{},
	}
	generate := func(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
		callCount++
		return &genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{Content: &genai.Content{Parts: []*genai.Part{
					{FunctionCall: &genai.FunctionCall{Name: "emit_filters", Args: args}},
				}}},
			},
		}, nil
	}

	p := buildFakeParser(generate)
	spec, err := p.parseWithRetry(context.Background(), query, FilterSpec{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected exactly 1 call, got %d", callCount)
	}
	if len(spec.Must) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(spec.Must))
	}
}

// --- helpers ---

func containsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(s) < len(sub) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

type alwaysAllowLimiter struct{}

func (alwaysAllowLimiter) Allow() bool { return true }
