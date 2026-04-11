package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"universal-search/internal/chunker"
	"universal-search/internal/desktop"
	"universal-search/internal/embedder"
	"universal-search/internal/indexer"
	"universal-search/internal/logger"
	"universal-search/internal/platform"
	"universal-search/internal/query"
	"universal-search/internal/search"
	"universal-search/internal/store"
	"universal-search/internal/vectorstore"
	"universal-search/internal/watcher"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var defaultIgnorePatterns = []string{
	"node_modules", ".git", "venv", ".venv", "__pycache__", ".mypy_cache",
	"dist", "build", ".next", ".nuxt", "out", "target", ".gradle", ".idea",
	".vscode", "Pods", "vendor", ".cache", ".sass-cache", "coverage",
}

// QueryStats tracks LLM call latency and cache hit/miss counts for observability.
type QueryStats struct {
	mu           sync.Mutex
	LLMCallCount int64
	LLMTotalMs   int64
	CacheHits    int64
	CacheMisses  int64
}

func (s *QueryStats) recordLLMCall(ms int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LLMCallCount++
	s.LLMTotalMs += ms
}

func (s *QueryStats) recordCacheHit() {
	s.mu.Lock()
	s.CacheHits++
	s.mu.Unlock()
}

func (s *QueryStats) recordCacheMiss() {
	s.mu.Lock()
	s.CacheMisses++
	s.mu.Unlock()
}

// App struct holds all backend components for the Wails application.
type App struct {
	ctx           context.Context
	logger        *slog.Logger
	store         *store.Store
	index         *vectorstore.Index
	indexPath     string
	embedder      *embedder.Embedder
	engine        *search.Engine
	pipeline      *indexer.Pipeline
	watcher       *watcher.Watcher
	tray          *desktop.TrayManager
	hotkeyMgr     *desktop.HotkeyManager
	trayIcon      []byte
	windowMu      sync.Mutex
	apiKeyMu      sync.Mutex // serialises concurrent SetGeminiAPIKey calls
	windowVisible bool

	saveTimerMu sync.Mutex
	saveTimer   *time.Timer

	// NL query understanding (Phase 6)
	parsedQueryCache *query.ParsedQueryCache
	llmParser        *query.LLMParser

	// Observability (Phase 8)
	queryStats *QueryStats
}

// SearchResultDTO is the JSON-serializable search result sent to the frontend.
type SearchResultDTO struct {
	FilePath      string  `json:"filePath"`
	FileName      string  `json:"fileName"`
	FileType      string  `json:"fileType"`
	Extension     string  `json:"extension"`
	SizeBytes     int64   `json:"sizeBytes"`
	ThumbnailPath string  `json:"thumbnailPath"`
	StartTime     float64 `json:"startTime"`
	EndTime       float64 `json:"endTime"`
	Score         float32 `json:"score"`
}

// ChipDTO represents a single parsed query filter chip for the frontend.
type ChipDTO struct {
	Label     string `json:"label"`
	Field     string `json:"field"`
	Op        string `json:"op"`
	Value     string `json:"value"`     // human-readable string representation
	ClauseKey string `json:"clauseKey"` // serialized "field|op|value" for denylist
}

// ParseQueryResult is the result of parsing a query into structured filters.
type ParseQueryResult struct {
	Chips         []ChipDTO `json:"chips"`
	SemanticQuery string    `json:"semanticQuery"`
	HasFilters    bool      `json:"hasFilters"`
	CacheHit      bool      `json:"cacheHit"`
	IsOffline     bool      `json:"isOffline"`
}

// SearchWithFiltersResult wraps search results with an optional relaxation banner.
type SearchWithFiltersResult struct {
	Results          []SearchResultDTO `json:"results"`
	RelaxationBanner string            `json:"relaxationBanner,omitempty"`
}

// IndexStatusDTO is the JSON-serializable indexing status sent to the frontend.
type IndexStatusDTO struct {
	TotalFiles    int    `json:"totalFiles"`
	IndexedFiles  int    `json:"indexedFiles"`
	FailedFiles   int    `json:"failedFiles"`
	CurrentFile   string `json:"currentFile"`
	IsRunning     bool   `json:"isRunning"`
	Paused        bool   `json:"paused"`
	QuotaPaused   bool   `json:"quotaPaused"`
	QuotaResumeAt string `json:"quotaResumeAt"`
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{
		queryStats: &QueryStats{},
	}
}

