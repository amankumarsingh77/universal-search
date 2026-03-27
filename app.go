package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"universal-search/internal/embedder"
	"universal-search/internal/indexer"
	"universal-search/internal/platform"
	"universal-search/internal/search"
	"universal-search/internal/store"
	"universal-search/internal/vectorstore"
	"universal-search/internal/watcher"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct holds all backend components for the Wails application.
type App struct {
	ctx      context.Context
	store    *store.Store
	index    *vectorstore.Index
	embedder *embedder.Embedder
	engine   *search.Engine
	pipeline *indexer.Pipeline
	watcher  *watcher.Watcher
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
	TotalFiles   int    `json:"totalFiles"`
	IndexedFiles int    `json:"indexedFiles"`
	CurrentFile  string `json:"currentFile"`
	IsRunning    bool   `json:"isRunning"`
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

	dbPath, err := platform.DBPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "db path: %v\n", err)
		return
	}

	a.store, err = store.NewStore(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "store: %v\n", err)
		return
	}

	indexPath, err := platform.IndexPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "index path: %v\n", err)
		return
	}

	a.index, err = vectorstore.LoadIndex(indexPath)
	if err != nil {
		a.index = vectorstore.NewIndex()
	}

	a.embedder, err = embedder.NewEmbedderFromEnv(768)
	if err != nil {
		fmt.Fprintf(os.Stderr, "embedder: %v\n", err)
	}

	a.engine = search.New(a.store, a.index)

	thumbDir, _ := platform.ThumbnailDir()
	a.pipeline = indexer.NewPipeline(a.store, a.index, a.embedder, thumbDir)

	// Start file watcher.
	eventCh := make(chan watcher.FileEvent, 100)
	a.watcher, err = watcher.New(eventCh, 500*time.Millisecond)
	if err != nil {
		fmt.Fprintf(os.Stderr, "watcher: %v\n", err)
	}

	// Background goroutines.
	go a.watchEvents(eventCh)
	go a.startWatchingFolders()
	go a.emitStatusLoop()
}

// shutdown is called when the Wails app is closing. It stops the pipeline,
// closes the watcher, saves the index, and closes the store.
func (a *App) shutdown(ctx context.Context) {
	if a.pipeline != nil {
		a.pipeline.Stop()
	}
	if a.watcher != nil {
		a.watcher.Close()
	}
	if a.index != nil {
		indexPath, _ := platform.IndexPath()
		a.index.Save(indexPath)
	}
	if a.store != nil {
		a.store.Close()
	}
}

// Search embeds the query via Gemini and returns the top search results.
func (a *App) Search(query string) ([]SearchResultDTO, error) {
	if a.embedder == nil {
		return nil, fmt.Errorf("embedder not initialized — set GEMINI_API_KEY")
	}

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
	return dtos, nil
}

// GetIndexStatus returns the current indexing pipeline status.
func (a *App) GetIndexStatus() IndexStatusDTO {
	if a.pipeline == nil {
		return IndexStatusDTO{}
	}
	s := a.pipeline.Status()
	return IndexStatusDTO{
		TotalFiles:   s.TotalFiles,
		IndexedFiles: s.IndexedFiles,
		CurrentFile:  s.CurrentFile,
		IsRunning:    s.IsRunning,
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
	go func() {
		runtime.ResetSignalHandlers()
		if a.store == nil || a.pipeline == nil {
			return
		}
		folders, _ := a.store.GetIndexedFolders()
		patterns, _ := a.store.GetExcludedPatterns()
		for _, folder := range folders {
			a.pipeline.IndexFolder(folder, patterns)
		}
	}()
}

// AddFolder adds a folder to the indexed folders list, starts watching it,
// and triggers indexing.
func (a *App) AddFolder(path string) error {
	if a.store == nil {
		return fmt.Errorf("store not initialized")
	}
	if err := a.store.AddIndexedFolder(path); err != nil {
		return err
	}
	if a.watcher != nil {
		a.watcher.Add(path)
	}
	// Trigger indexing for the newly added folder.
	go func() {
		runtime.ResetSignalHandlers()
		if a.pipeline != nil {
			patterns, _ := a.store.GetExcludedPatterns()
			a.pipeline.IndexFolder(path, patterns)
		}
	}()
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

	err = indexer.ExtractPreviewClip(videoPath, clipPath, timestamp, 15)
	return clipPath, err
}

// watchEvents processes file watcher events and triggers single-file indexing.
func (a *App) watchEvents(events <-chan watcher.FileEvent) {
	runtime.ResetSignalHandlers()
	for ev := range events {
		switch ev.Type {
		case watcher.FileCreated, watcher.FileModified:
			if a.pipeline != nil {
				a.pipeline.IndexSingleFile(ev.Path)
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
