package query

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// StoreQuerier is the store interface needed by the cache.
type StoreQuerier interface {
	UpsertParsedQueryCache(normalizedQuery, specJSON string, schemaVersion int) error
	GetParsedQueryCache(normalizedQuery string, schemaVersion int) (string, error)
}

// ParsedQueryCache wraps the store to cache parsed FilterSpecs by query key.
type ParsedQueryCache struct {
	store StoreQuerier
}

// NewParsedQueryCache creates a new cache backed by store.
func NewParsedQueryCache(store StoreQuerier) *ParsedQueryCache {
	return &ParsedQueryCache{store: store}
}

// trailingPuncRe matches trailing punctuation characters.
var trailingPuncRe = regexp.MustCompile(`[.?!]+$`)

// collapseSpaceRe matches runs of whitespace.
var collapseSpaceRe = regexp.MustCompile(`\s+`)

// NormalizeKey lowercases, collapses whitespace, and strips trailing punctuation.
func NormalizeKey(query string) string {
	s := strings.ToLower(strings.TrimSpace(query))
	s = collapseSpaceRe.ReplaceAllString(s, " ")
	s = trailingPuncRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// Get retrieves a cached FilterSpec for the query. Returns nil on cache miss or
// version mismatch (rows at a different schema_version are silently ignored).
func (c *ParsedQueryCache) Get(query string) (*FilterSpec, error) {
	key := NormalizeKey(query)
	data, err := c.store.GetParsedQueryCache(key, CacheSchemaVersion)
	if err != nil {
		return nil, err
	}
	if data == "" {
		return nil, nil // cache miss or version mismatch
	}
	var spec FilterSpec
	if err := unmarshalFilterSpec([]byte(data), &spec); err != nil {
		return nil, fmt.Errorf("cache: unmarshal: %w", err)
	}
	spec.Source = SourceCache
	return &spec, nil
}

// Set stores the FilterSpec for the query in the cache at the current schema version.
func (c *ParsedQueryCache) Set(query string, spec FilterSpec) error {
	key := NormalizeKey(query)
	data, err := marshalFilterSpec(spec)
	if err != nil {
		return fmt.Errorf("cache: marshal: %w", err)
	}
	return c.store.UpsertParsedQueryCache(key, string(data), CacheSchemaVersion)
}

// ---------------------------------------------------------------------------
// Custom JSON marshaling for FilterSpec (handles Value any)
// ---------------------------------------------------------------------------

// taggedValue is used to encode the type of the Value field in a Clause.
type taggedValue struct {
	Type  string `json:"t"`
	Value any    `json:"v"`
}

type clauseJSON struct {
	Field FieldEnum   `json:"field"`
	Op    Op          `json:"op"`
	TV    taggedValue `json:"value"`
	Boost float32     `json:"boost,omitempty"`
}

type filterSpecJSON struct {
	SemanticQuery string       `json:"semantic_query,omitempty"`
	Must          []clauseJSON `json:"must,omitempty"`
	MustNot       []clauseJSON `json:"must_not,omitempty"`
	Should        []clauseJSON `json:"should,omitempty"`
	Source        SpecSource   `json:"source,omitempty"`
}

func clauseToJSON(c Clause) clauseJSON {
	var tv taggedValue
	switch v := c.Value.(type) {
	case string:
		tv = taggedValue{Type: "string", Value: v}
	case int64:
		tv = taggedValue{Type: "int64", Value: v}
	case int:
		tv = taggedValue{Type: "int64", Value: int64(v)}
	case time.Time:
		tv = taggedValue{Type: "time", Value: v.Format(time.RFC3339Nano)}
	case []string:
		tv = taggedValue{Type: "strings", Value: v}
	default:
		// Fallback: encode as string representation.
		tv = taggedValue{Type: "string", Value: fmt.Sprintf("%v", v)}
	}
	return clauseJSON{Field: c.Field, Op: c.Op, TV: tv, Boost: c.Boost}
}

func clauseFromJSON(cj clauseJSON) (Clause, error) {
	c := Clause{Field: cj.Field, Op: cj.Op, Boost: cj.Boost}
	switch cj.TV.Type {
	case "string":
		s, ok := cj.TV.Value.(string)
		if !ok {
			return c, fmt.Errorf("expected string value, got %T", cj.TV.Value)
		}
		c.Value = s
	case "int64":
		// JSON numbers decode as float64
		switch v := cj.TV.Value.(type) {
		case float64:
			c.Value = int64(v)
		case int64:
			c.Value = v
		case json.Number:
			n, err := v.Int64()
			if err != nil {
				return c, err
			}
			c.Value = n
		default:
			return c, fmt.Errorf("expected int64 value, got %T", cj.TV.Value)
		}
	case "time":
		s, ok := cj.TV.Value.(string)
		if !ok {
			return c, fmt.Errorf("expected time string, got %T", cj.TV.Value)
		}
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return c, err
		}
		c.Value = t
	case "strings":
		// JSON array decodes as []interface{}
		switch v := cj.TV.Value.(type) {
		case []interface{}:
			ss := make([]string, len(v))
			for i, item := range v {
				s, ok := item.(string)
				if !ok {
					return c, fmt.Errorf("expected string in array, got %T", item)
				}
				ss[i] = s
			}
			c.Value = ss
		case []string:
			c.Value = v
		default:
			return c, fmt.Errorf("expected []string value, got %T", cj.TV.Value)
		}
	default:
		c.Value = cj.TV.Value
	}
	return c, nil
}

func marshalFilterSpec(spec FilterSpec) ([]byte, error) {
	j := filterSpecJSON{
		SemanticQuery: spec.SemanticQuery,
		Source:        spec.Source,
	}
	for _, c := range spec.Must {
		j.Must = append(j.Must, clauseToJSON(c))
	}
	for _, c := range spec.MustNot {
		j.MustNot = append(j.MustNot, clauseToJSON(c))
	}
	for _, c := range spec.Should {
		j.Should = append(j.Should, clauseToJSON(c))
	}
	return json.Marshal(j)
}

func unmarshalFilterSpec(data []byte, spec *FilterSpec) error {
	var j filterSpecJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	spec.SemanticQuery = j.SemanticQuery
	spec.Source = j.Source

	for _, cj := range j.Must {
		c, err := clauseFromJSON(cj)
		if err != nil {
			return err
		}
		spec.Must = append(spec.Must, c)
	}
	for _, cj := range j.MustNot {
		c, err := clauseFromJSON(cj)
		if err != nil {
			return err
		}
		spec.MustNot = append(spec.MustNot, c)
	}
	for _, cj := range j.Should {
		c, err := clauseFromJSON(cj)
		if err != nil {
			return err
		}
		spec.Should = append(spec.Should, c)
	}
	return nil
}