// startup is called when the Wails app starts. It initialises all backend
// components: store, vector index, embedder, search engine, indexing pipeline,
// and file watcher.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Initialize logger.
	dataDir, err := platform.DataDir()
	if err != nil {
		// Fallback: logger to stderr only if data dir fails.
		a.logger = slog.Default()
		a.logger.Error("failed to resolve data directory", "error", err)
		return
	}
	a.logger = logger.New(dataDir)
	log := a.logger.WithGroup("app")
	log.Info("starting Universal Search")

	dbPath, err := platform.DBPath()
	if err != nil {
		log.Error("failed to resolve db path", "error", err)
		return
	}

	a.store, err = store.NewStore(dbPath, a.logger)
	if err != nil {
		log.Error("failed to initialize store", "error", err)
		return
	}
	a.seedDefaultIgnorePatterns()

	go func() {
		if err := a.store.EvictOldQueryCache(7 * 24 * time.Hour); err != nil {
			log.Warn("query cache eviction failed", "error", err)
		}
		if err := a.store.EvictOldParsedQueryCache(); err != nil {
			log.Warn("failed to evict old parsed query cache", "error", err)
		}
	}()

	indexPath, err := platform.IndexPath()
	if err != nil {
		log.Error("failed to resolve index path", "error", err)
		return
	}

	a.indexPath = indexPath
	a.index, err = vectorstore.LoadIndex(indexPath, a.logger)
	if err != nil {
		log.Warn("no existing index found, creating new", "error", err)
		a.index = vectorstore.NewIndex(a.logger)
	}

	if dbKey, _ := a.store.GetSetting("gemini_api_key", ""); dbKey != "" {
		a.embedder, err = embedder.NewEmbedder(dbKey, 768, a.logger)
		if err != nil {
			log.Warn("embedder init from stored key failed", "keyLen", len(dbKey), "error", err)
		}
	} else {
		a.embedder, err = embedder.NewEmbedderFromEnv(768, a.logger)
		if err != nil {
			log.Warn("embedder not available", "error", err)
		}
	}

	a.engine = search.New(a.store, a.index, a.logger)

	// Wire NL query understanding components.
	a.parsedQueryCache = query.NewParsedQueryCache(a.store)
	if a.embedder != nil {
		a.llmParser = query.NewLLMParser(a.embedder.Client(), a.embedder.Limiter())
	}

	// Check ffmpeg/ffprobe availability for video processing.
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		log.Warn("ffmpeg not found in PATH — video thumbnails and previews will not work")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		log.Warn("ffprobe not found in PATH — video duration detection will not work")
	}

	thumbDir, _ := platform.ThumbnailDir()
	a.pipeline = indexer.NewPipeline(a.store, a.index, a.embedder, thumbDir, a.logger, a.saveIndex)

	// Start file watcher.
	eventCh := make(chan watcher.FileEvent, 100)
	a.watcher, err = watcher.New(eventCh, 500*time.Millisecond, a.logger)
	if err != nil {
		log.Error("failed to start file watcher", "error", err)
	}

	// Listen for add-folder requests from the frontend.
	runtime.EventsOn(ctx, "add-folder-request", func(optionalData ...interface{}) {
		dir, err := runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{
			Title: "Select folder to index",
		})
		if err == nil && dir != "" {
			a.AddFolder(dir)
			runtime.EventsEmit(ctx, "folders-changed")
		}
	})

	a.tray = desktop.NewTrayManager(a, a.trayIcon, log)
	a.tray.Start()

	a.hotkeyMgr = desktop.NewHotkeyManager(a, a.store, log)
	if err := a.hotkeyMgr.Start(); err != nil {
		log.Warn("global hotkey unavailable", "error", err)
	}

	// Crash-recovery passes — run in background, non-blocking.
	go a.pipeline.ReconcileIndex()
	go func() {
		folders, err := a.store.GetIndexedFolders()
		if err != nil {
			a.logger.WithGroup("app").Warn("could not load folders for startup rescan", "error", err)
			return
		}
		a.pipeline.StartupRescan(folders)
	}()

	// Background goroutines.
	go a.watchEvents(eventCh)
	go a.startWatchingFolders()
	go a.emitStatusLoop()

	a.windowVisible = true
	log.Info("startup complete")
}

