package app

import (
	"context"
	"log/slog"
	"testing"

	"findo/internal/apperr"
	"findo/internal/config"
	"findo/internal/embedder"
	"findo/internal/query"
)

// stubLLMParser is a fake llmQueryParser that returns a pre-canned ParseResult.
type stubLLMParser struct {
	result query.ParseResult
}

func (s *stubLLMParser) Parse(_ context.Context, _ string, _ query.FilterSpec) (query.ParseResult, error) {
	return s.result, nil
}

// stubParsedQueryCache records how many times Set has been called.
type stubParsedQueryCache struct {
	setCalls int
}

func (s *stubParsedQueryCache) Get(_ string) (*query.FilterSpec, error) {
	// Always miss, so the LLM path is exercised.
	return nil, nil
}

func (s *stubParsedQueryCache) Set(_ string, _ query.FilterSpec) error {
	s.setCalls++
	return nil
}

// newOutcomeTestApp builds a minimal App with a stub parser and cache.
// The query passed to ParseQuery must trigger the LLM; use queries with
// temporal/negation/file-type keywords or > 6 tokens.
func newOutcomeTestApp(t *testing.T, parser llmQueryParser, cache parsedQueryCacheIface) *App {
	t.Helper()
	cfg := config.DefaultConfig()
	// Use defaults for trigger — queries in these tests carry file-type keywords
	// or are long enough to trigger LLM via the heuristic.
	return &App{
		cfg:              cfg,
		ctx:              context.Background(),
		logger:           slog.Default(),
		llmParser:        parser,
		parsedQueryCache: cache,
		// embedder non-nil so isOffline=false and snapshotEmbedderState returns non-nil emb.
		embedder: embedder.NewFake("fake-model", 768),
	}
}

// TestParseQuery_OutcomeOK_WritesCache — REQ-005
// When the LLM parser returns OutcomeOK, the result must carry chips, no
// ErrorCode / Warning, and the cache.Set must be called exactly once.
// Query uses "documents" to match fileTypeRe and trigger the LLM heuristic.
func TestParseQuery_OutcomeOK_WritesCache(t *testing.T) {
	okSpec := query.FilterSpec{
		SemanticQuery: "budget documents from last week",
		Must: []query.Clause{
			{Field: query.FieldFileType, Op: query.OpEq, Value: "document"},
		},
		Source: query.SourceLLM,
	}
	parser := &stubLLMParser{result: query.ParseResult{Spec: okSpec, Outcome: query.OutcomeOK}}
	cache := &stubParsedQueryCache{}

	a := newOutcomeTestApp(t, parser, cache)
	// "documents" triggers fileTypeRe; "last" triggers temporalRe — LLM will be invoked.
	result, err := a.ParseQuery("documents from last week")
	if err != nil {
		t.Fatalf("ParseQuery returned error: %v", err)
	}
	if result.ErrorCode != "" {
		t.Errorf("expected empty ErrorCode for OutcomeOK, got %q", result.ErrorCode)
	}
	if result.Warning != "" {
		t.Errorf("expected empty Warning for OutcomeOK, got %q", result.Warning)
	}
	if cache.setCalls != 1 {
		t.Errorf("expected cache.Set called once for OutcomeOK, called %d times", cache.setCalls)
	}
}

