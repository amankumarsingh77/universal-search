package app

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"findo/internal/apperr"
	"findo/internal/config"
	"findo/internal/desktop"
	"findo/internal/embedder"
	"findo/internal/indexer"
	"findo/internal/logger"
	"findo/internal/platform"
	"findo/internal/query"
	"findo/internal/search"
	"findo/internal/store"
	"findo/internal/vectorstore"
	"findo/internal/watcher"

	"errors"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sync/errgroup"
)

// envAPIKey returns the Gemini API key from standard env vars. Kept as a
// small helper so the startup wiring reads clean.
func envAPIKey() string {
	if k := os.Getenv("GEMINI_API_KEY"); k != "" {
		return k
	}
	return os.Getenv("GOOGLE_API_KEY")
}

var defaultIgnorePatterns = []string{
	"node_modules", ".git", "venv", ".venv", "__pycache__", ".mypy_cache",
	"dist", "build", ".next", ".nuxt", "out", "target", ".gradle", ".idea",
	".vscode", "Pods", "vendor", ".cache", ".sass-cache", "coverage",
}

// App struct holds all backend components for the Wails application.
type App struct {
	ctx           context.Context
	cfg           *config.Config
	logger        *slog.Logger
	store         *store.Store
	index         *vectorstore.Index
	indexPath     string
	embedder      embedder.Embedder
	engine        *search.Engine
	pipeline      *indexer.Pipeline
	watcher       *watcher.Watcher
	tray          *desktop.TrayManager
	hotkeyMgr     *desktop.HotkeyManager
	trayIcon      []byte
	trayIconTpl   []byte
	windowMu      sync.Mutex
	apiKeyMu      sync.RWMutex // guards a.embedder and a.llmParser: write on SetGeminiAPIKey, read on ParseQuery/SearchWithFilters
	windowVisible bool

	saveTimerMu sync.Mutex
	saveTimer   *time.Timer

	// NL query understanding (Phase 6)
	parsedQueryCache parsedQueryCacheIface
	llmParser        llmQueryParser

	// Observability (Phase 8)
	queryStats *QueryStats

	// Graceful shutdown (Phase 8)
	baseCtx        context.Context
	shutdownCancel context.CancelFunc
	group          *errgroup.Group
	groupCtx       context.Context

	// Test hook — when non-nil, emitBackendError routes payloads here instead
	// of runtime.EventsEmit. Kept unexported so it cannot be bound by Wails.
	backendErrorSink func(payload map[string]any)
}

// deriveErrorCodeAndMessage inspects err for an apperr.Error and returns its
// code + message. Falls back to ERR_INTERNAL for bare errors.
func deriveErrorCodeAndMessage(err error) (code, message string) {
	var aerr *apperr.Error
	if errors.As(err, &aerr) {
		return aerr.Code, aerr.Message
	}
	return apperr.ErrInternal.Code, err.Error()
}

// NewApp creates a new App application struct wired to cfg. If cfg is nil the
// defaults from config.DefaultConfig are used — handy for tests that do not
// exercise the config loader.
func NewApp(cfg *config.Config) *App {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	return &App{
		cfg:        cfg,
		queryStats: &QueryStats{},
	}
}

// SetTrayIcon sets the raw bytes of the tray icon before startup. Package-level
// to avoid adding a Wails binding. The template icon is used on macOS (black +
// alpha, auto-tinted by the OS); the regular icon is the colored fallback for
// Windows and Linux.
func SetTrayIcon(a *App, regular, template []byte) {
	a.trayIcon = regular
	a.trayIconTpl = template
}

// FolderAllowed reports whether the given absolute path falls under one of the
// app's indexed folders. Used by the local-file asset handler. Package-level
// to avoid adding a Wails binding.
func FolderAllowed(a *App, filePath string) bool {
	if a.store == nil {
		return false
	}
	folders, err := a.store.GetIndexedFolders()
	if err != nil {
		return false
	}
	for _, folder := range folders {
		if strings.HasPrefix(filePath, folder) {
			return true
		}
	}
	return false
}

// ShowAboutDialog shows the macOS-native About dialog. Package-level to avoid
// adding a Wails binding.
func ShowAboutDialog(a *App) {
	runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:    runtime.InfoDialog,
		Title:   "Findo",
		Message: "Findo — fast local file search powered by vector embeddings.",
	})
}

// OnStartup returns the Wails startup callback for the given app. Exposed as
// a package-level function (not a method) so Wails reflection doesn't bind it
// into the frontend surface.
func OnStartup(a *App) func(context.Context) { return a.startup }

// OnShutdown returns the Wails shutdown callback for the given app.
func OnShutdown(a *App) func(context.Context) { return a.shutdown }