// saveIndex schedules a debounced persist of the vector index to disk.
// Rapid successive calls (e.g. from ReconcileIndex submitting many jobs)
// collapse into a single write that fires 2 seconds after the last call.
func (a *App) saveIndex() {
	if a.index == nil || a.indexPath == "" {
		return
	}
	a.saveTimerMu.Lock()
	if a.saveTimer != nil {
		a.saveTimer.Reset(2 * time.Second)
	} else {
		a.saveTimer = time.AfterFunc(2*time.Second, func() {
			if err := a.index.Save(a.indexPath); err != nil {
				a.logger.WithGroup("app").Error("failed to save index", "error", err)
			}
		})
	}
	a.saveTimerMu.Unlock()
}

// shutdown is called when the Wails app is closing. It stops the pipeline,
// closes the watcher, saves the index, and closes the store.
func (a *App) shutdown(ctx context.Context) {
	log := a.logger.WithGroup("app")
	log.Info("shutting down")
	if a.hotkeyMgr != nil {
		a.hotkeyMgr.Stop()
	}
	if a.tray != nil {
		a.tray.Stop()
	}
	if a.pipeline != nil {
		a.pipeline.Stop()
	}
	if a.watcher != nil {
		a.watcher.Close()
	}
	// Cancel any pending debounced save and write synchronously.
	a.saveTimerMu.Lock()
	if a.saveTimer != nil {
		a.saveTimer.Stop()
		a.saveTimer = nil
	}
	a.saveTimerMu.Unlock()
	if a.index != nil && a.indexPath != "" {
		if err := a.index.Save(a.indexPath); err != nil {
			log.Error("failed to save index on shutdown", "error", err)
		}
	}
	if a.store != nil {
		a.store.Close()
	}
	log.Info("shutdown complete")
}

// Search embeds the query via Gemini and returns the top search results.
func (a *App) Search(query string) ([]SearchResultDTO, error) {
	if a.embedder == nil {
		return nil, fmt.Errorf("embedder not initialized — set GEMINI_API_KEY")
	}

	a.logger.Info("search query", "query", query)

	var vec []float32
	if a.store != nil {
		cached, err := a.store.GetQueryCache(query)
		if err == nil && cached != nil {
			a.logger.Info("query cache hit", "query", query)
			vec = cached
		}
	}

	if vec == nil {
		var err error
		vec, err = a.embedder.EmbedQuery(a.ctx, query)
		if err != nil {
			return nil, err
		}
		go func() { a.store.SetQueryCache(query, vec) }()
	}

	results, err := a.engine.SearchByVector(vec, 20)
	if err != nil {
		return nil, err
	}

	dtos := make([]SearchResultDTO, len(results))
	for i, r := range results {
		dtos[i] = SearchResultDTO{
			FilePath:      r.File.Path,
			FileName:      filepath.Base(r.File.Path),
			FileType:      r.File.FileType,
			Extension:     r.File.Extension,
			SizeBytes:     r.File.SizeBytes,
			ThumbnailPath: r.File.ThumbnailPath,
			StartTime:     r.StartTime,
			EndTime:       r.EndTime,
			Score:         1 - r.Distance/2,
		}
	}

	a.logger.Info("search results", "query", query, "results", len(dtos))
	for i, d := range dtos {
		a.logger.Debug("search result",
			"rank", i+1,
			"file", d.FileName,
			"type", d.FileType,
			"path", d.FilePath,
		)
	}

	return dtos, nil
}

