package app

import (
	"context"

	"findo/internal/query"
)

// llmQueryParser is the narrow interface consumed by ParseQuery. It is
// satisfied by *query.LLMParser and by test stubs.
type llmQueryParser interface {
	Parse(ctx context.Context, raw string, grammarSpec query.FilterSpec) (query.ParseResult, error)
}

// parsedQueryCacheIface is the narrow interface for the parsed-query cache.
// It is satisfied by *query.ParsedQueryCache and by test stubs.
type parsedQueryCacheIface interface {
	Get(query string) (*query.FilterSpec, error)
	Set(query string, spec query.FilterSpec) error
}
