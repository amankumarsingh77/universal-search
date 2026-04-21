package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

// IndexStatus is a point-in-time snapshot of indexing progress and state.
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

// PipelineConfig holds tunable parameters for the indexing pipeline.
type PipelineConfig struct {
	Workers      int
	JobQueueSize int
	SaveEveryN   int
}

// DefaultPipelineConfig returns the historical defaults used before config wiring.
func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{Workers: 4, JobQueueSize: 64, SaveEveryN: 50}
}

// errStaleGeneration signals that a file's embedding results were discarded
// because a new generation (e.g. from ResetStatus) superseded the run. Callers
// must treat this as neither success nor failure — the file is simply skipped.
var errStaleGeneration = errors.New("stale generation: embedding discarded")

type indexJob struct {
	typ             jobType
	folderPath      string
	excludePatterns []string
	filePath        string
	force           bool
}

// OnJobDone is a callback invoked after each indexing job completes.
type OnJobDone func()

// Pipeline coordinates file indexing across worker goroutines.
type Pipeline struct {
	store    *store.Store
	index    *vectorstore.Index
	embedder embedder.Embedder
	thumbDir string
	logger   *slog.Logger

	mu         sync.RWMutex
	embedderMu sync.RWMutex
	status     IndexStatus
	paused     bool
	pauseCh    chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc

	jobCh       chan indexJob
	onJobDone   OnJobDone
	workerWg    sync.WaitGroup
	pendingJobs atomic.Int32
	generation  atomic.Int32
	workerCount int
	saveEveryN  int

	chunksSinceLastSave int // protected by mu
}

// NewPipeline builds a Pipeline with the given runtime config. Zero values in
// cfg are filled from DefaultPipelineConfig so callers can supply partial
// configs in tests.
func NewPipeline(s *store.Store, idx *vectorstore.Index, emb embedder.Embedder, thumbDir string, logger *slog.Logger, onDone OnJobDone, cfg PipelineConfig) *Pipeline {
	ctx, cancel := context.WithCancel(context.Background())
	log := logger.WithGroup("indexer")
	def := DefaultPipelineConfig()
	if cfg.Workers <= 0 {
		cfg.Workers = def.Workers
	}
	if cfg.JobQueueSize <= 0 {
		cfg.JobQueueSize = def.JobQueueSize
	}
	if cfg.SaveEveryN <= 0 {
		cfg.SaveEveryN = def.SaveEveryN
	}
	log.Info("pipeline created", "thumbDir", thumbDir, "workers", cfg.Workers, "queueSize", cfg.JobQueueSize, "saveEveryN", cfg.SaveEveryN)
	p := &Pipeline{
		store:       s,
		index:       idx,
		embedder:    emb,
		thumbDir:    thumbDir,
		logger:      log,
		pauseCh:     make(chan struct{}, 1),
		ctx:         ctx,
		cancel:      cancel,
		jobCh:       make(chan indexJob, cfg.JobQueueSize),
		onJobDone:   onDone,
		workerCount: cfg.Workers,
		saveEveryN:  cfg.SaveEveryN,
	}
	for i := 0; i < cfg.Workers; i++ {
		p.workerWg.Add(1)
		go p.worker()
	}
	return p
}

// SetEmbedder atomically replaces the pipeline's embedder.
// Safe to call while the worker goroutine is running.
func (p *Pipeline) SetEmbedder(e embedder.Embedder) {
	p.embedderMu.Lock()
	p.embedder = e
	p.embedderMu.Unlock()
}

// getEmbedder returns the active embedder. Returns nil if not configured.
func (p *Pipeline) getEmbedder() embedder.Embedder {
	p.embedderMu.RLock()
	defer p.embedderMu.RUnlock()
	return p.embedder
}

// Status returns a snapshot of the current indexing progress.
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

