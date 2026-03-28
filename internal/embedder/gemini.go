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
	DefaultModel = "gemini-embedding-2-preview"
	TaskRetrieval = "RETRIEVAL_QUERY"
	TaskDocument  = "RETRIEVAL_DOCUMENT"
	defaultRateLimit  = 55
	defaultRateWindow = time.Minute
)

type Embedder struct {
	searchKey *ManagedKey
	indexPool *KeyPool
	model     string
	dims      int32
	limiter   *RateLimiter
	logger    *slog.Logger
}

func NewEmbedder(apiKey string, dims int32, logger *slog.Logger) (*Embedder, error) {
	log := logger.WithGroup("embedder")
	log.Info("initializing embedder (single-key mode)", "model", DefaultModel, "dims", dims)

	mk, err := createManagedKey(apiKey)
	if err != nil {
		return nil, fmt.Errorf("embedder: create client: %w", err)
	}

	log.Info("embedder ready")
	return &Embedder{
		searchKey: mk,
		indexPool: NewKeyPool([]*ManagedKey{mk}, log),
		model:     DefaultModel,
		dims:      dims,
		limiter:   NewRateLimiter(defaultRateLimit, defaultRateWindow),
		logger:    log,
	}, nil
}

func NewMultiKeyEmbedder(apiKeys []string, dims int32, logger *slog.Logger) (*Embedder, error) {
	log := logger.WithGroup("embedder")
	if len(apiKeys) == 0 {
		return nil, fmt.Errorf("embedder: no API keys provided")
	}

	searchMK, err := createManagedKey(apiKeys[0])
	if err != nil {
		return nil, fmt.Errorf("embedder: create search client: %w", err)
	}

	var indexKeys []*ManagedKey
	if len(apiKeys) == 1 {
		indexKeys = []*ManagedKey{searchMK}
		log.Info("initializing embedder (single-key mode)", "model", DefaultModel, "dims", dims)
	} else {
		for i, key := range apiKeys[1:] {
			mk, err := createManagedKey(key)
			if err != nil {
				return nil, fmt.Errorf("embedder: create index client %d: %w", i, err)
			}
			indexKeys = append(indexKeys, mk)
		}
		log.Info("initializing embedder (multi-key mode)",
			"model", DefaultModel,
			"dims", dims,
			"searchKeys", 1,
			"indexKeys", len(indexKeys),
		)
	}

	return &Embedder{
		searchKey: searchMK,
		indexPool: NewKeyPool(indexKeys, log),
		model:     DefaultModel,
		dims:      dims,
		limiter:   NewRateLimiter(defaultRateLimit, defaultRateWindow),
		logger:    log,
	}, nil
}

func NewEmbedderFromEnv(dims int32, logger *slog.Logger) (*Embedder, error) {
	multiKeys := os.Getenv("GEMINI_API_KEYS")
	if multiKeys != "" {
		keys := strings.Split(multiKeys, ",")
		var trimmed []string
		for _, k := range keys {
			k = strings.TrimSpace(k)
			if k != "" {
				trimmed = append(trimmed, k)
			}
		}
		if len(trimmed) > 0 {
			return NewMultiKeyEmbedder(trimmed, dims, logger)
		}
	}

	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		key = os.Getenv("GOOGLE_API_KEY")
	}
	if key == "" {
		return nil, fmt.Errorf("embedder: GEMINI_API_KEY, GOOGLE_API_KEY, or GEMINI_API_KEYS must be set")
	}
	return NewEmbedder(key, dims, logger)
}

func (e *Embedder) IndexPool() *KeyPool {
	return e.indexPool
}

func createManagedKey(apiKey string) (*ManagedKey, error) {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, err
	}
	mk := NewManagedKey(apiKey)
	mk.SetClient(client)
	return mk, nil
}

func is429Error(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "429") ||
		strings.Contains(err.Error(), "RESOURCE_EXHAUSTED")
}

func is4xxNon429Error(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return (strings.Contains(s, "400") || strings.Contains(s, "403") ||
		strings.Contains(s, "404") || strings.Contains(s, "422")) &&
		!is429Error(err)
}

