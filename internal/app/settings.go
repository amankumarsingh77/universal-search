package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"universal-search/internal/apperr"
	"universal-search/internal/desktop"
	"universal-search/internal/embedder"
	"universal-search/internal/query"
	"universal-search/internal/search"
)

// GetSetting returns the persisted value for a key-value setting, or empty if unset.
func (a *App) GetSetting(key string) string {
	val, _ := a.store.GetSetting(key, "")
	return val
}

// SetSetting updates a key-value setting and applies any side effects (e.g. hotkey rebind).
func (a *App) SetSetting(key, value string) error {
	if key == "global_hotkey" && a.hotkeyMgr != nil {
		if err := a.hotkeyMgr.ChangeHotkey(value); err != nil {
			return apperr.Wrap(apperr.ErrConfigInvalid.Code, "could not update hotkey", err)
		}
		return nil
	}
	if err := a.store.SetSetting(key, value); err != nil {
		return apperr.Wrap(apperr.ErrStoreLocked.Code, "could not save setting", err)
	}
	return nil
}

// GetHotkeyString returns the current global hotkey as a human-readable string (e.g. "⌘⇧Space").
func (a *App) GetHotkeyString() string {
	combo, _ := a.store.GetSetting("global_hotkey", desktop.DefaultHotkey())
	mods, key, err := desktop.ParseHotkey(combo)
	if err != nil {
		return combo
	}
	return desktop.HumanReadableHotkey(mods, key)
}

// GetOnboarded returns true if the user has already seen the onboarding overlay.
func (a *App) GetOnboarded() bool {
	val, _ := a.store.GetSetting("onboarded", "")
	return val == "true"
}

// MarkOnboarded records that the user has dismissed the onboarding overlay.
func (a *App) MarkOnboarded() error {
	return a.store.SetSetting("onboarded", "true")
}

// SetGeminiAPIKey validates the supplied Gemini API key by making a real test
// embed call, then — if valid — persists it to the settings store and hot-swaps
// the live embedder and indexing pipeline. Returns a non-nil error on any failure;
// state is unchanged on failure.
func (a *App) SetGeminiAPIKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return apperr.New(apperr.ErrConfigInvalid.Code, "API key must not be empty")
	}

	a.apiKeyMu.Lock()
	defer a.apiKeyMu.Unlock()

	// Track whether we paused indexing so we can restore state.
	wasPaused := a.pipeline.Status().Paused
	if !wasPaused {
		a.pipeline.Pause()
	}

	restore := func() {
		if !wasPaused {
			a.pipeline.Resume()
		}
	}

	// Create a temporary embedder — never stored unless validation passes.
	tmpEmb, err := embedder.NewEmbedder(key, 768, a.logger)
	if err != nil {
		restore()
		return apperr.Wrap(apperr.ErrConfigInvalid.Code, "failed to create embedder", err)
	}

	// Validate with a real test call, 10s timeout.
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	if _, err := tmpEmb.EmbedQuery(ctx, "test"); err != nil {
		restore()
		return apperr.Wrap(apperr.ErrEmbedFailed.Code, "API key validation failed", err)
	}

	// Validation passed — commit atomically.
	if err := a.store.SetSetting("gemini_api_key", key); err != nil {
		restore()
		return apperr.Wrap(apperr.ErrStoreLocked.Code, "failed to persist API key", err)
	}
	a.embedder = tmpEmb
	a.pipeline.SetEmbedder(tmpEmb)
	// Rebuild LLM parser so it uses the new client from the hot-swapped embedder.
	a.llmParser = query.NewLLMParserWithConfig(tmpEmb.Client(), tmpEmb.Limiter(), query.LLMConfig{
		Model:      a.cfg.Query.LLMModel,
		TimeoutMs:  a.cfg.Query.LLMTimeoutMs,
		MaxRetries: a.cfg.Query.LLMMaxRetries,
	})

	restore()

	a.logger.Info("Gemini API key updated", "keyLen", len(key))
	return nil
}

// GetHasGeminiKey returns true if a Gemini API key is currently configured.
func (a *App) GetHasGeminiKey() bool {
	return a.embedder != nil
}

// GetNLQueryEnabled returns the current nl_query_enabled setting.
func (a *App) GetNLQueryEnabled() bool {
	return a.isNLQueryEnabled()
}

// SetNLQueryEnabled sets the nl_query_enabled setting.
func (a *App) SetNLQueryEnabled(enabled bool) error {
	if a.store == nil {
		return apperr.New(apperr.ErrInternal.Code, "store not initialized")
	}
	val := "false"
	if enabled {
		val = "true"
	}
	if err := a.store.SetSetting("nl_query_enabled", val); err != nil {
		return apperr.Wrap(apperr.ErrStoreLocked.Code, "could not save nl_query_enabled", err)
	}
	return nil
}

// getBruteForceThreshold returns the configured brute_force_threshold setting,
// falling back to DefaultBruteForceThreshold if unset or invalid.
func (a *App) getBruteForceThreshold() int {
	if a.store == nil {
		return search.DefaultBruteForceThreshold
	}
	val, err := a.store.GetSetting("brute_force_threshold", "")
	if err != nil || val == "" {
		return search.DefaultBruteForceThreshold
	}
	n, err := strconv.Atoi(val)
	if err != nil || n <= 0 {
		return search.DefaultBruteForceThreshold
	}
	return n
}
