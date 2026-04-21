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
		limiter:    alwaysAllowLimiter{},
		model:      DefaultLLMConfig().Model,
		maxRetries: DefaultLLMConfig().MaxRetries,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		generate:   generate,
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

// TestParseWithRetry_ContentsAlternateRoles asserts that contents arriving at
// attempt #2 and #3 have strictly alternating user/model roles (the model turn
// is echoed back before each correction turn).
func TestParseWithRetry_ContentsAlternateRoles(t *testing.T) {
	var receivedContents [][]*genai.Content
	callCount := 0

	// Always return a response with empty Must + empty MustNot for a query that
	// has a structured signal ("all .py files" → Extension signal), so the
	// validator fires on every attempt and we get 3 calls total.
	generate := func(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
		snap := make([]*genai.Content, len(contents))
		copy(snap, contents)
		receivedContents = append(receivedContents, snap)
		callCount++

		return &genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{
					Content: &genai.Content{
						Role: "model",
						Parts: []*genai.Part{
							{FunctionCall: &genai.FunctionCall{
								Name: "emit_filters",
								Args: map[string]any{
									"reasoning":      "empty",
									"semantic_query": "",
									"must":           []any{},
									"must_not":       []any{},
									"should":         []any{},
								},
							}},
						},
					},
				},
			},
		}, nil
	}

	p := buildFakeParser(generate)
	_, _ = p.parseWithRetry(context.Background(), "all .py files", FilterSpec{})

	if callCount != 3 {
		t.Fatalf("want 3 calls, got %d", callCount)
	}

	// Call 1: just the initial user query → [user]
	if len(receivedContents[0]) != 1 || receivedContents[0][0].Role != "user" {
		t.Fatalf("call 1: want [user], got %v", roleSlice(receivedContents[0]))
	}

	// Call 2: model echoed before correction → [user, model, user]
	if len(receivedContents[1]) != 3 {
		t.Fatalf("call 2: want 3 contents, got %d (%v)", len(receivedContents[1]), roleSlice(receivedContents[1]))
	}
	if receivedContents[1][0].Role != "user" || receivedContents[1][1].Role != "model" || receivedContents[1][2].Role != "user" {
		t.Fatalf("call 2: want [user,model,user], got %v", roleSlice(receivedContents[1]))
	}

	// Call 3: two model echoes, two corrections → [user, model, user, model, user]
	if len(receivedContents[2]) != 5 {
		t.Fatalf("call 3: want 5 contents, got %d (%v)", len(receivedContents[2]), roleSlice(receivedContents[2]))
	}
	expected := []string{"user", "model", "user", "model", "user"}
	for i, want := range expected {
		if receivedContents[2][i].Role != want {
			t.Fatalf("call 3 contents[%d]: want %q, got %q (%v)", i, want, receivedContents[2][i].Role, roleSlice(receivedContents[2]))
		}
	}
}

func roleSlice(cs []*genai.Content) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Role
	}
	return out
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
