package query

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"google.golang.org/genai"
)

// REF-044: MaxRetries=0 in LLMConfig means a single-attempt policy — an
// error-returning fake triggers the fallback to grammarSpec on the first
// error instead of the default 2 retries.
func TestLLMParser_MaxRetriesFromConfig_Zero(t *testing.T) {
	callCount := 0
	generate := func(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
		callCount++
		return nil, errors.New("boom")
	}

	p := &LLMParser{
		limiter:    alwaysAllowLimiter{},
		model:      "test",
		maxRetries: 0,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		generate:   generate,
	}

	grammar := FilterSpec{SemanticQuery: "grammar fallback"}
	spec, err := p.parseWithRetry(context.Background(), "find .pdf files", grammar)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if spec.SemanticQuery != "grammar fallback" {
		t.Errorf("expected grammar fallback, got %q", spec.SemanticQuery)
	}
	if callCount != 1 {
		t.Errorf("expected exactly 1 call with MaxRetries=0, got %d", callCount)
	}
}