// Pause halts worker processing of new jobs until Resume is called.
func (p *Pipeline) Pause() {
	p.logger.Info("pipeline paused")
	p.mu.Lock()
	p.paused = true
	p.mu.Unlock()
}

// Resume wakes the pipeline from a paused state so workers can process new jobs.
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

// Stop cancels the pipeline context and waits for workers to exit.
func (p *Pipeline) Stop() {
	p.logger.Info("pipeline stopping")
	p.cancel()
	p.workerWg.Wait()
}

// SubmitFolder queues a folder-walking job onto the pipeline.
func (p *Pipeline) SubmitFolder(folderPath string, excludePatterns []string, force bool) {
	p.pendingJobs.Add(1)
	select {
	case p.jobCh <- indexJob{typ: jobFolder, folderPath: folderPath, excludePatterns: excludePatterns, force: force}:
	case <-p.ctx.Done():
		p.pendingJobs.Add(-1)
	}
}

// SubmitFile queues a single-file indexing job onto the pipeline.
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

	// Process files concurrently using a semaphore-bounded pool.
	// We do NOT route back through jobCh — that would deadlock because this
	// function is itself running inside a worker. Instead, we spawn goroutines
	// directly here, bounded by workerCount to respect the rate limiter budget.
	concurrency := p.workerCount
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

fileLoop:
	for _, filePath := range files {
		if p.generation.Load() != gen {
			p.logger.Info("reindex generation changed, cancelling folder run", "folder", folderPath)
			break
		}
		select {
		case <-p.ctx.Done():
			break fileLoop
		default:
		}

		p.waitIfPaused()

		sem <- struct{}{} // acquire slot
		wg.Add(1)
		fp := filePath
		go func() {
			defer wg.Done()
			defer func() { <-sem }() // release slot

			p.mu.Lock()
			p.status.CurrentFile = fp
			p.mu.Unlock()

			if err := p.indexFile(p.ctx, fp, force); err != nil {
				if errors.Is(err, errStaleGeneration) {
					// Discarded due to generation change — neither success nor failure.
					return
				}
				p.logger.Warn("file indexing failed", "path", fp, "error", err)
				p.mu.Lock()
				p.status.FailedFiles++
				if isQuotaExhaustedError(err) {
					resumeAt := p.quotaResumeTime()
					p.status.QuotaPaused = true
					p.status.QuotaResumeAt = resumeAt.Format(time.RFC3339)
					p.logger.Error("quota exhausted, pausing indexing", "resumeAt", resumeAt)
				}
				p.mu.Unlock()
			} else {
				p.mu.Lock()
				p.status.IndexedFiles++
				p.mu.Unlock()
			}
		}()
	}

	wg.Wait()

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

	if err := p.indexFile(p.ctx, filePath, false); err != nil {
		if errors.Is(err, errStaleGeneration) {
			return
		}
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

func (p *Pipeline) indexFile(ctx context.Context, filePath string, force bool) error {
	gen := p.generation.Load()

	stale, info, hash, err := p.checkStale(filePath, force)
	if err != nil {
		return err
	}
	if !stale {
		return nil
	}

	chunks, fileType, err := p.chunk(filePath)
	if err != nil {
		return err
	}
	if len(chunks) == 0 {
		return nil
	}

	thumbPath := p.generateThumbnail(filePath, info, fileType)

	fileID, err := p.upsertPending(filePath, info, fileType, thumbPath)
	if err != nil {
		return err
	}

	vectors, batchChunks, err := p.embedBatched(ctx, filePath, fileID, chunks, hash)
	if err != nil {
		return err
	}
	if vectors == nil && batchChunks == nil {
		// All chunks were empty; hash already committed inside embedBatched.
		return nil
	}

	if p.generation.Load() != gen {
		p.logger.Info("generation changed mid-batch, discarding results", "path", filePath)
		return errStaleGeneration
	}

	if err := p.storeChunks(fileID, batchChunks, vectors); err != nil {
		return fmt.Errorf("one or more chunks failed to embed for %s", filePath)
	}

	return p.commit(fileID, hash)
}

// checkStale decides whether filePath needs (re-)indexing. It returns the stat
// info and the freshly computed content hash so later phases can reuse them
// without a second stat/open.
func (p *Pipeline) checkStale(path string, force bool) (bool, fs.FileInfo, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, nil, "", err
	}
	hash, err := hashFile(path)
	if err != nil {
		return false, nil, "", err
	}
	if !force {
		existing, err := p.store.GetFileByPath(path)
		if err == nil && existing.ContentHash == hash {
			p.logger.Debug("skipping unchanged file", "path", path)
			return false, info, hash, nil
		}
	}
	return true, info, hash, nil
}

