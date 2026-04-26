// Package config defines the application's TOML-backed configuration surface,
// loader, merge semantics, and schema-version migrators.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the root application configuration.
type Config struct {
	SchemaVersion  int                  `toml:"schema_version"`
	App            AppConfig            `toml:"app"`
	Indexing       IndexingConfig       `toml:"indexing"`
	Embedder       EmbedderConfig       `toml:"embedder"`
	HNSW           HNSWConfig           `toml:"hnsw"`
	Search         SearchConfig         `toml:"search"`
	Query          QueryConfig          `toml:"query"`
	Chunking       ChunkingConfig       `toml:"chunking"`
	Logging        LoggingConfig        `toml:"logging"`
	FilenameSearch FilenameSearchConfig `toml:"filename_search"`
}

// AppConfig holds window and lifecycle settings.
type AppConfig struct {
	Hotkey            string `toml:"hotkey"`
	Theme             string `toml:"theme"`
	ShutdownTimeoutMs int    `toml:"shutdown_timeout_ms"`
}

// IndexingConfig holds indexing pipeline tuning parameters.
type IndexingConfig struct {
	Workers            int `toml:"workers"`
	JobQueueSize       int `toml:"job_queue_size"`
	SaveEveryN         int `toml:"save_every_n"`
	RateLimitPerMinute int `toml:"rate_limit_per_minute"`
	BatchSize          int `toml:"batch_size"`
}

// EmbedderConfig selects and configures the embedding provider.
type EmbedderConfig struct {
	Provider   string               `toml:"provider"`
	Model      string               `toml:"model"`
	Dimensions int                  `toml:"dimensions"`
	Gemini     EmbedderGeminiConfig `toml:"gemini"`
}

// EmbedderGeminiConfig holds Gemini-specific embedder tuning.
type EmbedderGeminiConfig struct {
	RateLimitPerMinute    int `toml:"rate_limit_per_minute"`
	BatchSize             int `toml:"batch_size"`
	RetryMaxAttempts      int `toml:"retry_max_attempts"`
	RetryInitialBackoffMs int `toml:"retry_initial_backoff_ms"`
	RetryMaxBackoffMs     int `toml:"retry_max_backoff_ms"`
}

// HNSWConfig holds parameters for the HNSW vector index.
type HNSWConfig struct {
	M              int     `toml:"m"`
	Ml             float64 `toml:"ml"`
	EfSearch       int     `toml:"ef_search"`
	EfConstruction int     `toml:"ef_construction"`
	Distance       string  `toml:"distance"`
}

// SearchConfig holds search engine tuning knobs.
type SearchConfig struct {
	BruteForceThreshold    int      `toml:"brute_force_threshold"`
	OverFetchMultiplier    int      `toml:"over_fetch_multiplier"`
	RecencyBoostMultiplier float64  `toml:"recency_boost_multiplier"`
	RecencyWindowDays      int      `toml:"recency_window_days"`
	RelaxationEnabled      bool     `toml:"relaxation_enabled"`
	RelaxationDropOrder    []string `toml:"relaxation_drop_order"`
	FilenameMergeEnabled   bool     `toml:"filename_merge_enabled"`
}

// QueryConfig holds NL query understanding parameters.
type QueryConfig struct {
	LLMModel         string `toml:"llm_model"`
	LLMTimeoutMs     int    `toml:"llm_timeout_ms"`
	LLMMaxRetries    int    `toml:"llm_max_retries"`
	TriggerMinTokens int    `toml:"trigger_min_tokens"`
	TriggerMaxChars  int    `toml:"trigger_max_chars"`
	NLEnabled        bool   `toml:"nl_enabled"`
	CacheTTLHours    int    `toml:"cache_ttl_hours"`
}

// ChunkingConfig holds chunking strategy parameters.
type ChunkingConfig struct {
	TextMaxTokens       int `toml:"text_max_tokens"`
	TextOverlapTokens   int `toml:"text_overlap_tokens"`
	VideoChunkSeconds   int `toml:"video_chunk_seconds"`
	VideoOverlapSeconds int `toml:"video_overlap_seconds"`
}

// LoggingConfig holds logger settings.
type LoggingConfig struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
}

// FilenameSearchConfig holds tuning knobs for the filename-search pipeline.
type FilenameSearchConfig struct {
	Enabled    bool    `toml:"enabled"`
	FuzzyTopN  int     `toml:"fuzzy_top_n"`
	RrfK       int     `toml:"rrf_k"`
	ExactBonus float64 `toml:"exact_bonus"`
}

// Warning describes a non-fatal config issue surfaced during loading.
type Warning struct {
	Key     string
	Message string
}

// ErrInvalidConfig wraps a TOML parse or decode failure.
var ErrInvalidConfig = errors.New("invalid config")

// Resolve returns the platform-specific config file path.
func Resolve() (string, error) {
	if runtime.GOOS == "windows" {
		appdata := os.Getenv("APPDATA")
		return filepath.Join(appdata, "findo", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "findo", "config.toml"), nil
}

// Load reads a config TOML file, merges it over embedded defaults, runs any
// schema migrators whose from-version <= file's schema_version < CurrentSchemaVersion,
// and returns the merged Config plus a list of non-fatal warnings.
//
// A missing file returns DefaultConfig() with no warnings and no error. A
// malformed TOML returns an error wrapping the file path.
func Load(path string) (*Config, []Warning, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil, nil
		}
		return nil, nil, fmt.Errorf("%w: %s: %v", ErrInvalidConfig, path, err)
	}

	var doc map[string]any
	if _, err := toml.Decode(string(raw), &doc); err != nil {
		return nil, nil, fmt.Errorf("%w: %s: %v", ErrInvalidConfig, path, err)
	}

	version := 0
	if v, ok := doc["schema_version"]; ok {
		if n, ok := toInt(v); ok {
			version = int(n)
		}
	}

	migrated := false
	if version < CurrentSchemaVersion {
		newDoc, newVersion, err := RunMigrations(doc, version)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %s: migration: %v", ErrInvalidConfig, path, err)
		}
		doc = newDoc
		version = newVersion
		migrated = true
	}

	body, err := encodeDocToTOML(doc)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s: re-encode after migration: %v", ErrInvalidConfig, path, err)
	}

	var override Config
	meta, err := toml.Decode(body, &override)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s: %v", ErrInvalidConfig, path, err)
	}

	warnings := collectUnknownKeyWarnings(meta)

	cfg := DefaultConfig()
	merge(cfg, &override)
	cfg.SchemaVersion = version

	if migrated {
		if err := writeDocFile(path, doc); err != nil {
			return nil, nil, fmt.Errorf("%w: %s: rewrite after migration: %v", ErrInvalidConfig, path, err)
		}
	}

	return cfg, warnings, nil
}

func collectUnknownKeyWarnings(meta toml.MetaData) []Warning {
	var warnings []Warning
	for _, key := range meta.Undecoded() {
		joined := strings.Join(key, ".")
		warnings = append(warnings, Warning{
			Key:     joined,
			Message: fmt.Sprintf("unknown config key %q", joined),
		})
	}
	return warnings
}

func encodeDocToTOML(doc map[string]any) (string, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(doc); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func writeDocFile(path string, doc map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(doc); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func toInt(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case float64:
		return int64(x), true
	}
	return 0, false
}
