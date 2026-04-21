package app

// SearchResultDTO is the JSON-serializable search result sent to the frontend.
type SearchResultDTO struct {
	FilePath      string  `json:"filePath"`
	FileName      string  `json:"fileName"`
	FileType      string  `json:"fileType"`
	Extension     string  `json:"extension"`
	SizeBytes     int64   `json:"sizeBytes"`
	ThumbnailPath string  `json:"thumbnailPath"`
	StartTime     float64 `json:"startTime"`
	EndTime       float64 `json:"endTime"`
	Score         float32 `json:"score"`
	ModifiedAt    int64   `json:"modifiedAt"` // Unix timestamp seconds
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
type ParseQueryResult struct {
	Chips         []ChipDTO `json:"chips"`
	SemanticQuery string    `json:"semanticQuery"`
	HasFilters    bool      `json:"hasFilters"`
	CacheHit      bool      `json:"cacheHit"`
	IsOffline     bool      `json:"isOffline"`
}

// SearchWithFiltersResult wraps search results with an optional relaxation banner.
// ErrorCode is set to a stable apperr code (e.g. "ERR_MODEL_MISMATCH") when the
// backend detected a non-fatal condition the UI should surface; in that case
// Results is empty and the method returns a nil Go error.
type SearchWithFiltersResult struct {
	Results          []SearchResultDTO `json:"results"`
	RelaxationBanner string            `json:"relaxationBanner,omitempty"`
	ErrorCode        string            `json:"errorCode,omitempty"`
}

// IndexStatusDTO is the JSON-serializable indexing status sent to the frontend.
type IndexStatusDTO struct {
	TotalFiles    int    `json:"totalFiles"`
	IndexedFiles  int    `json:"indexedFiles"`
	FailedFiles   int    `json:"failedFiles"`
	CurrentFile   string `json:"currentFile"`
	IsRunning     bool   `json:"isRunning"`
	Paused        bool   `json:"paused"`
	QuotaPaused   bool   `json:"quotaPaused"`
	QuotaResumeAt string `json:"quotaResumeAt"`
}
