package app

// HighlightRangeDTO is a byte range [Start, End) within a filename string
// that should be highlighted in the UI.
type HighlightRangeDTO struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// SearchResultDTO is the JSON-serializable search result sent to the frontend.
type SearchResultDTO struct {
	FilePath      string              `json:"filePath"`
	FileName      string              `json:"fileName"`
	FileType      string              `json:"fileType"`
	Extension     string              `json:"extension"`
	SizeBytes     int64               `json:"sizeBytes"`
	ThumbnailPath string              `json:"thumbnailPath"`
	StartTime     float64             `json:"startTime"`
	EndTime       float64             `json:"endTime"`
	Score         float32             `json:"score"`
	ModifiedAt    int64               `json:"modifiedAt"`           // Unix timestamp seconds
	MatchKind     string              `json:"matchKind"`            // "filename" | "content" | "both"
	Highlights    []HighlightRangeDTO `json:"highlights,omitempty"` // byte offsets in FileName
}

// ChipDTO represents a single parsed query filter chip for the frontend.
type ChipDTO struct {
	Label      string `json:"label"`
	Field      string `json:"field"`
	Op         string `json:"op"`
	Value      string `json:"value"`      // human-readable string representation
	ClauseKey  string `json:"clauseKey"`  // serialized "field|op|value" for denylist
	ClauseType string `json:"clauseType"` // "must" | "must_not" | "should"
}

// ParseQueryResult is the result of parsing a query into structured filters.
// ErrorCode and Warning are set when LLM query understanding fails non-fatally
// (REQ-014); RetryAfterMs is populated on ERR_QUERY_RATE_LIMITED (REQ-021).
type ParseQueryResult struct {
	Chips         []ChipDTO `json:"chips"`
	SemanticQuery string    `json:"semanticQuery"`
	HasFilters    bool      `json:"hasFilters"`
	CacheHit      bool      `json:"cacheHit"`
	IsOffline     bool      `json:"isOffline"`
	ErrorCode     string    `json:"errorCode,omitempty"`
	Warning       string    `json:"warning,omitempty"`
	RetryAfterMs  int64     `json:"retryAfterMs,omitempty"`
}

// SearchWithFiltersResult wraps search results with an optional relaxation banner.
// ErrorCode is set to a stable apperr code (e.g. "ERR_MODEL_MISMATCH") when the
// backend detected a non-fatal condition the UI should surface; in that case
// Results is empty and the method returns a nil Go error.
// RetryAfterMs is populated when ErrorCode is ERR_RATE_LIMITED so the
// frontend can show a countdown and retry automatically (REQ-021).
type SearchWithFiltersResult struct {
	Results          []SearchResultDTO `json:"results"`
	RelaxationBanner string            `json:"relaxationBanner,omitempty"`
	ErrorCode        string            `json:"errorCode,omitempty"`
	RetryAfterMs     int64             `json:"retryAfterMs,omitempty"`
}

// FailureGroupDTO aggregates per-code failure counts for the frontend.
type FailureGroupDTO struct {
	Code        string   `json:"code"`
	Label       string   `json:"label"`
	Count       int      `json:"count"`
	SampleFiles []string `json:"sampleFiles"`
}

// IndexFailureDTO is a single per-file failure entry sent to the frontend.
type IndexFailureDTO struct {
	Path         string `json:"path"`
	Code         string `json:"code"`
	Message      string `json:"message"`
	Attempts     int    `json:"attempts"`
	LastFailedAt int64  `json:"lastFailedAt"` // unix seconds
}

// EmbedderStatsDTO is the JSON-serializable snapshot of embedder activity
// surfaced on the API Key settings panel.
type EmbedderStatsDTO struct {
	Configured    bool   `json:"configured"`
	Model         string `json:"model"`
	RequestsToday int    `json:"requestsToday"`
	CurrentRPM    int    `json:"currentRpm"`
	MaxRPM        int    `json:"maxRpm"`
	LastEmbedAt   int64  `json:"lastEmbedAt"` // unix seconds; 0 when never
}

// IndexingSettingsDTO is the JSON-serializable view of editable indexing
// settings. The *Saved fields reflect what is persisted (and will take effect
// after restart for fields that are not hot-applied); the *Runtime fields
// reflect the values currently in use by the running pipeline. The frontend
// surfaces a "restart required" hint whenever Saved != Runtime for a field.
type IndexingSettingsDTO struct {
	WorkersSaved     int `json:"workersSaved"`
	WorkersRuntime   int `json:"workersRuntime"`
	RateLimitSaved   int `json:"rateLimitSaved"`
	RateLimitRuntime int `json:"rateLimitRuntime"`
}

// IndexStatusDTO is the JSON-serializable indexing status sent to the frontend.
type IndexStatusDTO struct {
	TotalFiles        int               `json:"totalFiles"`
	IndexedFiles      int               `json:"indexedFiles"`
	FailedFiles       int               `json:"failedFiles"`
	CurrentFile       string            `json:"currentFile"`
	IsRunning         bool              `json:"isRunning"`
	Paused            bool              `json:"paused"`
	QuotaPaused       bool              `json:"quotaPaused"`
	QuotaResumeAt     string            `json:"quotaResumeAt"`
	PendingRetryFiles int               `json:"pendingRetryFiles"`
	FailedFileGroups  []FailureGroupDTO `json:"failedFileGroups"`
}