// GetIndexStatus returns the current indexing pipeline status.
func (a *App) GetIndexStatus() IndexStatusDTO {
	if a.pipeline == nil {
		return IndexStatusDTO{}
	}
	s := a.pipeline.Status()
	return IndexStatusDTO{
		TotalFiles:    s.TotalFiles,
		IndexedFiles:  s.IndexedFiles,
		FailedFiles:   s.FailedFiles,
		CurrentFile:   s.CurrentFile,
		IsRunning:     s.IsRunning,
		Paused:        s.Paused,
		QuotaPaused:   s.QuotaPaused,
		QuotaResumeAt: s.QuotaResumeAt,
	}
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

// countIndexableFiles walks the given folders and counts files that the indexer
// would process, applying the same exclude-pattern logic as processFolder.
func countIndexableFiles(folders []string, excludePatterns []string) int {
	total := 0
	for _, folder := range folders {
		filepath.WalkDir(folder, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				for _, pat := range excludePatterns {
					if matched, _ := filepath.Match(pat, d.Name()); matched {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if chunker.Classify(path) != chunker.TypeUnknown {
				total++
			}
			return nil
		})
	}
	return total
}

// AddFolder adds a folder to the indexed folders list, starts watching it,
// and triggers indexing.
func (a *App) AddFolder(path string) error {
	if a.store == nil {
		return fmt.Errorf("store not initialized")
	}
	a.logger.Info("adding folder", "path", path)
	if err := a.store.AddIndexedFolder(path); err != nil {
		return err
	}
	if a.watcher != nil {
		a.watcher.Add(path)
	}
	// Queue indexing for the newly added folder.
	if a.pipeline != nil {
		patterns, _ := a.store.GetExcludedPatterns()
		a.pipeline.SubmitFolder(path, patterns, false)
	}
	return nil
}

// RemoveFolder removes a folder from the indexed folders list and stops watching it.
// If deleteData is true, it also removes all indexed file data and vectors.
func (a *App) RemoveFolder(path string, deleteData bool) error {
	if a.store == nil {
		return fmt.Errorf("store not initialized")
	}
	a.logger.Info("removing folder", "path", path, "deleteData", deleteData)

	vecIDs, err := a.store.RemoveIndexedFolder(path, deleteData)
	if err != nil {
		return err
	}

	// Stop watching the folder.
	if a.watcher != nil {
		a.watcher.Remove(path)
	}

	// Remove vectors from the HNSW index.
	if deleteData && a.index != nil {
		for _, vid := range vecIDs {
			a.index.Delete(vid)
		}
		a.saveIndex()
		a.logger.Info("removed vectors from index", "count", len(vecIDs))
	}

	return nil
}

// GetFolders returns all indexed folder paths.
func (a *App) GetFolders() ([]string, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}
	return a.store.GetIndexedFolders()
}

// OpenFile opens a file using the system default application.
func (a *App) OpenFile(path string) {
	openPath(path)
}

// OpenFolder opens the folder containing the given file path.
func (a *App) OpenFolder(path string) {
	openPath(filepath.Dir(path))
}

// openPath opens a file or directory using the platform-specific default handler.
func openPath(path string) {
	var cmd string
	var args []string
	switch goruntime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{path}
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", "", path}
	default: // linux and others
		cmd = "xdg-open"
		args = []string{path}
	}
	exec.Command(cmd, args...).Start()
}

// GetPreviewClipPath extracts a short preview clip from a video at the given
// timestamp using ffmpeg. Returns the path to the generated clip.
func (a *App) GetPreviewClipPath(videoPath string, timestamp float64) (string, error) {
	thumbDir, err := platform.ThumbnailDir()
	if err != nil {
		return "", err
	}
	clipName := fmt.Sprintf("preview_%x_%.0f.mp4", []byte(videoPath), timestamp)
	clipPath := filepath.Join(thumbDir, clipName)

	if _, err := os.Stat(clipPath); err == nil {
		return clipPath, nil
	}

	if err := indexer.ExtractPreviewClip(videoPath, clipPath, timestamp, 15); err != nil {
		return "", fmt.Errorf("ffmpeg preview clip failed: %w", err)
	}
	return clipPath, nil
}

