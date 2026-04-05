package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"universal-search/internal/embedder"
	"universal-search/internal/indexer"
	"universal-search/internal/logger"
	"universal-search/internal/platform"
	"universal-search/internal/search"
	"universal-search/internal/store"
	"universal-search/internal/vectorstore"
	"universal-search/internal/watcher"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct holds all backend components for the Wails application.
type App struct {
	ctx       context.Context
	logger    *slog.Logger
	store     *store.Store
	index     *vectorstore.Index
	indexPath string
	embedder  *embedder.Embedder
	engine    *search.Engine
	pipeline  *indexer.Pipeline
	watcher   *watcher.Watcher
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
}

// IndexStatusDTO is the JSON-serializable indexing status sent to the frontend.
type IndexStatusDTO struct {
	TotalFiles    int    `json:"totalFiles"`
	IndexedFiles  int    `json:"indexedFiles"`
	FailedFiles   int    `json:"failedFiles"`
	CurrentFile   string `json:"currentFile"`
	IsRunning     bool   `json:"isRunning"`
	QuotaPaused   bool   `json:"quotaPaused"`
	QuotaResumeAt string `json:"quotaResumeAt"`
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{}
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

	a.embedder, err = embedder.NewEmbedderFromEnv(768, a.logger)
	if err != nil {
		log.Warn("embedder not available", "error", err)
	}

	a.engine = search.New(a.store, a.index, a.logger)

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

	// Background goroutines.
	go a.watchEvents(eventCh)
	go a.startWatchingFolders()
	go a.emitStatusLoop()

	log.Info("startup complete")
}

// saveIndex persists the vector index to disk.
func (a *App) saveIndex() {
	if a.index == nil || a.indexPath == "" {
		return
	}
	if err := a.index.Save(a.indexPath); err != nil {
		a.logger.WithGroup("app").Error("failed to save index", "error", err)
	}
}

// shutdown is called when the Wails app is closing. It stops the pipeline,
// closes the watcher, saves the index, and closes the store.
func (a *App) shutdown(ctx context.Context) {
	log := a.logger.WithGroup("app")
	log.Info("shutting down")
	if a.pipeline != nil {
		a.pipeline.Stop()
	}
	if a.watcher != nil {
		a.watcher.Close()
	}
	a.saveIndex()
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
	vec, err := a.embedder.EmbedQuery(a.ctx, query)
	if err != nil {
		return nil, err
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
	for _, folder := range folders {
		a.pipeline.SubmitFolder(folder, patterns)
	}
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
		a.pipeline.SubmitFolder(path, patterns)
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
	exec.Command("xdg-open", path).Start()
}

// OpenFolder opens the folder containing the given file path.
func (a *App) OpenFolder(path string) {
	dir := filepath.Dir(path)
	exec.Command("xdg-open", dir).Start()
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

// watchEvents processes file watcher events and queues single-file indexing.
func (a *App) watchEvents(events <-chan watcher.FileEvent) {
	runtime.ResetSignalHandlers()
	for ev := range events {
		switch ev.Type {
		case watcher.FileCreated, watcher.FileModified:
			if a.pipeline != nil {
				a.pipeline.SubmitFile(ev.Path)
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
