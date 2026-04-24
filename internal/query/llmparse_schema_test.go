package query

import (
	"context"
	"testing"

	"google.golang.org/genai"
)

// TestBuildResponseSchemaTightened verifies the tightened ResponseSchema fields.
func TestBuildResponseSchemaTightened(t *testing.T) {
	schema := buildResponseSchema()

	// Root Required includes all five top-level fields.
	wantRequired := []string{"reasoning", "semantic_query", "must", "must_not", "should"}
	if len(schema.Required) != len(wantRequired) {
		t.Errorf("root Required: want %v, got %v", wantRequired, schema.Required)
	} else {
		for i, f := range wantRequired {
			if schema.Required[i] != f {
				t.Errorf("root Required[%d]: want %q, got %q", i, f, schema.Required[i])
			}
		}
	}

	// PropertyOrdering matches design.
	wantOrdering := []string{"reasoning", "semantic_query", "must", "must_not", "should"}
	if len(schema.PropertyOrdering) != len(wantOrdering) {
		t.Errorf("PropertyOrdering: want %v, got %v", wantOrdering, schema.PropertyOrdering)
	} else {
		for i, f := range wantOrdering {
			if schema.PropertyOrdering[i] != f {
				t.Errorf("PropertyOrdering[%d]: want %q, got %q", i, f, schema.PropertyOrdering[i])
			}
		}
	}

	// Verify clauseSchema via the "must" array items.
	mustSchema, ok := schema.Properties["must"]
	if !ok {
		t.Fatal("schema missing 'must' property")
	}
	cs := mustSchema.Items
	if cs == nil {
		t.Fatal("must.Items is nil")
	}

	// clauseSchema Required = [field, op, value].
	wantClauseRequired := []string{"field", "op", "value"}
	if len(cs.Required) != len(wantClauseRequired) {
		t.Errorf("clauseSchema Required: want %v, got %v", wantClauseRequired, cs.Required)
	} else {
		for i, f := range wantClauseRequired {
			if cs.Required[i] != f {
				t.Errorf("clauseSchema Required[%d]: want %q, got %q", i, f, cs.Required[i])
			}
		}
	}

	// field.Enum == fieldEnumValues.
	fieldSchema, ok := cs.Properties["field"]
	if !ok {
		t.Fatal("clauseSchema missing 'field' property")
	}
	if len(fieldSchema.Enum) != len(fieldEnumValues) {
		t.Errorf("field.Enum len: want %d, got %d", len(fieldEnumValues), len(fieldSchema.Enum))
	}

	// op.Enum == opEnumValues.
	opSchema, ok := cs.Properties["op"]
	if !ok {
		t.Fatal("clauseSchema missing 'op' property")
	}
	if len(opSchema.Enum) != len(opEnumValues) {
		t.Errorf("op.Enum len: want %d, got %d", len(opEnumValues), len(opSchema.Enum))
	}

	// boost.Minimum == 0.0 and .Maximum == 5.0.
	boostSchema, ok := cs.Properties["boost"]
	if !ok {
		t.Fatal("clauseSchema missing 'boost' property")
	}
	if boostSchema.Minimum == nil {
		t.Fatal("boost.Minimum is nil")
	}
	if *boostSchema.Minimum != 0.0 {
		t.Errorf("boost.Minimum: want 0.0, got %f", *boostSchema.Minimum)
	}
	if boostSchema.Maximum == nil {
		t.Fatal("boost.Maximum is nil")
	}
	if *boostSchema.Maximum != 5.0 {
		t.Errorf("boost.Maximum: want 5.0, got %f", *boostSchema.Maximum)
	}
}

// TestBuildGenerateContentConfig verifies direct JSON mode config.
func TestBuildGenerateContentConfig(t *testing.T) {
	cfg := buildGenerateContentConfig("test system prompt")

	// No Tools.
	if len(cfg.Tools) != 0 {
		t.Errorf("expected no Tools, got %d", len(cfg.Tools))
	}
	// No ToolConfig.
	if cfg.ToolConfig != nil {
		t.Error("expected nil ToolConfig")
	}
	// ResponseMIMEType == "application/json".
	if cfg.ResponseMIMEType != "application/json" {
		t.Errorf("ResponseMIMEType: want %q, got %q", "application/json", cfg.ResponseMIMEType)
	}
	// ResponseSchema non-nil.
	if cfg.ResponseSchema == nil {
		t.Error("ResponseSchema is nil")
	}
	// SystemInstruction preserved.
	if cfg.SystemInstruction == nil {
		t.Fatal("SystemInstruction is nil")
	}
	if len(cfg.SystemInstruction.Parts) == 0 || cfg.SystemInstruction.Parts[0].Text != "test system prompt" {
		t.Errorf("SystemInstruction not preserved correctly")
	}
	// Temperature == 0.
	if cfg.Temperature == nil {
		t.Fatal("Temperature is nil")
	}
	if *cfg.Temperature != 0 {
		t.Errorf("Temperature: want 0, got %f", *cfg.Temperature)
	}
}

// TestRetryLoopStillWorks verifies that injecting two bad responses followed
// by one good response returns the good spec with OutcomeOK.
func TestRetryLoopStillWorks(t *testing.T) {
	callCount := 0
	goodArgs := map[string]any{
		"semantic_query": "good response",
		"reasoning":      "",
		"must": []any{
			map[string]any{"field": "extension", "op": "eq", "value": ".pdf"},
		},
		"must_not": []any{},
		"should":   []any{},
	}

	generate := func(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
		callCount++
		if callCount < 3 {
			// First two calls return malformed JSON.
			return &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{{
					Content: &genai.Content{
						Parts: []*genai.Part{{Text: "bad json{{"}},
					},
				}},
			}, nil
		}
		// Third call returns valid JSON.
		return &genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{{
				Content: &genai.Content{
					Parts: []*genai.Part{{Text: mustMarshal(goodArgs)}},
				},
			}},
		}, nil
	}

	p := buildFakeParser(generate)
	p.maxRetries = 2 // 3 attempts total
	result, err := p.parseWithRetry(context.Background(), "find .pdf files", FilterSpec{SemanticQuery: "fallback"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != OutcomeOK {
		t.Errorf("expected OutcomeOK, got %v", result.Outcome)
	}
	if result.Spec.SemanticQuery != "good response" {
		t.Errorf("expected 'good response', got %q", result.Spec.SemanticQuery)
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
}
