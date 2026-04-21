package embedder

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"google.golang.org/genai"
)

const (
	DefaultModel      = "gemini-embedding-2-preview"
	defaultRateWindow = time.Minute

	taskTypeRetrievalDocument = "RETRIEVAL_DOCUMENT"
	taskTypeRetrievalQuery    = "RETRIEVAL_QUERY"
)

// GeminiConfig holds the tunable parameters for the Gemini embedder.
type GeminiConfig struct {
	APIKey                string
	Model                 string
	Dimensions            int
	RateLimitPerMinute    int
	BatchSize             int
	RetryMaxAttempts      int
	RetryInitialBackoffMs int
	RetryMaxBackoffMs     int
}

// DefaultGeminiConfig returns the historical defaults.
func DefaultGeminiConfig() GeminiConfig {
	return GeminiConfig{
		Model:                 DefaultModel,
		Dimensions:            768,
		RateLimitPerMinute:    55,
		BatchSize:             100,
		RetryMaxAttempts:      5,
		RetryInitialBackoffMs: 2000,
		RetryMaxBackoffMs:     60000,
	}
}

func (c *GeminiConfig) fillDefaults() {
	def := DefaultGeminiConfig()
	if c.Model == "" {
		c.Model = def.Model
	}
	if c.Dimensions == 0 {
		c.Dimensions = def.Dimensions
	}
	if c.RateLimitPerMinute <= 0 {
		c.RateLimitPerMinute = def.RateLimitPerMinute
	}
	if c.BatchSize <= 0 {
		c.BatchSize = def.BatchSize
	}
	if c.RetryMaxAttempts < 0 {
		c.RetryMaxAttempts = def.RetryMaxAttempts
	}
	if c.RetryInitialBackoffMs <= 0 {
		c.RetryInitialBackoffMs = def.RetryInitialBackoffMs
	}
	if c.RetryMaxBackoffMs <= 0 {
		c.RetryMaxBackoffMs = def.RetryMaxBackoffMs
	}
}

// ChunkInput represents one chunk to embed in a batch call.
type ChunkInput struct {
	Title    string
	Text     string
	MIMEType string
	Data     []byte
}

// embedFunc performs a single EmbedContent API call. Injected via constructor
// so tests can exercise the retry/batching logic without a network client.
type embedFunc func(ctx context.Context, model string, contents []*genai.Content, config *genai.EmbedContentConfig) ([][]float32, error)

// GeminiEmbedder is the live Gemini-API-backed Embedder implementation.
type GeminiEmbedder struct {
	client       *genai.Client
	doer         embedFunc
	model        string
	dims         int32
	maxBatchSize int
	maxRetries   int
	initialDelay time.Duration
	maxDelay     time.Duration
	limiter      *RateLimiter
	logger       *slog.Logger
}

var _ Embedder = (*GeminiEmbedder)(nil)

func defaultEmbedFn(client *genai.Client) embedFunc {
	return func(ctx context.Context, model string, contents []*genai.Content, config *genai.EmbedContentConfig) ([][]float32, error) {
		resp, err := client.Models.EmbedContent(ctx, model, contents, config)
		if err != nil {
			return nil, err
		}
		result := make([][]float32, len(resp.Embeddings))
		for i, emb := range resp.Embeddings {
			result[i] = emb.Values
		}
		return result, nil
	}
}

// NewEmbedder constructs a GeminiEmbedder wired to the live genai SDK using
// the default Gemini config with the given api key and dimensions. Retained
// for call sites that have not yet adopted NewGeminiEmbedderFromConfig.
func NewEmbedder(apiKey string, dims int32, logger *slog.Logger) (*GeminiEmbedder, error) {
	cfg := DefaultGeminiConfig()
	cfg.APIKey = apiKey
	cfg.Dimensions = int(dims)
	return NewGeminiEmbedderFromConfig(cfg, logger)
}