// GetFilePreview returns up to 8192 bytes of a text/code file's content.
// Returns an error if the file is binary, unreadable, or not valid UTF-8.
func (a *App) GetFilePreview(path string) (string, error) {
	const maxBytes = 8192
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer f.Close()
	buf := make([]byte, maxBytes)
	n, err := f.Read(buf)
	if n == 0 {
		return "", fmt.Errorf("empty file")
	}
	buf = buf[:n]
	for _, b := range buf {
		if b == 0 {
			return "", fmt.Errorf("binary file")
		}
	}
	if !utf8.Valid(buf) {
		return "", fmt.Errorf("invalid UTF-8")
	}
	return string(buf), nil
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

func (a *App) ShowWindow() {
	a.windowMu.Lock()
	defer a.windowMu.Unlock()
	a.logger.Info("showing window")
	runtime.WindowShow(a.ctx)
	runtime.WindowCenter(a.ctx)
	a.windowVisible = true
	runtime.EventsEmit(a.ctx, "window-shown")
}

func (a *App) HideWindow() {
	a.windowMu.Lock()
	defer a.windowMu.Unlock()
	a.logger.Info("hiding window")
	runtime.WindowHide(a.ctx)
	a.windowVisible = false
}

func (a *App) ToggleWindow() {
	a.windowMu.Lock()
	visible := a.windowVisible
	a.windowMu.Unlock()
	a.logger.Info("toggle window", "currentlyVisible", visible)
	if visible {
		a.HideWindow()
	} else {
		a.ShowWindow()
	}
}

func (a *App) EmitEvent(name string) {
	runtime.EventsEmit(a.ctx, name)
}

func (a *App) Quit() {
	runtime.Quit(a.ctx)
}

// PreEmbedQuery speculatively embeds the query and caches the result.
// Called by the frontend while the user types. Errors are swallowed — best-effort.
func (a *App) PreEmbedQuery(query string) {
	if a.embedder == nil || a.store == nil {
		return
	}
	cached, err := a.store.GetQueryCache(query)
	if err != nil || cached != nil {
		return
	}
	vec, err := a.embedder.EmbedQuery(a.ctx, query)
	if err != nil {
		a.logger.Debug("pre-embed failed", "query", query, "error", err)
		return
	}
	if err := a.store.SetQueryCache(query, vec); err != nil {
		a.logger.Debug("pre-embed cache write failed", "query", query, "error", err)
	}
}

func (a *App) GetSetting(key string) string {
	val, _ := a.store.GetSetting(key, "")
	return val
}

func (a *App) SetSetting(key, value string) error {
	if key == "global_hotkey" && a.hotkeyMgr != nil {
		return a.hotkeyMgr.ChangeHotkey(value)
	}
	return a.store.SetSetting(key, value)
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
		return fmt.Errorf("API key must not be empty")
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
		return fmt.Errorf("failed to create embedder: %w", err)
	}

	// Validate with a real test call, 10s timeout.
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	if _, err := tmpEmb.EmbedQuery(ctx, "test"); err != nil {
		restore()
		return fmt.Errorf("API key validation failed: %w", err)
	}

	// Validation passed — commit atomically.
	if err := a.store.SetSetting("gemini_api_key", key); err != nil {
		restore()
		return fmt.Errorf("failed to persist API key: %w", err)
	}
	a.embedder = tmpEmb
	a.pipeline.SetEmbedder(tmpEmb)

	restore()

	a.logger.Info("Gemini API key updated", "keyLen", len(key))
	return nil
}

// GetHasGeminiKey returns true if a Gemini API key is currently configured.
func (a *App) GetHasGeminiKey() bool {
	return a.embedder != nil
}

func (a *App) seedDefaultIgnorePatterns() {
	has, err := a.store.HasAnyExcludedPattern()
	if err != nil {
		a.logger.WithGroup("app").Warn("could not check excluded patterns", "error", err)
		return
	}
	if has {
		return
	}
	for _, p := range defaultIgnorePatterns {
		if err := a.store.AddExcludedPattern(p); err != nil {
			a.logger.WithGroup("app").Warn("failed to seed ignore pattern", "pattern", p, "error", err)
		}
	}
	a.logger.WithGroup("app").Info("seeded default ignore patterns", "count", len(defaultIgnorePatterns))
}

func (a *App) GetIgnoredFolders() ([]string, error) {
	if a.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}
	return a.store.GetExcludedPatterns()
}

func (a *App) AddIgnoredFolder(pattern string) error {
	if a.store == nil {
		return fmt.Errorf("store not initialized")
	}
	if strings.TrimSpace(pattern) == "" {
		return fmt.Errorf("pattern must not be empty")
	}
	return a.store.AddExcludedPattern(strings.TrimSpace(pattern))
}

func (a *App) RemoveIgnoredFolder(pattern string) error {
	if a.store == nil {
		return fmt.Errorf("store not initialized")
	}
	return a.store.RemoveExcludedPattern(pattern)
}

// watchEvents processes file watcher events and queues single-file indexing.
func (a *App) watchEvents(events <-chan watcher.FileEvent) {
	runtime.ResetSignalHandlers()
	for ev := range events {
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

// startWatchingFolders adds all previously indexed folders to the file watcher.
func (a *App) startWatchingFolders() {
	runtime.ResetSignalHandlers()
	if a.watcher == nil || a.store == nil {
		return
	}
	folders, _ := a.store.GetIndexedFolders()
	for _, f := range folders {
		a.watcher.Add(f)
	}
}

// emitStatusLoop sends indexing status updates to the frontend every second.
func (a *App) emitStatusLoop() {
	runtime.ResetSignalHandlers()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			status := a.GetIndexStatus()
			runtime.EventsEmit(a.ctx, "indexing-status", status)
		case <-a.ctx.Done():
			return
		}
	}
}

// isNLQueryEnabled returns true unless nl_query_enabled is explicitly set to "false".
func (a *App) isNLQueryEnabled() bool {
	if a.store == nil {
		return true
	}
	val, _ := a.store.GetSetting("nl_query_enabled", "true")
	return val != "false"
}

