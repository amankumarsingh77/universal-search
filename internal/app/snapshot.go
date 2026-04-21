package app

import (
	"universal-search/internal/embedder"
	"universal-search/internal/query"
)

// snapshotEmbedderState returns the currently installed embedder and LLM parser
// under a single apiKeyMu read-lock, so concurrent SetGeminiAPIKey calls can't
// tear a pair apart.
func (a *App) snapshotEmbedderState() (embedder.Embedder, *query.LLMParser) {
	a.apiKeyMu.RLock()
	defer a.apiKeyMu.RUnlock()
	return a.embedder, a.llmParser
}
