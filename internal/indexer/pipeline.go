package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"universal-search/internal/chunker"
	"universal-search/internal/embedder"
	"universal-search/internal/store"
	"universal-search/internal/vectorstore"
)

type IndexStatus struct {
	TotalFiles    int
	IndexedFiles  int
	FailedFiles   int
	CurrentFile   string
	IsRunning     bool
	Paused        bool
	QuotaPaused   bool
	QuotaResumeAt string
}

type jobType int

const (
	jobFolder jobType = iota
	jobSingleFile
)

const saveInterval = 50

type indexJob struct {
	typ             jobType
	folderPath      string
	excludePatterns []string
	filePath        string
	force           bool
}

type OnJobDone func()

type Pipeline struct {
	store    *store.Store
	index    *vectorstore.Index
	embedder *embedder.Embedder
	thumbDir string
	logger   *slog.Logger

	mu         sync.RWMutex
	embedderMu sync.RWMutex
	status     IndexStatus
	paused     bool
	pauseCh    chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc

	jobCh      chan indexJob
	onJobDone  OnJobDone
	workerWg   sync.WaitGroup
	pendingJobs atomic.Int32
	generation  atomic.Int32

	chunksSinceLastSave int // protected by mu
}

func NewPipeline(s *store.Store, idx *vectorstore.Index, emb *embedder.Embedder, thumbDir string, logger *slog.Logger, onDone OnJobDone) *Pipeline {
	ctx, cancel := context.WithCancel(context.Background())
	log := logger.WithGroup("indexer")
	log.Info("pipeline created", "thumbDir", thumbDir)
	p := &Pipeline{
		store:     s,
		index:     idx,
		embedder:  emb,
		thumbDir:  thumbDir,
		logger:    log,
		pauseCh:   make(chan struct{}, 1),
		ctx:       ctx,
		cancel:    cancel,
		jobCh:     make(chan indexJob, 64),
		onJobDone: onDone,
	}
	p.workerWg.Add(1)
	go p.worker()
	return p
}

// SetEmbedder atomically replaces the pipeline's embedder.
// Safe to call while the worker goroutine is running.
func (p *Pipeline) SetEmbedder(e *embedder.Embedder) {
	p.embedderMu.Lock()
	p.embedder = e
	p.embedderMu.Unlock()
}

func (p *Pipeline) Status() IndexStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s := p.status
	s.Paused = p.paused
	return s
}

// SetTotalFiles sets the TotalFiles counter and marks indexing as running.
// Call before submitting force-reindex jobs to show an accurate total upfront.
func (p *Pipeline) SetTotalFiles(n int) {
	p.mu.Lock()
	p.status.TotalFiles = n
	p.status.IsRunning = true
	p.mu.Unlock()
}

// ResetStatus resets indexing counters to zero. Call before starting a new reindex run.
func (p *Pipeline) ResetStatus() {
	p.generation.Add(1)
	p.mu.Lock()
	p.status.TotalFiles = 0
	p.status.IndexedFiles = 0
	p.status.FailedFiles = 0
	p.status.CurrentFile = ""
	p.mu.Unlock()
}

func (p *Pipeline) Pause() {
	p.logger.Info("pipeline paused")
	p.mu.Lock()
	p.paused = true
	p.mu.Unlock()
}

func (p *Pipeline) Resume() {
	p.logger.Info("pipeline resumed")
	p.mu.Lock()
	p.paused = false
	p.mu.Unlock()
	select {
	case p.pauseCh <- struct{}{}:
	default:
	}
}

func (p *Pipeline) Stop() {
	p.logger.Info("pipeline stopping")
	p.cancel()
	p.workerWg.Wait()
}

func (p *Pipeline) SubmitFolder(folderPath string, excludePatterns []string, force bool) {
	p.pendingJobs.Add(1)
	select {
	case p.jobCh <- indexJob{typ: jobFolder, folderPath: folderPath, excludePatterns: excludePatterns, force: force}:
	case <-p.ctx.Done():
		p.pendingJobs.Add(-1)
	}
}

func (p *Pipeline) SubmitFile(filePath string) {
	p.pendingJobs.Add(1)
	select {
	case p.jobCh <- indexJob{typ: jobSingleFile, filePath: filePath}:
	case <-p.ctx.Done():
		p.pendingJobs.Add(-1)
	}
}