// isOfflineMode returns true if no Gemini API key is available (embedder not initialised).
// It checks the environment variable first, then the settings table.
func (a *App) isOfflineMode() bool {
	if os.Getenv("GEMINI_API_KEY") != "" {
		return false
	}
	if a.store == nil {
		return true
	}
	apiKey, err := a.store.GetSetting("gemini_api_key", "")
	if err != nil || apiKey == "" {
		return true
	}
	return false
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

// NeedsReindex returns true if chunks are missing vector_blob data (upgrade scenario).
// Called by frontend on startup to decide whether to show the re-index modal.
func (a *App) NeedsReindex() bool {
	return a.DetectMissingVectorBlobs()
}

// GetNLQueryEnabled returns the current nl_query_enabled setting.
func (a *App) GetNLQueryEnabled() bool {
	return a.isNLQueryEnabled()
}

// SetNLQueryEnabled sets the nl_query_enabled setting.
func (a *App) SetNLQueryEnabled(enabled bool) error {
	if a.store == nil {
		return fmt.Errorf("store not initialized")
	}
	val := "false"
	if enabled {
		val = "true"
	}
	return a.store.SetSetting("nl_query_enabled", val)
}

// ParseQuery parses a raw query string into structured filter chips.
// It runs a grammar parse always, checks the cache, and conditionally invokes
// the LLM parser if the residual query warrants it.
// When offline (no API key), LLM parse is skipped and IsOffline is set to true.
func (a *App) ParseQuery(raw string) (ParseQueryResult, error) {
	if !a.isNLQueryEnabled() {
		return ParseQueryResult{}, nil
	}

	offline := a.isOfflineMode()

	// Grammar parse — always, no network.
	grammarSpec := query.Parse(raw)

	// Check cache before LLM.
	var mergedSpec query.FilterSpec
	cacheHit := false
	if a.parsedQueryCache != nil {
		if cached, err := a.parsedQueryCache.Get(raw); err == nil && cached != nil {
			mergedSpec = *cached
			cacheHit = true
		}
	}

	if !cacheHit {
		if a.queryStats != nil {
			a.queryStats.recordCacheMiss()
		}
		llmSpec := grammarSpec
		// Skip LLM when offline.
		if !offline && query.ShouldInvokeLLM(grammarSpec.SemanticQuery) && a.llmParser != nil && a.ctx != nil {
			ctx, cancel := context.WithTimeout(a.ctx, 500*time.Millisecond)
			defer cancel()
			llmStart := time.Now()
			parsed, err := a.llmParser.Parse(ctx, raw, grammarSpec)
			if a.queryStats != nil {
				a.queryStats.recordLLMCall(time.Since(llmStart).Milliseconds())
			}
			if err == nil {
				llmSpec = parsed
			}
		}
		mergedSpec = query.Merge(grammarSpec, llmSpec, nil)
		if a.parsedQueryCache != nil {
			_ = a.parsedQueryCache.Set(raw, mergedSpec)
		}
	} else {
		if a.queryStats != nil {
			a.queryStats.recordCacheHit()
		}
	}

	chips := buildChipDTOs(mergedSpec)

	return ParseQueryResult{
		Chips:         chips,
		SemanticQuery: mergedSpec.SemanticQuery,
		HasFilters:    len(mergedSpec.Must)+len(mergedSpec.MustNot)+len(mergedSpec.Should) > 0,
		CacheHit:      cacheHit,
		IsOffline:     offline,
	}, nil
}

// SearchWithFilters runs a search using the parsed FilterSpec, applying a
// denylist to remove chips the user has dismissed.
// When offline (no API key), it falls back to filename-contains search.
// If the vector search returns a network error, it falls back to filename search for that query.
func (a *App) SearchWithFilters(raw string, semanticQuery string, denyList []string) (SearchWithFiltersResult, error) {
	start := time.Now()

	if !a.isNLQueryEnabled() {
		results, err := a.Search(raw)
		return SearchWithFiltersResult{Results: results}, err
	}

	// Offline mode: skip embedding entirely, use filename search.
	isOffline := a.isOfflineMode()
	if isOffline {
		return a.searchFilenameOnly(raw)
	}

	// Get current FilterSpec (from cache or grammar).
	grammarSpec := query.Parse(raw)
	grammarFilterCount := len(grammarSpec.Must) + len(grammarSpec.MustNot) + len(grammarSpec.Should)

	var mergedSpec query.FilterSpec
	cacheHit := false
	if a.parsedQueryCache != nil {
		if cached, err := a.parsedQueryCache.Get(raw); err == nil && cached != nil {
			mergedSpec = *cached
			cacheHit = true
		} else {
			mergedSpec = grammarSpec
		}
	} else {
		mergedSpec = grammarSpec
	}

	llmFilterCount := len(mergedSpec.Must) + len(mergedSpec.MustNot) + len(mergedSpec.Should)

	// Apply denylist.
	denyClauseKeys := parseDenyList(denyList)
	mergedSpec = query.Merge(mergedSpec, query.FilterSpec{}, denyClauseKeys)

	// Override semantic query if provided.
	if semanticQuery != "" {
		mergedSpec.SemanticQuery = semanticQuery
	}

	// Embed the semantic query.
	queryText := mergedSpec.SemanticQuery
	if queryText == "" {
		queryText = raw
	}
	if queryText == "" {
		return SearchWithFiltersResult{}, nil
	}

	queryVec, err := a.getQueryVector(queryText)
	if err != nil {
		return SearchWithFiltersResult{}, err
	}

	// Run SearchWithSpec; on network errors fall back to filename search.
	searchResult, err := a.engine.SearchWithSpec(queryVec, mergedSpec, raw, 20)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "network") ||
			strings.Contains(strings.ToLower(err.Error()), "embed") {
			a.logger.Warn("vector search failed, falling back to filename search", "error", err)
			return a.searchFilenameOnly(queryText)
		}
		return SearchWithFiltersResult{}, err
	}

	dtos := make([]SearchResultDTO, 0, len(searchResult.Results))
	for _, r := range searchResult.Results {
		dtos = append(dtos, toSearchResultDTO(r))
	}

	a.logger.Debug("query pipeline complete",
		"raw", raw,
		"grammar_filter_count", grammarFilterCount,
		"llm_filter_count", llmFilterCount,
		"chosen_strategy", searchResult.Strategy,
		"planner_count", searchResult.PlannerCount,
		"final_result_count", len(dtos),
		"latency_ms", time.Since(start).Milliseconds(),
		"cache_hit", cacheHit,
		"offline", isOffline,
	)

	return SearchWithFiltersResult{
		Results:          dtos,
		RelaxationBanner: searchResult.RelaxationBanner,
	}, nil
}

