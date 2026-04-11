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
	defaultRateLimit  = 55
	defaultRateWindow = time.Minute

	maxRetries   = 5
	initialDelay = 2 * time.Second
	maxDelay     = 60 * time.Second

	maxBatchSize = 100

	taskTypeRetrievalDocument = "RETRIEVAL_DOCUMENT"
	taskTypeRetrievalQuery    = "RETRIEVAL_QUERY"
)

// ChunkInput represents one chunk to embed in a batch call.
type ChunkInput struct {
	Title    string
	Text     string // non-empty for text chunks
	MIMEType string // non-empty for binary chunks
	Data     []byte // non-empty for binary chunks
}

// embedFunc is the low-level hook that performs a single EmbedContent API
// call. The real implementation delegates to the genai SDK; tests replace it
// with a fake to drive (*Embedder).EmbedBatch without a network client.
type embedFunc func(ctx context.Context, model string, contents []*genai.Content, config *genai.EmbedContentConfig) ([][]float32, error)

type Embedder struct {
	client   *genai.Client
	embedFn  embedFunc
	model    string
	dims     int32
	limiter  *RateLimiter
	logger   *slog.Logger
}

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

func NewEmbedder(apiKey string, dims int32, logger *slog.Logger) (*Embedder, error) {
	log := logger.WithGroup("embedder")
	log.Info("initializing embedder", "model", DefaultModel, "dims", dims)

	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("embedder: create client: %w", err)
	}

	log.Info("embedder ready")
	e := &Embedder{
		client:  client,
		model:   DefaultModel,
		dims:    dims,
		limiter: NewRateLimiter(defaultRateLimit, defaultRateWindow),
		logger:  log,
	}
	e.embedFn = defaultEmbedFn(client)
	return e, nil
}

func NewEmbedderFromEnv(dims int32, logger *slog.Logger) (*Embedder, error) {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		key = os.Getenv("GOOGLE_API_KEY")
	}
	if key == "" {
		return nil, fmt.Errorf("embedder: GEMINI_API_KEY or GOOGLE_API_KEY must be set")
	}
	return NewEmbedder(key, dims, logger)
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

// retryDelayRe matches patterns like retry_delay:{seconds:30} or retry_delay:{seconds:30 nanos:0}
var retryDelayRe = regexp.MustCompile(`retry_delay:\{seconds:(\d+)`)

// parseRetryAfter tries to extract a retry duration from the error string.
// It looks for patterns like "retry_delay:{seconds:30}". Returns 0 if not found.
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

func (e *Embedder) embed(ctx context.Context, contents []*genai.Content, taskType string) ([][]float32, error) {
	config := &genai.EmbedContentConfig{
		OutputDimensionality: genai.Ptr(e.dims),
		TaskType:             taskType,
	}

	delay := initialDelay
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := e.limiter.Wait(ctx); err != nil {
			return nil, err
		}

		result, err := e.embedFn(ctx, e.model, contents, config)
		if err == nil {
			return result, nil
		}

		lastErr = err

		if !isRetryable(err) {
			return nil, fmt.Errorf("embedder: embed: %w", err)
		}

		if attempt == maxRetries {
			break
		}

		// Check for a server-provided retry delay (429 with Retry-After).
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
			delay = initialDelay // reset backoff after explicit retry-after
			continue
		}

		e.logger.Warn("retryable error, backing off",
			"attempt", attempt+1,
			"maxRetries", maxRetries,
			"delay", delay,
			"error", err,
		)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}

		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}

	return nil, fmt.Errorf("embedder: all %d retries exhausted: %w", maxRetries, lastErr)
}

func (e *Embedder) embedOne(ctx context.Context, contents []*genai.Content, taskType string) ([]float32, error) {
	result, err := e.embed(ctx, contents, taskType)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("embedder: no embeddings returned")
	}
	return result[0], nil
}

// Limiter returns the rate limiter used by the embedder.
func (e *Embedder) Limiter() *RateLimiter {
	return e.limiter
}

// EmbedQuery embeds a search query using the inline instruction format
// required by gemini-embedding-2-preview.
func (e *Embedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	instructed := fmt.Sprintf("task: search result | query: %s", query)
	content := genai.NewContentFromText(instructed, genai.RoleUser)
	return e.embedOne(ctx, []*genai.Content{content}, taskTypeRetrievalQuery)
}

// EmbedDocumentWithTitle embeds a text document with a title using the inline
// instruction format required by gemini-embedding-2-preview.
func (e *Embedder) EmbedDocumentWithTitle(ctx context.Context, title, text string) ([]float32, error) {
	instructed := fmt.Sprintf("title: %s | text: %s", title, text)
	content := genai.NewContentFromText(instructed, genai.RoleUser)
	return e.embedOne(ctx, []*genai.Content{content}, taskTypeRetrievalDocument)
}

// EmbedBytes embeds binary content (image, video, audio) with a document
// instruction Part alongside the binary data.
func (e *Embedder) EmbedBytes(ctx context.Context, data []byte, mimeType, title string) ([]float32, error) {
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
func (e *Embedder) EmbedBatch(ctx context.Context, chunks []ChunkInput) ([][]float32, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	result := make([][]float32, 0, len(chunks))
	for start := 0; start < len(chunks); start += maxBatchSize {
		end := start + maxBatchSize
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
