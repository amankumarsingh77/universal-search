package query

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"google.golang.org/genai"
)

// buildFakeParserWithRetries constructs an LLMParser with a configurable maxRetries for testing.
func buildFakeParserWithRetries(generate generateContentFn, maxRetries int) *LLMParser {
	return &LLMParser{
		limiter:    alwaysAllowLimiter{},
		model:      DefaultLLMConfig().Model,
		maxRetries: maxRetries,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		generate:   generate,
	}
}

// buildValidToolCallResponse constructs a minimal valid *genai.GenerateContentResponse
// containing a FunctionCall to emit_filters with a valid set of args.
func buildValidToolCallResponse(semanticQuery string) *genai.GenerateContentResponse {
	argsMap := map[string]any{
		"reasoning":      "",
		"semantic_query": semanticQuery,
		"must":           []any{},
		"must_not":       []any{},
		"should":         []any{},
	}
	return &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{
				Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "emit_filters", Args: argsMap}}},
			},
		}},
	}
}

// TestParseOutcomeOK verifies that a valid tool-call response produces OutcomeOK.
// REQ-001 (baseline OK path).
func TestParseOutcomeOK(t *testing.T) {
	generate := func(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
		return buildValidToolCallResponse("x"), nil
	}
	p := buildFakeParserWithRetries(generate, 0)
	grammarSpec := FilterSpec{SemanticQuery: "fallback"}
	result, err := p.Parse(context.Background(), "x", grammarSpec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != OutcomeOK {
		t.Errorf("expected OutcomeOK, got %v", result.Outcome)
	}
}

// TestParseOutcomeFailed verifies that a non-rate-limit, non-timeout transport
// error produces OutcomeFailed and returns grammarSpec unchanged.
// REQ-001, EDGE-004 (attempt-0 silent fallback removed).
func TestParseOutcomeFailed(t *testing.T) {
	generate := func(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
		return nil, fmt.Errorf("dial tcp: connection refused")
	}
	p := buildFakeParserWithRetries(generate, 0)
	grammarSpec := FilterSpec{SemanticQuery: "grammar fallback", Source: SourceGrammar}
	result, err := p.Parse(context.Background(), "some query", grammarSpec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != OutcomeFailed {
		t.Errorf("expected OutcomeFailed, got %v", result.Outcome)
	}
	if result.Spec.SemanticQuery != grammarSpec.SemanticQuery {
		t.Errorf("expected grammarSpec back, got SemanticQuery=%q", result.Spec.SemanticQuery)
	}
	if result.Spec.Source != grammarSpec.Source {
		t.Errorf("expected grammarSpec.Source back, got Source=%v", result.Spec.Source)
	}
}

// TestParseOutcomeTimeout verifies that a deadline-exceeded context produces OutcomeTimeout.
// REQ-002, EDGE-003.
func TestParseOutcomeTimeout(t *testing.T) {
	generate := func(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
		// Block until context is cancelled.
		<-ctx.Done()
		return nil, ctx.Err()
	}
	p := buildFakeParserWithRetries(generate, 0)
	grammarSpec := FilterSpec{SemanticQuery: "grammar fallback"}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result, err := p.Parse(ctx, "some query", grammarSpec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != OutcomeTimeout {
		t.Errorf("expected OutcomeTimeout, got %v", result.Outcome)
	}
	if result.Spec.SemanticQuery != grammarSpec.SemanticQuery {
		t.Errorf("expected grammarSpec back, got SemanticQuery=%q", result.Spec.SemanticQuery)
	}
}

// TestParseOutcomeRateLimitedLocal verifies that when the local rate limiter
// denies a token, Parse returns OutcomeRateLimited immediately.
// REQ-003.
func TestParseOutcomeRateLimitedLocal(t *testing.T) {
	p := &LLMParser{
		limiter:    neverAllowLimiter{},
		model:      DefaultLLMConfig().Model,
		maxRetries: 0,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		generate: func(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			t.Fatal("generate should not be called when limiter denies")
			return nil, nil
		},
	}
	grammarSpec := FilterSpec{SemanticQuery: "grammar fallback"}
	result, err := p.Parse(context.Background(), "some query", grammarSpec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != OutcomeRateLimited {
		t.Errorf("expected OutcomeRateLimited, got %v", result.Outcome)
	}
	if result.RetryAfterMs != 0 {
		t.Errorf("expected RetryAfterMs==0 for local limiter denial, got %d", result.RetryAfterMs)
	}
}

// TestParseOutcomeRateLimited429WithRetryDelay verifies a 429 error with a
// retry_delay field produces OutcomeRateLimited and parses the delay in ms.
// REQ-004.
func TestParseOutcomeRateLimited429WithRetryDelay(t *testing.T) {
	generate := func(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
		return nil, fmt.Errorf("googleapi: Error 429: ... retry_delay:{seconds:30}")
	}
	p := buildFakeParserWithRetries(generate, 0)
	grammarSpec := FilterSpec{SemanticQuery: "grammar fallback"}
	result, err := p.Parse(context.Background(), "some query", grammarSpec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != OutcomeRateLimited {
		t.Errorf("expected OutcomeRateLimited, got %v", result.Outcome)
	}
	if result.RetryAfterMs != 30000 {
		t.Errorf("expected RetryAfterMs==30000, got %d", result.RetryAfterMs)
	}
}

// TestParseOutcomeRateLimited429NoRetryAfter verifies a plain 429 error (no
// parseable retry-after) produces OutcomeRateLimited with RetryAfterMs==0.
// EDGE-001.
func TestParseOutcomeRateLimited429NoRetryAfter(t *testing.T) {
	generate := func(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
		return nil, fmt.Errorf("google: 429")
	}
	p := buildFakeParserWithRetries(generate, 0)
	grammarSpec := FilterSpec{SemanticQuery: "grammar fallback"}
	result, err := p.Parse(context.Background(), "some query", grammarSpec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != OutcomeRateLimited {
		t.Errorf("expected OutcomeRateLimited, got %v", result.Outcome)
	}
	if result.RetryAfterMs != 0 {
		t.Errorf("expected RetryAfterMs==0 (no parseable header), got %d", result.RetryAfterMs)
	}
}

// TestParseRetryAfterHeaderNonNumeric verifies that a non-numeric Retry-After
// value produces RetryAfterMs==0.
// EDGE-002.
func TestParseRetryAfterHeaderNonNumeric(t *testing.T) {
	generate := func(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
		return nil, fmt.Errorf("Retry-After: abc, 429")
	}
	p := buildFakeParserWithRetries(generate, 0)
	grammarSpec := FilterSpec{SemanticQuery: "grammar fallback"}
	result, err := p.Parse(context.Background(), "some query", grammarSpec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The error contains "429" so it should be RateLimited, but RetryAfterMs==0 because "abc" is non-numeric.
	if result.Outcome != OutcomeRateLimited {
		t.Errorf("expected OutcomeRateLimited (error contains 429), got %v", result.Outcome)
	}
	if result.RetryAfterMs != 0 {
		t.Errorf("expected RetryAfterMs==0 for non-numeric Retry-After, got %d", result.RetryAfterMs)
	}
}

// neverAllowLimiter is a stub limiter that always denies.
type neverAllowLimiter struct{}

func (neverAllowLimiter) Allow() bool { return false }