func (e *Embedder) embedWithClient(ctx context.Context, client *genai.Client, contents []*genai.Content, taskType string) ([][]float32, error) {
	config := &genai.EmbedContentConfig{
		OutputDimensionality: genai.Ptr(e.dims),
	}
	if taskType != "" {
		config.TaskType = taskType
	}
	resp, err := client.Models.EmbedContent(ctx, e.model, contents, config)
	if err != nil {
		return nil, err
	}
	result := make([][]float32, len(resp.Embeddings))
	for i, emb := range resp.Embeddings {
		result[i] = emb.Values
	}
	return result, nil
}

func (e *Embedder) embedWithSearch(ctx context.Context, contents []*genai.Content, taskType string) ([][]float32, error) {
	e.limiter.Wait()
	result, err := e.embedWithClient(ctx, e.searchKey.Client, contents, taskType)
	if err != nil {
		if is429Error(err) {
			e.searchKey.RecordFailure()
			e.logger.Error("search key rate limited", "error", err)
		}
		return nil, fmt.Errorf("embedder: search embed: %w", err)
	}
	e.searchKey.Reset()
	return result, nil
}

func (e *Embedder) embedWithRotation(ctx context.Context, contents []*genai.Content, taskType string) ([][]float32, error) {
	tried := 0
	poolSize := e.indexPool.Len()

	for tried < poolSize {
		mk, idx, err := e.indexPool.NextHealthy()
		if err != nil {
			return nil, fmt.Errorf("embedder: %w", err)
		}

		e.limiter.Wait()
		result, callErr := e.embedWithClient(ctx, mk.Client, contents, taskType)
		if callErr == nil {
			mk.Reset()
			return result, nil
		}

		e.logger.Warn("embed call failed", "keyIndex", idx, "error", callErr)

		if is429Error(callErr) {
			mk.RecordFailure()
			e.logger.Warn("key rate limited, rotating", "keyIndex", idx, "state", mk.State())
			tried++
			continue
		}

		if is4xxNon429Error(callErr) {
			return nil, fmt.Errorf("embedder: client error (key %d): %w", idx, callErr)
		}

		// Network/5xx: retry once on same key.
		e.logger.Warn("retrying on same key", "keyIndex", idx, "error", callErr)
		e.limiter.Wait()
		result, retryErr := e.embedWithClient(ctx, mk.Client, contents, taskType)
		if retryErr == nil {
			return result, nil
		}
		return nil, fmt.Errorf("embedder: retry failed (key %d): %w", idx, retryErr)
	}

	return nil, fmt.Errorf("embedder: all %d index keys exhausted", poolSize)
}

func (e *Embedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	content := genai.NewContentFromText(query, genai.RoleUser)
	result, err := e.embedWithSearch(ctx, []*genai.Content{content}, TaskRetrieval)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("embedder: no embeddings returned")
	}
	return result[0], nil
}

func (e *Embedder) EmbedText(ctx context.Context, text string, taskType string) ([]float32, error) {
	content := genai.NewContentFromText(text, genai.RoleUser)
	result, err := e.embedWithRotation(ctx, []*genai.Content{content}, taskType)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("embedder: no embeddings returned")
	}
	return result[0], nil
}

func (e *Embedder) EmbedTexts(ctx context.Context, texts []string, taskType string) ([][]float32, error) {
	contents := make([]*genai.Content, len(texts))
	for i, t := range texts {
		contents[i] = genai.NewContentFromText(t, genai.RoleUser)
	}
	result, err := e.embedWithRotation(ctx, contents, taskType)
	if err != nil {
		return nil, err
	}
	if len(result) != len(texts) {
		return nil, fmt.Errorf("embedder: expected %d embeddings, got %d", len(texts), len(result))
	}
	return result, nil
}

func (e *Embedder) EmbedBytes(ctx context.Context, data []byte, mimeType string) ([]float32, error) {
	content := genai.NewContentFromBytes(data, mimeType, genai.RoleUser)
	result, err := e.embedWithRotation(ctx, []*genai.Content{content}, "")
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("embedder: no embeddings returned")
	}
	return result[0], nil
}

func (e *Embedder) EmbedDocument(ctx context.Context, text string) ([]float32, error) {
	return e.EmbedText(ctx, text, TaskDocument)
}
