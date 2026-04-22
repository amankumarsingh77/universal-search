package app

import (
	"context"
	"time"

	"findo/internal/watcher"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// GetIndexStatus returns the current indexing pipeline status.
func (a *App) GetIndexStatus() IndexStatusDTO {
	if a.pipeline == nil {
		return IndexStatusDTO{FailedFileGroups: []FailureGroupDTO{}}
	}
	s := a.pipeline.Status()

	// Build top-5 failure groups from the registry (already sorted desc by count).
	rawGroups := a.pipeline.Registry().Groups()
	const maxGroups = 5
	if len(rawGroups) > maxGroups {
		rawGroups = rawGroups[:maxGroups]
	}
	groups := make([]FailureGroupDTO, 0, len(rawGroups))
	for _, g := range rawGroups {
		groups = append(groups, FailureGroupDTO{
			Code:        g.Code,
			Label:       g.Label,
			Count:       g.Count,
			SampleFiles: g.SampleFiles,
		})
	}

	return IndexStatusDTO{
		TotalFiles:        s.TotalFiles,
		IndexedFiles:      s.IndexedFiles,
		FailedFiles:       s.FailedFiles,
		CurrentFile:       s.CurrentFile,
		IsRunning:         s.IsRunning,
		Paused:            s.Paused,
		QuotaPaused:       s.QuotaPaused,
		QuotaResumeAt:     s.QuotaResumeAt,
		PendingRetryFiles: s.PendingRetryFiles,
		FailedFileGroups:  groups,
	}
}

// GetIndexFailures returns the full snapshot of per-file terminal failures.
// Performs no I/O — reads only from the in-memory failure registry (REQ-044).
func (a *App) GetIndexFailures() []IndexFailureDTO {
	if a.pipeline == nil {
		return []IndexFailureDTO{}
	}
	snap := a.pipeline.Registry().Snapshot()
	out := make([]IndexFailureDTO, 0, len(snap))
	for _, e := range snap {
		out = append(out, IndexFailureDTO{
			Path:         e.Path,
			Code:         e.Code,
			Message:      e.Message,
			Attempts:     e.Attempts,
			LastFailedAt: e.LastFailedAt.Unix(),
		})
	}
	return out
}

// PauseIndexing pauses the indexing pipeline.
func (a *App) PauseIndexing() {
	if a.pipeline != nil {
		a.pipeline.Pause()
	}
}

// ResumeIndexing resumes the indexing pipeline after a pause.
func (a *App) ResumeIndexing() {
	if a.pipeline != nil {
		a.pipeline.Resume()
	}
}

// ReindexNow triggers a full re-index of all watched folders in the background.
func (a *App) ReindexNow() {
	if a.store == nil || a.pipeline == nil {
		return
	}
	folders, _ := a.store.GetIndexedFolders()
	patterns, _ := a.store.GetExcludedPatterns()
	a.pipeline.ResetStatus()
	total := countIndexableFiles(folders, patterns)
	a.pipeline.SetTotalFiles(total)
	for _, folder := range folders {
		a.pipeline.SubmitFolder(folder, patterns, true)
	}
}

// ReindexFolder triggers a re-index of a single folder.
func (a *App) ReindexFolder(path string) {
	if a.store == nil || a.pipeline == nil {
		return
	}
	patterns, _ := a.store.GetExcludedPatterns()
	a.pipeline.ResetStatus()
	total := countIndexableFiles([]string{path}, patterns)
	a.pipeline.SetTotalFiles(total)
	a.pipeline.SubmitFolder(path, patterns, true)
}

// DetectMissingVectorBlobs returns true if any indexed chunks are missing vector data.
// Called by frontend on startup to determine if re-indexing is needed.
func (a *App) DetectMissingVectorBlobs() bool {
	has, err := a.store.HasMissingVectorBlobs()
	if err != nil {
		a.logger.Warn("failed to check for missing vector blobs", "error", err)
		return false
	}
	return has
}

// NeedsReindex returns true if chunks are missing vector_blob data (upgrade scenario).
// Called by frontend on startup to decide whether to show the re-index modal.
func (a *App) NeedsReindex() bool {
	return a.DetectMissingVectorBlobs()
}

// GetDebugStats returns LLM call counts, average latency, and cache hit/miss stats.
func (a *App) GetDebugStats() map[string]any {
	if a.queryStats == nil {
		return map[string]any{
			"llm_call_count": int64(0),
			"llm_avg_ms":     int64(0),
			"cache_hits":     int64(0),
			"cache_misses":   int64(0),
		}
	}
	a.queryStats.mu.Lock()
	defer a.queryStats.mu.Unlock()

	avgMs := int64(0)
	if a.queryStats.LLMCallCount > 0 {
		avgMs = a.queryStats.LLMTotalMs / a.queryStats.LLMCallCount
	}

	return map[string]any{
		"llm_call_count": a.queryStats.LLMCallCount,
		"llm_avg_ms":     avgMs,
		"cache_hits":     a.queryStats.CacheHits,
		"cache_misses":   a.queryStats.CacheMisses,
	}
}

// watchEvents processes file watcher events and queues single-file indexing.
// Exits when ctx is cancelled or the events channel is closed.
func (a *App) watchEvents(ctx context.Context, events <-chan watcher.FileEvent) {
	runtime.ResetSignalHandlers()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			switch ev.Type {
			case watcher.FileCreated, watcher.FileModified:
				if a.pipeline != nil {
					a.pipeline.SubmitFile(ev.Path)
				}
			case watcher.FileDeleted:
				if a.pipeline != nil {
					a.pipeline.DeleteFile(ev.Path)
				}
			}
		}
	}
}

// emitStatusLoop sends indexing status updates to the frontend every second.
// Exits when ctx is cancelled.
func (a *App) emitStatusLoop(ctx context.Context) {
	runtime.ResetSignalHandlers()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			status := a.GetIndexStatus()
			runtime.EventsEmit(a.ctx, "indexing-status", status)
		case <-ctx.Done():
			return
		}
	}
}
