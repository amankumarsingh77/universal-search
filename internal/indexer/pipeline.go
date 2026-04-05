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
	QuotaPaused   bool
	QuotaResumeAt string
}

type jobType int

const (
	jobFolder jobType = iota
	jobSingleFile
)

type indexJob struct {
	typ             jobType
	folderPath      string
	excludePatterns []string
	filePath        string
}

type OnJobDone func()

type Pipeline struct {
	store    *store.Store
	index    *vectorstore.Index
	embedder *embedder.Embedder
	thumbDir string
	logger   *slog.Logger

	mu      sync.RWMutex
	status  IndexStatus
	paused  bool
	pauseCh chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc

	jobCh      chan indexJob
	onJobDone  OnJobDone
	workerWg   sync.WaitGroup
	pendingJobs atomic.Int32
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

func (p *Pipeline) Status() IndexStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.status
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

func (p *Pipeline) SubmitFolder(folderPath string, excludePatterns []string) {
	p.pendingJobs.Add(1)
	select {
	case p.jobCh <- indexJob{typ: jobFolder, folderPath: folderPath, excludePatterns: excludePatterns}:
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

func (p *Pipeline) worker() {
	defer p.workerWg.Done()
	for {
		select {
		case job := <-p.jobCh:
			switch job.typ {
			case jobFolder:
				p.processFolder(job.folderPath, job.excludePatterns)
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

func (p *Pipeline) processFolder(folderPath string, excludePatterns []string) {
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

	p.mu.Lock()
	p.status.TotalFiles += len(files)
	p.status.IsRunning = true
	p.mu.Unlock()

	for _, filePath := range files {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		p.waitIfPaused()

		p.mu.Lock()
		p.status.CurrentFile = filePath
		p.mu.Unlock()

		if err := p.indexFile(filePath); err != nil {
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

	p.logger.Info("folder indexing complete",
		"folder", folderPath,
		"indexed", p.status.IndexedFiles,
		"failed", p.status.FailedFiles,
		"total", p.status.TotalFiles,
		"duration", time.Since(start),
	)
}

func (p *Pipeline) processSingleFile(filePath string) {
	p.mu.Lock()
	p.status.TotalFiles++
	p.status.IsRunning = true
	p.status.CurrentFile = filePath
	p.mu.Unlock()

	if err := p.indexFile(filePath); err != nil {
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

func (p *Pipeline) indexFile(filePath string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}

	hash, err := hashFile(filePath)
	if err != nil {
		return err
	}

	existing, err := p.store.GetFileByPath(filePath)
	if err == nil && existing.ContentHash == hash {
		p.logger.Debug("skipping unchanged file", "path", filePath)
		return nil
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

	fileID, err := p.store.UpsertFile(store.FileRecord{
		Path:          filePath,
		FileType:      string(fileType),
		Extension:     ext,
		SizeBytes:     info.Size(),
		ModifiedAt:    info.ModTime(),
		IndexedAt:     time.Now(),
		ContentHash:   hash,
		ThumbnailPath: thumbPath,
	})
	if err != nil {
		return err
	}

	oldVecIDs, _ := p.store.GetVectorIDsByFileID(fileID)
	for _, vid := range oldVecIDs {
		p.index.Delete(vid)
	}
	p.store.DeleteChunksByFileID(fileID)

	fileName := filepath.Base(filePath)
	for _, chunk := range chunks {
		var vec []float32

		if chunk.Text != "" {
			vec, err = p.embedder.EmbedDocumentWithTitle(p.ctx, fileName, chunk.Text)
		} else if len(chunk.Content) > 0 {
			vec, err = p.embedder.EmbedBytes(p.ctx, chunk.Content, chunk.MimeType, fileName)
		} else {
			continue
		}

		if err != nil {
			p.logger.Warn("embedding failed", "path", filePath, "chunk", chunk.Index, "error", err)
			continue
		}

		vecID := fmt.Sprintf("f%d-c%d", fileID, chunk.Index)
		if err := p.index.Add(vecID, vec); err != nil {
			p.logger.Warn("adding vector failed", "path", filePath, "chunk", chunk.Index, "error", err)
			continue
		}

		p.store.InsertChunk(store.ChunkRecord{
			FileID:     fileID,
			VectorID:   vecID,
			StartTime:  chunk.StartTime,
			EndTime:    chunk.EndTime,
			ChunkIndex: chunk.Index,
		})
	}

	return nil
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