// startup is called when the Wails app starts. It initialises all backend
// components: store, vector index, embedder, search engine, indexing pipeline,
// and file watcher.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startErrgroup()

	// Hide from Dock / Cmd-Tab on macOS. Wails hardcodes Regular activation
	// policy in its AppDelegate, so this must be set after startup.
	desktop.SetAccessoryActivationPolicy()

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
	log.Info("starting Findo")

	dbPath, err := platform.DBPath()
	if err != nil {
		log.Error("failed to resolve db path", "error", err)
		return
	}

	// The backfill closure fills embedding_model/embedding_dims on pre-migration-004
	// chunk rows. We don't know the live embedder yet, so we fall back to the
	// configured default model/dims — accurate for single-model installs, which
	// is the overwhelming common case.
	backfillModel := a.cfg.Embedder.Model
	backfillDims := a.cfg.Embedder.Dimensions
	backfill := func(db *sql.DB) error {
		_, bfErr := db.Exec(
			`UPDATE chunks SET embedding_model = ?, embedding_dims = ? WHERE embedding_model = ''`,
			backfillModel, backfillDims,
		)
		return bfErr
	}
	a.store, err = store.NewStoreWithBackfill(dbPath, a.logger, backfill)
	if err != nil {
		log.Error("failed to initialize store", "error", err)
		a.emitBackendError(apperr.ErrMigrationFailed.Code, "database could not be opened", map[string]any{"path": dbPath})
		return
	}
	a.seedDefaultIgnorePatterns()
	a.applyPersistedIndexingOverrides()

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
		a.index = vectorstore.NewIndex(vectorstore.HNSWConfig{
			M:              a.cfg.HNSW.M,
			Ml:             a.cfg.HNSW.Ml,
			EfSearch:       a.cfg.HNSW.EfSearch,
			EfConstruction: a.cfg.HNSW.EfConstruction,
			Distance:       a.cfg.HNSW.Distance,
		}, a.logger)
	}

	embCfg := embedder.GeminiConfig{
		Model:                 a.cfg.Embedder.Model,
		Dimensions:            a.cfg.Embedder.Dimensions,
		RateLimitPerMinute:    a.cfg.Embedder.Gemini.RateLimitPerMinute,
		BatchSize:             a.cfg.Embedder.Gemini.BatchSize,
		RetryMaxAttempts:      a.cfg.Embedder.Gemini.RetryMaxAttempts,
		RetryInitialBackoffMs: a.cfg.Embedder.Gemini.RetryInitialBackoffMs,
		RetryMaxBackoffMs:     a.cfg.Embedder.Gemini.RetryMaxBackoffMs,
	}
	if dbKey, _ := a.store.GetSetting("gemini_api_key", ""); dbKey != "" {
		embCfg.APIKey = dbKey
		a.embedder, err = embedder.NewGeminiEmbedderFromConfig(embCfg, a.logger)
		if err != nil {
			log.Warn("embedder init from stored key failed", "keyLen", len(dbKey), "error", err)
		}
	} else if envKey := envAPIKey(); envKey != "" {
		embCfg.APIKey = envKey
		a.embedder, err = embedder.NewGeminiEmbedderFromConfig(embCfg, a.logger)
		if err != nil {
			log.Warn("embedder not available", "error", err)
		}
	} else {
		log.Warn("no gemini api key configured — embedder not initialised")
	}

	a.engine = a.buildSearchEngine()

	// Wire NL query understanding components.
	a.parsedQueryCache = query.NewParsedQueryCache(a.store)
	if g, ok := a.embedder.(*embedder.GeminiEmbedder); ok {
		a.llmParser = query.NewLLMParserWithConfig(g.Client(), g.Limiter(), query.LLMConfig{
			Model:      a.cfg.Query.LLMModel,
			TimeoutMs:  a.cfg.Query.LLMTimeoutMs,
			MaxRetries: a.cfg.Query.LLMMaxRetries,
		})
	}

	// Check ffmpeg/ffprobe availability for video processing.
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		log.Warn("ffmpeg not found in PATH — video thumbnails and previews will not work")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		log.Warn("ffprobe not found in PATH — video duration detection will not work")
	}

	thumbDir, _ := platform.ThumbnailDir()
	a.pipeline = indexer.NewPipeline(a.store, a.index, a.embedder, thumbDir, a.logger, a.saveIndex, indexer.PipelineConfig{
		Workers:      a.cfg.Indexing.Workers,
		JobQueueSize: a.cfg.Indexing.JobQueueSize,
		SaveEveryN:   a.cfg.Indexing.SaveEveryN,
	})

	// Start file watcher.
	eventCh := make(chan watcher.FileEvent, 100)
	a.watcher, err = watcher.New(eventCh, 500*time.Millisecond, a.logger)
	if err != nil {
		log.Error("failed to start file watcher", "error", err)
	}

	a.tray = desktop.NewTrayManager(a, a.trayIcon, a.trayIconTpl, log)
	a.tray.Start()

	a.hotkeyMgr = desktop.NewHotkeyManager(a, a.store, log)
	if err := a.hotkeyMgr.Start(); err != nil {
		log.Warn("global hotkey unavailable", "error", err)
	}

	// Crash-recovery passes — run in background, supervised by errgroup.
	a.group.Go(func() error {
		a.pipeline.ReconcileIndex()
		return nil
	})
	a.group.Go(func() error {
		folders, err := a.store.GetIndexedFolders()
		if err != nil {
			a.reportBackgroundError("startup-rescan", apperr.Wrap(apperr.ErrStoreLocked.Code, "could not load folders for startup rescan", err))
			return nil
		}
		a.pipeline.StartupRescan(folders)
		return nil
	})

	// Background goroutines.
	a.group.Go(func() error { a.watchEvents(a.groupCtx, eventCh); return nil })
	a.group.Go(func() error { a.startWatchingFolders(); return nil })
	a.group.Go(func() error { a.emitStatusLoop(a.groupCtx); return nil })

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

// shutdown is called when the Wails app is closing. It cancels the errgroup
// context, waits for supervised goroutines to finish (bounded by
// cfg.App.ShutdownTimeoutMs), then tears down pipeline/tray/hotkey/index/store.
// Exits with code 2 if the shutdown deadline is exceeded.
func (a *App) shutdown(ctx context.Context) {
	log := a.logger.WithGroup("app")
	log.Info("shutting down")

	timedOut := a.shutdownWithTimeout()

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

	if timedOut {
		exitProcess(2)
	}
}