// searchFilenameOnly runs a filename-contains search and returns results as DTOs.
func (a *App) searchFilenameOnly(queryText string) (SearchWithFiltersResult, error) {
	if a.store == nil || queryText == "" {
		return SearchWithFiltersResult{}, nil
	}
	files, err := a.store.SearchFilenameContains(queryText)
	if err != nil {
		return SearchWithFiltersResult{}, err
	}
	dtos := make([]SearchResultDTO, 0, len(files))
	for _, f := range files {
		dtos = append(dtos, SearchResultDTO{
			FilePath:      f.Path,
			FileName:      filepath.Base(f.Path),
			FileType:      f.FileType,
			Extension:     f.Extension,
			SizeBytes:     f.SizeBytes,
			ThumbnailPath: f.ThumbnailPath,
			Score:         0,
		})
	}
	return SearchWithFiltersResult{Results: dtos}, nil
}

// getQueryVector embeds the query and caches the result.
func (a *App) getQueryVector(queryText string) ([]float32, error) {
	if a.embedder == nil {
		return nil, fmt.Errorf("embedder not initialized — set GEMINI_API_KEY")
	}

	if a.store != nil {
		cached, err := a.store.GetQueryCache(queryText)
		if err == nil && cached != nil {
			return cached, nil
		}
	}

	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	vec, err := a.embedder.EmbedQuery(ctx, queryText)
	if err != nil {
		return nil, err
	}
	if a.store != nil {
		go func() { a.store.SetQueryCache(queryText, vec) }()
	}
	return vec, nil
}

// toSearchResultDTO converts a store.SearchResult to a SearchResultDTO.
func toSearchResultDTO(r store.SearchResult) SearchResultDTO {
	return SearchResultDTO{
		FilePath:      r.File.Path,
		FileName:      filepath.Base(r.File.Path),
		FileType:      r.File.FileType,
		Extension:     r.File.Extension,
		SizeBytes:     r.File.SizeBytes,
		ThumbnailPath: r.File.ThumbnailPath,
		StartTime:     r.StartTime,
		EndTime:       r.EndTime,
		Score:         1 - r.Distance/2,
	}
}