// DeleteFile removes a file from the store and vector index.
func (p *Pipeline) DeleteFile(filePath string) {
	vecIDs, err := p.store.RemoveFileByPath(filePath)
	if err != nil {
		p.logger.Debug("delete file skipped", "path", filePath, "error", err)
		return
	}
	for _, vid := range vecIDs {
		p.index.Delete(vid)
	}
	if len(vecIDs) > 0 && p.onJobDone != nil {
		p.onJobDone()
	}
	p.logger.Info("deleted file from index", "path", filePath, "vectors", len(vecIDs))
}

func (p *Pipeline) worker() {
	defer p.workerWg.Done()
	for {
		select {
		case job := <-p.jobCh:
			switch job.typ {
			case jobFolder:
				p.processFolder(job.folderPath, job.excludePatterns, job.force)
			case jobSingleFile:
				p.processSingleFile(job.filePath)
			}
			remaining := p.pendingJobs.Add(-1)
			if remaining == 0 {
				p.mu.Lock()
				p.status.IsRunning = false
				p.status.CurrentFile = ""
				p.mu.Unlock()
			}
			if p.onJobDone != nil {
				p.onJobDone()
			}
		case <-p.ctx.Done():
			return
		}
	}
}

func (p *Pipeline) waitIfPaused() {
	for {
		p.mu.RLock()
		paused := p.paused
		p.mu.RUnlock()
		if !paused {
			return
		}
		select {
		case <-p.pauseCh:
		case <-p.ctx.Done():
			return
		}
	}
}