// NewGeminiEmbedderFromConfig constructs a GeminiEmbedder from cfg.
func NewGeminiEmbedderFromConfig(cfg GeminiConfig, logger *slog.Logger) (*GeminiEmbedder, error) {
	cfg.fillDefaults()
	log := logger.WithGroup("embedder")
	log.Info("initializing embedder",
		"model", cfg.Model,
		"dims", cfg.Dimensions,
		"batch_size", cfg.BatchSize,
		"rate_limit", cfg.RateLimitPerMinute,
	)

	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  cfg.APIKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("embedder: create client: %w", err)
	}

	log.Info("embedder ready")
	return &GeminiEmbedder{
		client:       client,
		doer:         defaultEmbedFn(client),
		model:        cfg.Model,
		dims:         int32(cfg.Dimensions),
		maxBatchSize: cfg.BatchSize,
		maxRetries:   cfg.RetryMaxAttempts,
		initialDelay: time.Duration(cfg.RetryInitialBackoffMs) * time.Millisecond,
		maxDelay:     time.Duration(cfg.RetryMaxBackoffMs) * time.Millisecond,
		limiter:      NewRateLimiter(cfg.RateLimitPerMinute, defaultRateWindow),
		logger:       log,
	}, nil
}

// NewEmbedderFromEnv constructs a GeminiEmbedder from GEMINI_API_KEY or
// GOOGLE_API_KEY environment variables.
func NewEmbedderFromEnv(dims int32, logger *slog.Logger) (*GeminiEmbedder, error) {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		key = os.Getenv("GOOGLE_API_KEY")
	}
	if key == "" {
		return nil, fmt.Errorf("embedder: GEMINI_API_KEY or GOOGLE_API_KEY must be set")
	}
	return NewEmbedder(key, dims, logger)
}

// newWithFunc builds a GeminiEmbedder with a caller-provided doer. Intended
// for in-package tests that exercise the retry/batching paths without the
// genai SDK.
func newWithFunc(doer embedFunc, dims int32, logger *slog.Logger) *GeminiEmbedder {
	def := DefaultGeminiConfig()
	return &GeminiEmbedder{
		doer:         doer,
		model:        def.Model,
		dims:         dims,
		maxBatchSize: def.BatchSize,
		maxRetries:   def.RetryMaxAttempts,
		initialDelay: time.Duration(def.RetryInitialBackoffMs) * time.Millisecond,
		maxDelay:     time.Duration(def.RetryMaxBackoffMs) * time.Millisecond,
		limiter:      NewRateLimiter(def.RateLimitPerMinute, defaultRateWindow),
		logger:       logger,
	}
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "429") ||
		strings.Contains(s, "503") ||
		strings.Contains(s, "RESOURCE_EXHAUSTED")
}

var retryDelayRe = regexp.MustCompile(`retry_delay:\{seconds:(\d+)`)

func parseRetryAfter(err error) time.Duration {
	if err == nil {
		return 0
	}
	s := err.Error()
	if m := retryDelayRe.FindStringSubmatch(s); len(m) == 2 {
		secs, parseErr := strconv.Atoi(m[1])
		if parseErr == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 0
}

func (e *GeminiEmbedder) embed(ctx context.Context, contents []*genai.Content, taskType string) ([][]float32, error) {
	config := &genai.EmbedContentConfig{
		OutputDimensionality: genai.Ptr(e.dims),
		TaskType:             taskType,
	}

	delay := e.initialDelay
	var lastErr error

	for attempt := 0; attempt <= e.maxRetries; attempt++ {
		if err := e.limiter.Wait(ctx); err != nil {
			return nil, err
		}

		result, err := e.doer(ctx, e.model, contents, config)
		if err == nil {
			return result, nil
		}

		lastErr = err

		if !isRetryable(err) {
			return nil, fmt.Errorf("embedder: embed: %w", err)
		}

		if attempt == e.maxRetries {
			break
		}

		if d := parseRetryAfter(err); d > 0 {
			e.limiter.PauseUntil(time.Now().Add(d))
			e.logger.Warn("rate limited with retry-after, pausing",
				"attempt", attempt+1,
				"retryAfter", d,
			)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(d):
			}
			delay = e.initialDelay
			continue
		}

		e.logger.Warn("retryable error, backing off",
			"attempt", attempt+1,
			"maxRetries", e.maxRetries,
			"delay", delay,
			"error", err,
		)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}

		delay *= 2
		if delay > e.maxDelay {
			delay = e.maxDelay
		}
	}

	return nil, fmt.Errorf("embedder: all %d retries exhausted: %w", e.maxRetries, lastErr)
}