// chunk runs the chunker registry on path and returns the chunks plus detected
// file type (needed for thumbnailing and the file row).
func (p *Pipeline) chunk(path string) ([]chunker.Chunk, chunker.FileType, error) {
	return chunker.ChunkFile(path)
}

// generateThumbnail writes a thumbnail if supported; errors are logged only.
func (p *Pipeline) generateThumbnail(path string, info fs.FileInfo, fileType chunker.FileType) string {
	p.logger.Debug("indexing file", "path", path, "type", string(fileType), "size", info.Size())
	thumbPath, err := GenerateThumbnail(path, p.thumbDir, string(fileType))
	if err != nil {
		p.logger.Warn("thumbnail generation failed", "path", path, "error", err)
	}
	return thumbPath
}

// upsertPending writes the file row with an empty content_hash (Phase 1 of the
// two-phase commit) and clears any stale vectors/chunks from a prior run.
func (p *Pipeline) upsertPending(path string, info fs.FileInfo, fileType chunker.FileType, thumbPath string) (int64, error) {
	ext := strings.ToLower(filepath.Ext(path))
	fileID, err := p.store.UpsertFile(store.FileRecord{
		Path:          path,
		FileType:      string(fileType),
		Extension:     ext,
		SizeBytes:     info.Size(),
		ModifiedAt:    info.ModTime(),
		IndexedAt:     time.Now(),
		ContentHash:   "",
		ThumbnailPath: thumbPath,
	})
	if err != nil {
		return 0, err
	}
	oldVecIDs, _ := p.store.GetVectorIDsByFileID(fileID)
	for _, vid := range oldVecIDs {
		p.index.Delete(vid)
	}
	p.store.DeleteChunksByFileID(fileID)
	return fileID, nil
}

// embedBatched filters empty chunks, calls the embedder once, and returns the
// vectors aligned with the surviving chunks. If every chunk was empty, it
// commits the hash in place (so we don't re-scan the file forever) and returns
// nil slices to signal the coordinator to stop. Quota errors are translated
// into QuotaPaused/QuotaResumeAt status fields.
func (p *Pipeline) embedBatched(ctx context.Context, path string, fileID int64, chunks []chunker.Chunk, hash string) ([][]float32, []chunker.Chunk, error) {
	emb := p.getEmbedder()
	if emb == nil {
		return nil, nil, fmt.Errorf("embedder not initialized — set GEMINI_API_KEY")
	}

	fileName := filepath.Base(path)
	batchInputs := make([]embedder.ChunkInput, 0, len(chunks))
	batchChunks := make([]chunker.Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		switch {
		case chunk.Text != "":
			batchInputs = append(batchInputs, embedder.ChunkInput{Title: fileName, Text: chunk.Text})
			batchChunks = append(batchChunks, chunk)
		case len(chunk.Content) > 0:
			batchInputs = append(batchInputs, embedder.ChunkInput{Title: fileName, MIMEType: chunk.MimeType, Data: chunk.Content})
			batchChunks = append(batchChunks, chunk)
		}
	}
	if len(batchInputs) == 0 {
		p.logger.Debug("file has only empty chunks, committing hash without embedding", "path", path, "chunks", len(chunks))
		if err := p.store.UpdateContentHash(fileID, hash); err != nil {
			p.logger.Error("failed to update content hash", "path", path, "error", err)
			return nil, nil, err
		}
		return nil, nil, nil
	}

	vecs, err := emb.EmbedBatch(ctx, batchInputs)
	if err != nil {
		if isQuotaExhaustedError(err) {
			resumeAt := p.quotaResumeTime()
			p.mu.Lock()
			p.status.QuotaPaused = true
			p.status.QuotaResumeAt = resumeAt.Format(time.RFC3339)
			p.mu.Unlock()
			p.logger.Warn("quota exhausted, pipeline paused", "resumeAt", resumeAt)
		}
		p.logger.Warn("batch embedding failed", "path", path, "error", err)
		return nil, nil, fmt.Errorf("one or more chunks failed to embed for %s", path)
	}
	if len(vecs) != len(batchChunks) {
		p.logger.Warn("embedding count mismatch", "path", path, "chunks", len(batchChunks), "vecs", len(vecs))
		return nil, nil, fmt.Errorf("embedding count mismatch for %s: got %d vecs for %d chunks", path, len(vecs), len(batchChunks))
	}
	return vecs, batchChunks, nil
}

