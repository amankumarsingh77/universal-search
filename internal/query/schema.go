package query

// FieldEnum names a filterable field in the query pipeline.
type FieldEnum string

const (
	FieldFileType         FieldEnum = "file_type"
	FieldExtension        FieldEnum = "extension"
	FieldSizeBytes        FieldEnum = "size_bytes"
	FieldModifiedAt       FieldEnum = "modified_at"
	FieldPath             FieldEnum = "path"
	FieldSemanticContains FieldEnum = "semantic_contains"
)

// KnownFields is the closed-world set of valid fields.
var KnownFields = map[FieldEnum]bool{
	FieldFileType:         true,
	FieldExtension:        true,
	FieldSizeBytes:        true,
	FieldModifiedAt:       true,
	FieldPath:             true,
	FieldSemanticContains: true,
}

// Op names a comparison operator.
type Op string

const (
	OpEq       Op = "eq"
	OpNeq      Op = "neq"
	OpGt       Op = "gt"
	OpGte      Op = "gte"
	OpLt       Op = "lt"
	OpLte      Op = "lte"
	OpContains Op = "contains"
	OpInSet    Op = "in_set"
)

// Clause is a single filter predicate.
type Clause struct {
	Field FieldEnum
	Op    Op
	Value any     // string | int64 | time.Time | []string
	Boost float32 // for should clauses; 0 means 1.0
}

// SpecSource identifies the origin of a FilterSpec.
type SpecSource string

const (
	SourceGrammar SpecSource = "grammar"
	SourceLLM     SpecSource = "llm"
	SourceMerged  SpecSource = "merged"
	SourceCache   SpecSource = "cache"
)

// FilterSpec is the rich pipeline type describing a structured query.
type FilterSpec struct {
	SemanticQuery string
	Must          []Clause
	MustNot       []Clause
	Should        []Clause
	Source        SpecSource
}

// CacheSchemaVersion is the version written to parsed_query_cache.schema_version.
// Bump this when the FilterSpec serialization or parser semantics change in a way
// that makes cached entries unsafe to reuse.
const CacheSchemaVersion = 2

// KnownKindValues maps user-facing kind names to file_type values.
var KnownKindValues = map[string]string{
	"image":    "image",
	"img":      "image",
	"photo":    "image",
	"picture":  "image",
	"video":    "video",
	"audio":    "audio",
	"document": "document",
	"doc":      "document",
	"pdf":      "document",
	"text":     "text",
	"code":     "text",
}
