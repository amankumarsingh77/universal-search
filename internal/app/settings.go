package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"findo/internal/apperr"
	"findo/internal/desktop"
	"findo/internal/embedder"
	"findo/internal/query"
	"findo/internal/search"
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

// GetEmbedderStats returns a snapshot of the embedder's recent activity for
// the API Key settings panel. Safe to call when no embedder is configured —
// returns an empty DTO with Configured=false.
func (a *App) GetEmbedderStats() EmbedderStatsDTO {
	emb, _ := a.snapshotEmbedderState()
	if emb == nil {
		return EmbedderStatsDTO{}
	}
	s := emb.Stats()
	var lastUnix int64
	if !s.LastEmbedAt.IsZero() {
		lastUnix = s.LastEmbedAt.Unix()
	}
	return EmbedderStatsDTO{
		Configured:    true,
		Model:         emb.ModelID(),
		RequestsToday: s.RequestsToday,
		CurrentRPM:    s.CurrentRPM,
		MaxRPM:        s.MaxRPM,
		LastEmbedAt:   lastUnix,
	}
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

// applyPersistedIndexingOverrides mutates a.cfg with values from the settings
// KV store, when present and valid. Called once during startup before the
// pipeline and embedder are constructed so the overrides flow into them.
// Out-of-range or unparseable values are ignored (cfg defaults stand).
func (a *App) applyPersistedIndexingOverrides() {
	if v, err := a.store.GetSetting(settingIndexingWorkers, ""); err == nil && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= indexingWorkersMin && n <= indexingWorkersMax {
			a.cfg.Indexing.Workers = n
		}
	}
	if v, err := a.store.GetSetting(settingIndexingRateLimit, ""); err == nil && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= indexingRateLimitMin && n <= indexingRateLimitMax {
			a.cfg.Embedder.Gemini.RateLimitPerMinute = n
		}
	}
}

// Indexing settings keys persisted in the settings KV table.
const (
	settingIndexingWorkers   = "indexing.workers"
	settingIndexingRateLimit = "indexing.rate_limit_per_minute"

	indexingWorkersMin   = 1
	indexingWorkersMax   = 32
	indexingRateLimitMin = 1
	indexingRateLimitMax = 10000
)

// GetIndexingSettings returns the current saved and runtime values for the
// editable indexing settings. WorkersRuntime reflects the worker count the
// pipeline was started with (changing it requires an app restart);
// RateLimitRuntime reflects the limiter's currently configured maximum.
func (a *App) GetIndexingSettings() IndexingSettingsDTO {
	workersSaved := a.cfg.Indexing.Workers
	if v, err := a.store.GetSetting(settingIndexingWorkers, ""); err == nil && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			workersSaved = n
		}
	}
	rateSaved := a.cfg.Embedder.Gemini.RateLimitPerMinute
	if v, err := a.store.GetSetting(settingIndexingRateLimit, ""); err == nil && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rateSaved = n
		}
	}

	rateRuntime := a.cfg.Embedder.Gemini.RateLimitPerMinute
	if g, ok := a.embedder.(*embedder.GeminiEmbedder); ok && g.Limiter() != nil {
		_, rateRuntime = g.Limiter().Stats()
	}

	return IndexingSettingsDTO{
		WorkersSaved:     workersSaved,
		WorkersRuntime:   a.cfg.Indexing.Workers,
		RateLimitSaved:   rateSaved,
		RateLimitRuntime: rateRuntime,
	}
}

// SetIndexingSettings validates and persists the editable indexing settings
// atomically (neither value is written if either is invalid). The rate limit
// is hot-applied to the running embedder; the worker count is persisted only
// and takes effect on next app start.
func (a *App) SetIndexingSettings(workers, rateLimit int) error {
	if workers < indexingWorkersMin || workers > indexingWorkersMax {
		return apperr.New(apperr.ErrConfigInvalid.Code, "workers must be between 1 and 32")
	}
	if rateLimit < indexingRateLimitMin || rateLimit > indexingRateLimitMax {
		return apperr.New(apperr.ErrConfigInvalid.Code, "rate limit must be between 1 and 10000")
	}
	if err := a.store.SetSetting(settingIndexingWorkers, strconv.Itoa(workers)); err != nil {
		return apperr.Wrap(apperr.ErrStoreLocked.Code, "could not save workers", err)
	}
	if err := a.store.SetSetting(settingIndexingRateLimit, strconv.Itoa(rateLimit)); err != nil {
		return apperr.Wrap(apperr.ErrStoreLocked.Code, "could not save rate limit", err)
	}
	if g, ok := a.embedder.(*embedder.GeminiEmbedder); ok && g.Limiter() != nil {
		g.Limiter().SetRatePerMinute(rateLimit)
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