func (e *GeminiEmbedder) embedOne(ctx context.Context, contents []*genai.Content, taskType string) ([]float32, error) {
	result, err := e.embed(ctx, contents, taskType)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("embedder: no embeddings returned")
	}
	return result[0], nil
}

// ModelID returns the Gemini model identifier in use.
func (e *GeminiEmbedder) ModelID() string { return e.model }

// Dimensions returns the embedding dimensionality.
func (e *GeminiEmbedder) Dimensions() int { return int(e.dims) }

// Limiter returns the rate limiter used by the embedder.
func (e *GeminiEmbedder) Limiter() *RateLimiter { return e.limiter }

// Client returns the underlying genai.Client used by the embedder.
func (e *GeminiEmbedder) Client() *genai.Client { return e.client }

// EmbedQuery embeds a search query using the inline instruction format
// required by gemini-embedding-2-preview.
func (e *GeminiEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	instructed := fmt.Sprintf("task: search result | query: %s", query)
	content := genai.NewContentFromText(instructed, genai.RoleUser)
	return e.embedOne(ctx, []*genai.Content{content}, taskTypeRetrievalQuery)
}

// EmbedDocumentWithTitle embeds a text document with a title using the inline
// instruction format required by gemini-embedding-2-preview.
func (e *GeminiEmbedder) EmbedDocumentWithTitle(ctx context.Context, title, text string) ([]float32, error) {
	instructed := fmt.Sprintf("title: %s | text: %s", title, text)
	content := genai.NewContentFromText(instructed, genai.RoleUser)
	return e.embedOne(ctx, []*genai.Content{content}, taskTypeRetrievalDocument)
}

// EmbedBytes embeds binary content (image, video, audio) with a document
// instruction Part alongside the binary data.
func (e *GeminiEmbedder) EmbedBytes(ctx context.Context, data []byte, mimeType, title string) ([]float32, error) {
	if title == "" {
		title = "none"
	}
	instruction := genai.NewPartFromText(fmt.Sprintf("title: %s | text: embedded media", title))
	media := genai.NewPartFromBytes(data, mimeType)
	content := genai.NewContentFromParts([]*genai.Part{instruction, media}, genai.RoleUser)
	return e.embedOne(ctx, []*genai.Content{content}, taskTypeRetrievalDocument)
}

// EmbedBatch embeds multiple chunks in batched API calls (up to maxBatchSize per call).
// Returns embeddings in the same order as inputs. Empty input returns nil, nil.
func (e *GeminiEmbedder) EmbedBatch(ctx context.Context, chunks []ChunkInput) ([][]float32, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	result := make([][]float32, 0, len(chunks))
	for start := 0; start < len(chunks); start += e.maxBatchSize {
		end := start + e.maxBatchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		batch := chunks[start:end]
		contents := make([]*genai.Content, len(batch))
		for i, c := range batch {
			if len(c.Data) > 0 {
				instruction := genai.NewPartFromText(fmt.Sprintf("title: %s | text: embedded media", c.Title))
				media := genai.NewPartFromBytes(c.Data, c.MIMEType)
				contents[i] = genai.NewContentFromParts([]*genai.Part{instruction, media}, genai.RoleUser)
			} else {
				instructed := fmt.Sprintf("title: %s | text: %s", c.Title, c.Text)
				contents[i] = genai.NewContentFromText(instructed, genai.RoleUser)
			}
		}
		vecs, err := e.embed(ctx, contents, taskTypeRetrievalDocument)
		if err != nil {
			return nil, err
		}
		if len(vecs) != len(batch) {
			return nil, fmt.Errorf("embedder: EmbedBatch cardinality mismatch: sent %d inputs, got %d vectors", len(batch), len(vecs))
		}
		result = append(result, vecs...)
	}
	return result, nil
}