// storeChunks writes vectors to HNSW and chunk rows to SQLite, triggering the
// periodic HNSW save every saveEveryN inserted chunks.
func (p *Pipeline) storeChunks(fileID int64, chunks []chunker.Chunk, vectors [][]float32) error {
	emb := p.getEmbedder()
	var modelID string
	var dims int
	if emb != nil {
		modelID = emb.ModelID()
		dims = emb.Dimensions()
	}
	for i, vec := range vectors {
		chunk := chunks[i]
		vecID := fmt.Sprintf("f%d-c%d", fileID, chunk.Index)
		if err := p.index.Add(vecID, vec); err != nil {
			p.logger.Warn("adding vector failed", "fileID", fileID, "chunk", chunk.Index, "error", err)
			return err
		}
		p.store.InsertChunk(store.ChunkRecord{
			FileID:         fileID,
			VectorID:       vecID,
			StartTime:      chunk.StartTime,
			EndTime:        chunk.EndTime,
			ChunkIndex:     chunk.Index,
			VectorBlob:     store.VecToBlob(vec),
			EmbeddingModel: modelID,
			EmbeddingDims:  dims,
		})

		p.mu.Lock()
		p.chunksSinceLastSave++
		shouldSave := p.chunksSinceLastSave >= p.saveEveryN
		if shouldSave {
			p.chunksSinceLastSave = 0
		}
		p.mu.Unlock()
		if shouldSave && p.onJobDone != nil {
			p.onJobDone()
		}
	}
	return nil
}

// commit writes the final content_hash (Phase 2 of the two-phase commit). Once
// this succeeds the file is considered fully indexed.
func (p *Pipeline) commit(fileID int64, hash string) error {
	if err := p.store.UpdateContentHash(fileID, hash); err != nil {
		p.logger.Error("failed to update content hash", "fileID", fileID, "error", err)
		return err
	}
	return nil
}

// quotaResumeTime returns the time when the quota pause is expected to expire.
// It reads from the embedder's rate limiter when available; otherwise falls back
// to a 30-second default from now.
func (p *Pipeline) quotaResumeTime() time.Time {
	p.embedderMu.RLock()
	emb := p.embedder
	p.embedderMu.RUnlock()
	if g, ok := emb.(interface{ Limiter() *embedder.RateLimiter }); ok {
		if t := g.Limiter().PausedUntil(); !t.IsZero() {
			return t
		}
	}
	return time.Now().Add(30 * time.Second)
}

func isQuotaExhaustedError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "keys exhausted") ||
		strings.Contains(s, "keys are cooling or exhausted")
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