// parseDenyList converts a slice of "field|op|value" strings into ClauseKey values.
func parseDenyList(denyList []string) []query.ClauseKey {
	if len(denyList) == 0 {
		return nil
	}
	keys := make([]query.ClauseKey, 0, len(denyList))
	for _, s := range denyList {
		parts := strings.SplitN(s, "|", 3)
		if len(parts) != 3 {
			continue
		}
		keys = append(keys, query.ClauseKey{
			Field: query.FieldEnum(parts[0]),
			Op:    query.Op(parts[1]),
			Value: parts[2],
		})
	}
	return keys
}

// buildChipDTOs converts a FilterSpec into a slice of ChipDTO values for the frontend.
func buildChipDTOs(spec query.FilterSpec) []ChipDTO {
	var chips []ChipDTO
	for _, c := range spec.Must {
		if chip, ok := clauseToChip(c, false); ok {
			chips = append(chips, chip)
		}
	}
	for _, c := range spec.MustNot {
		if chip, ok := clauseToChip(c, true); ok {
			chips = append(chips, chip)
		}
	}
	for _, c := range spec.Should {
		if chip, ok := clauseToChip(c, false); ok {
			chips = append(chips, chip)
		}
	}
	return chips
}

// clauseToChip converts a single Clause to a ChipDTO with a human-readable label.
func clauseToChip(c query.Clause, negate bool) (ChipDTO, bool) {
	var label, valueStr string

	switch c.Field {
	case query.FieldFileType:
		s, ok := c.Value.(string)
		if !ok {
			return ChipDTO{}, false
		}
		valueStr = s
		label = fileTypeLabel(s)

	case query.FieldExtension:
		switch v := c.Value.(type) {
		case string:
			valueStr = v
			label = v
		case []string:
			valueStr = strings.Join(v, ",")
			label = strings.Join(v, ", ")
		default:
			return ChipDTO{}, false
		}

	case query.FieldSizeBytes:
		var bytes int64
		switch v := c.Value.(type) {
		case int64:
			bytes = v
		case int:
			bytes = int64(v)
		default:
			return ChipDTO{}, false
		}
		valueStr = fmt.Sprintf("%d", bytes)
		opStr := opSymbol(c.Op)
		label = fmt.Sprintf("%s %s", opStr, formatBytes(bytes))

	case query.FieldModifiedAt:
		t, ok := c.Value.(time.Time)
		if !ok {
			return ChipDTO{}, false
		}
		valueStr = t.Format("2006-01-02")
		switch c.Op {
		case query.OpGte, query.OpGt:
			label = "Since " + t.Format("Jan 2")
		case query.OpLte, query.OpLt:
			label = "Before " + t.Format("Jan 2")
		default:
			label = t.Format("Jan 2")
		}

	case query.FieldPath:
		s, ok := c.Value.(string)
		if !ok {
			return ChipDTO{}, false
		}
		valueStr = s
		label = "Path: " + s

	default:
		s, ok := c.Value.(string)
		if !ok {
			return ChipDTO{}, false
		}
		valueStr = s
		label = s
	}

	if negate {
		label = "Not " + label
	}

	clauseKey := fmt.Sprintf("%s|%s|%s", c.Field, c.Op, valueStr)

	return ChipDTO{
		Label:     label,
		Field:     string(c.Field),
		Op:        string(c.Op),
		Value:     valueStr,
		ClauseKey: clauseKey,
	}, true
}

// fileTypeLabel returns a human-readable label for a file_type value.
func fileTypeLabel(ft string) string {
	switch ft {
	case "image":
		return "Images"
	case "video":
		return "Videos"
	case "audio":
		return "Audio"
	case "document":
		return "Documents"
	case "text":
		return "Text"
	default:
		return strings.Title(ft) //nolint:staticcheck
	}
}

// opSymbol returns a human-readable operator symbol.
func opSymbol(op query.Op) string {
	switch op {
	case query.OpGt:
		return ">"
	case query.OpGte:
		return ">="
	case query.OpLt:
		return "<"
	case query.OpLte:
		return "<="
	case query.OpEq:
		return "="
	case query.OpNeq:
		return "!="
	default:
		return string(op)
	}
}

// formatBytes formats a byte count as a human-readable size string.
func formatBytes(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%d GB", b/GB)
	case b >= MB:
		return fmt.Sprintf("%d MB", b/MB)
	case b >= KB:
		return fmt.Sprintf("%d KB", b/KB)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