func (p *Pipeline) processFolder(folderPath string, excludePatterns []string, force bool) {
	p.logger.Info("indexing folder", "path", folderPath, "excludePatterns", len(excludePatterns))
	start := time.Now()

	var files []string
	err := filepath.WalkDir(folderPath, func(path string, d os.DirEntry, err error) error {
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
		ft := chunker.Classify(path)
		if ft != chunker.TypeUnknown {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		p.logger.Error("folder walk failed", "path", folderPath, "error", err)
		return
	}

	p.logger.Info("discovered files", "count", len(files), "folder", folderPath)

	if !force {
		p.mu.Lock()
		p.status.TotalFiles += len(files)
		p.status.IsRunning = true
		p.mu.Unlock()
	}

	gen := p.generation.Load()

	for _, filePath := range files {
		// Cancel if a new reindex was triggered.
		if p.generation.Load() != gen {
			p.logger.Info("reindex generation changed, cancelling in-flight run", "folder", folderPath)
			return
		}

		select {
		case <-p.ctx.Done():
			return
		default:
		}

		p.waitIfPaused()

		p.mu.Lock()
		p.status.CurrentFile = filePath
		p.mu.Unlock()

		if err := p.indexFile(filePath, force); err != nil {
			p.logger.Warn("file indexing failed", "path", filePath, "error", err)
			p.mu.Lock()
			p.status.FailedFiles++
			if isQuotaExhaustedError(err) {
				p.status.QuotaPaused = true
				p.status.QuotaResumeAt = time.Now().Add(30 * time.Minute).Format(time.RFC3339)
				p.logger.Error("all API keys exhausted, pausing indexing", "resumeAt", p.status.QuotaResumeAt)
			}
			p.mu.Unlock()

			if isQuotaExhaustedError(err) {
				p.waitForQuotaRecovery()
			}
		} else {
			p.mu.Lock()
			p.status.IndexedFiles++
			p.mu.Unlock()
		}
	}

	p.mu.RLock()
	indexed := p.status.IndexedFiles
	failed := p.status.FailedFiles
	total := p.status.TotalFiles
	p.mu.RUnlock()

	p.logger.Info("folder indexing complete",
		"folder", folderPath,
		"indexed", indexed,
		"failed", failed,
		"total", total,
		"duration", time.Since(start),
	)
}

func (p *Pipeline) processSingleFile(filePath string) {
	p.mu.Lock()
	p.status.TotalFiles++
	p.status.IsRunning = true
	p.status.CurrentFile = filePath
	p.mu.Unlock()

	if err := p.indexFile(filePath, false); err != nil {
		p.logger.Warn("single file indexing failed", "path", filePath, "error", err)
		p.mu.Lock()
		p.status.FailedFiles++
		p.mu.Unlock()
	} else {
		p.mu.Lock()
		p.status.IndexedFiles++
		p.mu.Unlock()
	}
}

func (p *Pipeline) indexFile(filePath string, force bool) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}

	hash, err := hashFile(filePath)
	if err != nil {
		return err
	}

	if !force {
		existing, err := p.store.GetFileByPath(filePath)
		if err == nil && existing.ContentHash == hash {
			p.logger.Debug("skipping unchanged file", "path", filePath)
			return nil
		}
	}

	chunks, fileType, err := chunker.ChunkFile(filePath)
	if err != nil {
		return err
	}
	if len(chunks) == 0 {
		return nil
	}

	ext := filepath.Ext(filePath)
	p.logger.Debug("indexing file", "path", filePath, "type", string(fileType), "size", info.Size(), "chunks", len(chunks))

	thumbPath, thumbErr := GenerateThumbnail(filePath, p.thumbDir, string(fileType))
	if thumbErr != nil {
		p.logger.Warn("thumbnail generation failed", "path", filePath, "error", thumbErr)
	}

	// Phase 1: register file with empty hash — not yet fully indexed.
	fileID, err := p.store.UpsertFile(store.FileRecord{
		Path:          filePath,
		FileType:      string(fileType),
		Extension:     ext,
		SizeBytes:     info.Size(),
		ModifiedAt:    info.ModTime(),
		IndexedAt:     time.Now(),
		ContentHash:   "",
		ThumbnailPath: thumbPath,
	})
	if err != nil {
		return err
	}

	// Delete old vectors/chunks before re-embedding.
	oldVecIDs, _ := p.store.GetVectorIDsByFileID(fileID)
	for _, vid := range oldVecIDs {
		p.index.Delete(vid)
	}
	p.store.DeleteChunksByFileID(fileID)

	p.embedderMu.RLock()
	emb := p.embedder
	p.embedderMu.RUnlock()
	if emb == nil {
		return fmt.Errorf("embedder not initialized — set GEMINI_API_KEY")
	}

	// Phase 2: embed all chunks. Track whether all succeed.
	allSucceeded := true
	fileName := filepath.Base(filePath)
	for _, chunk := range chunks {
		var vec []float32

		if chunk.Text != "" {
			vec, err = emb.EmbedDocumentWithTitle(p.ctx, fileName, chunk.Text)
		} else if len(chunk.Content) > 0 {
			vec, err = emb.EmbedBytes(p.ctx, chunk.Content, chunk.MimeType, fileName)
		} else {
			continue
		}

		if err != nil {
			p.logger.Warn("embedding failed", "path", filePath, "chunk", chunk.Index, "error", err)
			allSucceeded = false
			continue
		}

		vecID := fmt.Sprintf("f%d-c%d", fileID, chunk.Index)
		if addErr := p.index.Add(vecID, vec); addErr != nil {
			p.logger.Warn("adding vector failed", "path", filePath, "chunk", chunk.Index, "error", addErr)
			allSucceeded = false
			continue
		}

		p.store.InsertChunk(store.ChunkRecord{
			FileID:     fileID,
			VectorID:   vecID,
			StartTime:  chunk.StartTime,
			EndTime:    chunk.EndTime,
			ChunkIndex: chunk.Index,
		})

		// Periodic HNSW save every saveInterval chunks.
		p.mu.Lock()
		p.chunksSinceLastSave++
		shouldSave := p.chunksSinceLastSave >= saveInterval
		if shouldSave {
			p.chunksSinceLastSave = 0
		}
		p.mu.Unlock()
		if shouldSave && p.onJobDone != nil {
			p.onJobDone()
		}
	}

	// Phase 3: commit content_hash only if all chunks succeeded.
	if allSucceeded {
		if err := p.store.UpdateContentHash(fileID, hash); err != nil {
			p.logger.Error("failed to update content hash", "path", filePath, "error", err)
			return err
		}
		return nil
	}

	p.logger.Warn("some chunks failed — content_hash not committed, file will be re-indexed on next startup", "path", filePath)
	return fmt.Errorf("one or more chunks failed to embed for %s", filePath)
}

func isQuotaExhaustedError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "keys exhausted") ||
		strings.Contains(s, "keys are cooling or exhausted")
}

func (p *Pipeline) waitForQuotaRecovery() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.mu.Lock()
			p.status.QuotaPaused = false
			p.status.QuotaResumeAt = ""
			p.mu.Unlock()
			p.logger.Info("quota recovery check, resuming indexing")
			return
		}
	}
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
