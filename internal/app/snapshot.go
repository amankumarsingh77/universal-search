package app

import (
	"findo/internal/embedder"
)

// snapshotEmbedderState returns the currently installed embedder and LLM parser
// under a single apiKeyMu read-lock, so concurrent SetGeminiAPIKey calls can't
// tear a pair apart.
func (a *App) snapshotEmbedderState() (embedder.Embedder, llmQueryParser) {
	a.apiKeyMu.RLock()
	defer a.apiKeyMu.RUnlock()
	return a.embedder, a.llmParser
}
