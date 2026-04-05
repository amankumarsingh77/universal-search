package embedder

import (
	"context"
	"fmt"
	"log/slog"
	"os"
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
)

type Embedder struct {
	client  *genai.Client
	model   string
	dims    int32
	limiter *RateLimiter
	logger  *slog.Logger
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
	return &Embedder{
		client:  client,
		model:   DefaultModel,
		dims:    dims,
		limiter: NewRateLimiter(defaultRateLimit, defaultRateWindow),
		logger:  log,
	}, nil
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

func (e *Embedder) embed(ctx context.Context, contents []*genai.Content) ([][]float32, error) {
	config := &genai.EmbedContentConfig{
		OutputDimensionality: genai.Ptr(e.dims),
	}

	delay := initialDelay
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		e.limiter.Wait()

		resp, err := e.client.Models.EmbedContent(ctx, e.model, contents, config)
		if err == nil {
			result := make([][]float32, len(resp.Embeddings))
			for i, emb := range resp.Embeddings {
				result[i] = emb.Values
			}
			return result, nil
		}

		lastErr = err

		if !isRetryable(err) {
			return nil, fmt.Errorf("embedder: embed: %w", err)
		}

		if attempt == maxRetries {
			break
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

func (e *Embedder) embedOne(ctx context.Context, contents []*genai.Content) ([]float32, error) {
	result, err := e.embed(ctx, contents)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("embedder: no embeddings returned")
	}
	return result[0], nil
}

// EmbedQuery embeds a search query using the inline instruction format
// required by gemini-embedding-2-preview.
func (e *Embedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	instructed := fmt.Sprintf("task: search result | query: %s", query)
	content := genai.NewContentFromText(instructed, genai.RoleUser)
	return e.embedOne(ctx, []*genai.Content{content})
}

// EmbedDocumentWithTitle embeds a text document with a title using the inline
// instruction format required by gemini-embedding-2-preview.
func (e *Embedder) EmbedDocumentWithTitle(ctx context.Context, title, text string) ([]float32, error) {
	instructed := fmt.Sprintf("title: %s | text: %s", title, text)
	content := genai.NewContentFromText(instructed, genai.RoleUser)
	return e.embedOne(ctx, []*genai.Content{content})
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
	return e.embedOne(ctx, []*genai.Content{content})
}