// TestParseQuery_OutcomeTimeout_SetsWarning_NoCacheWrite — REQ-006, EDGE-010
// On OutcomeTimeout, Warning must be "query_parse_timeout", ErrorCode must be
// empty, chips must come from the grammar parse, and cache.Set must NOT be called.
func TestParseQuery_OutcomeTimeout_SetsWarning_NoCacheWrite(t *testing.T) {
	// grammarSpec will be used by the parser on timeout.
	parser := &stubLLMParser{result: query.ParseResult{Outcome: query.OutcomeTimeout}}
	cache := &stubParsedQueryCache{}

	a := newOutcomeTestApp(t, parser, cache)
	// "kind:image" is parsed by grammar so we'll get at least one chip.
	result, err := a.ParseQuery("kind:image recent photos")
	if err != nil {
		t.Fatalf("ParseQuery returned error: %v", err)
	}
	if result.Warning != "query_parse_timeout" {
		t.Errorf("expected Warning=query_parse_timeout, got %q", result.Warning)
	}
	if result.ErrorCode != "" {
		t.Errorf("expected empty ErrorCode on timeout, got %q", result.ErrorCode)
	}
	if len(result.Chips) == 0 {
		t.Error("expected chips from grammar parse on timeout")
	}
	if cache.setCalls != 0 {
		t.Errorf("expected cache.Set NOT called on timeout, called %d times", cache.setCalls)
	}
}

// TestParseQuery_OutcomeFailed_SetsErrorCode_NoCacheWrite — REQ-007
// On OutcomeFailed, ErrorCode must be ERR_QUERY_PARSE_FAILED, Chips must be
// empty, and cache.Set must NOT be called.
// Query uses "documents" to trigger the LLM heuristic.
func TestParseQuery_OutcomeFailed_SetsErrorCode_NoCacheWrite(t *testing.T) {
	parser := &stubLLMParser{result: query.ParseResult{Outcome: query.OutcomeFailed}}
	cache := &stubParsedQueryCache{}

	a := newOutcomeTestApp(t, parser, cache)
	// "documents" triggers fileTypeRe; LLM will be invoked.
	result, err := a.ParseQuery("budget documents from yesterday")
	if err != nil {
		t.Fatalf("ParseQuery returned error: %v", err)
	}
	if result.ErrorCode != apperr.ErrQueryParseFailed.Code {
		t.Errorf("expected ErrorCode=%q, got %q", apperr.ErrQueryParseFailed.Code, result.ErrorCode)
	}
	if len(result.Chips) != 0 {
		t.Errorf("expected empty Chips on OutcomeFailed, got %d", len(result.Chips))
	}
	if cache.setCalls != 0 {
		t.Errorf("expected cache.Set NOT called on OutcomeFailed, called %d times", cache.setCalls)
	}
}

// TestParseQuery_OutcomeRateLimited_PropagatesRetryAfterMs_NoCacheWrite — REQ-008
// On OutcomeRateLimited, ErrorCode must be ERR_QUERY_RATE_LIMITED, RetryAfterMs
// must be propagated, Chips must be empty, and cache.Set must NOT be called.
// Query uses "documents" to trigger the LLM heuristic.
func TestParseQuery_OutcomeRateLimited_PropagatesRetryAfterMs_NoCacheWrite(t *testing.T) {
	const wantRetryAfterMs int64 = 15000
	parser := &stubLLMParser{result: query.ParseResult{
		Outcome:      query.OutcomeRateLimited,
		RetryAfterMs: wantRetryAfterMs,
	}}
	cache := &stubParsedQueryCache{}

	a := newOutcomeTestApp(t, parser, cache)
	// "documents" triggers fileTypeRe; LLM will be invoked.
	result, err := a.ParseQuery("budget documents from yesterday")
	if err != nil {
		t.Fatalf("ParseQuery returned error: %v", err)
	}
	if result.ErrorCode != apperr.ErrQueryRateLimited.Code {
		t.Errorf("expected ErrorCode=%q, got %q", apperr.ErrQueryRateLimited.Code, result.ErrorCode)
	}
	if result.RetryAfterMs != wantRetryAfterMs {
		t.Errorf("expected RetryAfterMs=%d, got %d", wantRetryAfterMs, result.RetryAfterMs)
	}
	if len(result.Chips) != 0 {
		t.Errorf("expected empty Chips on OutcomeRateLimited, got %d", len(result.Chips))
	}
	if cache.setCalls != 0 {
		t.Errorf("expected cache.Set NOT called on OutcomeRateLimited, called %d times", cache.setCalls)
	}
}
