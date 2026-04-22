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

	"findo/internal/apperr"
	"findo/internal/chunker"
	"findo/internal/embedder"
	"findo/internal/store"
	"findo/internal/vectorstore"
)

// vectorIndexer abstracts the HNSW index operations needed by the pipeline.
// *vectorstore.Index satisfies this interface.
type vectorIndexer interface {
	Add(id string, vec []float32) error
	Delete(id string) bool
	Has(id string) bool
}

// maxAttempts is the maximum number of indexing attempts per file (including the
// initial attempt). If a transient error persists beyond this threshold, the failure
// is recorded as terminal.
const maxAttempts = 3

// IndexStatus is a point-in-time snapshot of indexing progress and state.
type IndexStatus struct {
	TotalFiles        int
	IndexedFiles      int
	FailedFiles       int
	PendingRetryFiles int
	CurrentFile       string
	IsRunning         bool
	Paused            bool
	QuotaPaused       bool
	QuotaResumeAt     string
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
	attempts        int // 0 = first attempt (treated as attempt 1 in the worker)
}

// OnJobDone is a callback invoked after each indexing job completes.
type OnJobDone func()

// Pipeline coordinates file indexing across worker goroutines.
type Pipeline struct {
	store    *store.Store
	index    vectorIndexer
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

	// Failure tracking (Phase 5).
	registry   *FailureRegistry
	retryCoord *RetryCoordinator
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
		registry:    NewFailureRegistry(10000),
	}
	// Wire up the retry coordinator using the embedder's rate limiter when available.
	var limiter *embedder.RateLimiter
	if g, ok := emb.(interface{ Limiter() *embedder.RateLimiter }); ok {
		limiter = g.Limiter()
	}
	if limiter == nil {
		limiter = embedder.NewRateLimiter(55, time.Minute)
	}
	p.retryCoord = NewRetryCoordinator(p, limiter)
	p.retryCoord.Start()

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
// Also clears the failure registry and drops all pending retries (REQ-013, REQ-025).
func (p *Pipeline) ResetStatus() {
	currentGen := p.generation.Add(1)
	p.mu.Lock()
	p.status.TotalFiles = 0
	p.status.IndexedFiles = 0
	p.status.FailedFiles = 0
	p.status.PendingRetryFiles = 0
	p.status.CurrentFile = ""
	p.mu.Unlock()
	if p.registry != nil {
		p.registry.Reset()
	}
	if p.retryCoord != nil {
		p.retryCoord.DropAll(currentGen)
	}
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
	if p.retryCoord != nil {
		p.retryCoord.Stop()
	}
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
//
// EDGE-010: if the file is currently pending retry, the pending retry is cancelled
// (PendingRetryFiles decremented) and a fresh job is submitted instead. This
// prevents duplicate attempts for the same path. The retry coordinator will drop
// the stale job at dequeue (stamp mismatch) and decrement its own pendingCount then.
func (p *Pipeline) SubmitFile(filePath string) {
	// Cancel any pending retry for this path (EDGE-010).
	if p.retryCoord != nil {
		if p.retryCoord.CancelPath(filePath) {
			// Decrement PendingRetryFiles immediately so the UI reflects the collapse.
			// The coordinator will decrement its own pendingCount when it dequeues the
			// stale job — do NOT decrement pendingCount here to avoid double-decrement.
			p.mu.Lock()
			if p.status.PendingRetryFiles > 0 {
				p.status.PendingRetryFiles--
			}
			p.mu.Unlock()
		}
	}
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
				if job.attempts > 0 {
					// Re-submitted by the retry coordinator — do NOT increment TotalFiles.
					p.processRetryFile(job.filePath, job.attempts)
				} else {
					p.processSingleFile(job.filePath, 0)
				}
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
				p.handleFileError(fp, 1, err)
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

func (p *Pipeline) processSingleFile(filePath string, attempts int) {
	p.mu.Lock()
	p.status.TotalFiles++
	p.status.IsRunning = true
	p.status.CurrentFile = filePath
	p.mu.Unlock()

	curAttempts := attempts
	if curAttempts <= 0 {
		curAttempts = 1
	}

	if err := p.indexFile(p.ctx, filePath, false); err != nil {
		p.handleFileError(filePath, curAttempts, err)
	} else {
		p.mu.Lock()
		p.status.IndexedFiles++
		p.mu.Unlock()
	}
}

// processRetryFile handles a file that was re-submitted by the retry coordinator.
// Unlike processSingleFile it does NOT increment TotalFiles (REQ-032).
func (p *Pipeline) processRetryFile(filePath string, attempts int) {
	p.mu.Lock()
	p.status.CurrentFile = filePath
	p.mu.Unlock()

	curAttempts := attempts
	if curAttempts <= 0 {
		curAttempts = 1
	}

	if err := p.indexFile(p.ctx, filePath, false); err != nil {
		p.handleFileError(filePath, curAttempts, err)
	} else {
		p.mu.Lock()
		p.status.IndexedFiles++
		p.mu.Unlock()
	}
}

// Registry returns the pipeline's failure registry. Never nil after NewPipeline.
func (p *Pipeline) Registry() *FailureRegistry {
	return p.registry
}

// handleFileError classifies err and either records a terminal failure or
// schedules a retry. It handles the errStaleGeneration sentinel by discarding
// without any counter change (EDGE-001).
//
// curAttempts is the number of attempts including the one that just failed.
func (p *Pipeline) handleFileError(filePath string, curAttempts int, err error) {
	if errors.Is(err, errStaleGeneration) {
		// Discarded due to generation change — neither success nor failure (EDGE-001).
		return
	}

	// Extract apperr.Error; fall back to ERR_INTERNAL/Permanent for raw errors (REQ-004).
	var appErr *apperr.Error
	code := apperr.ErrInternal.Code
	msg := err.Error()
	if errors.As(err, &appErr) {
		code = appErr.Code
		msg = appErr.Message
	}

	// Handle quota status update alongside classification.
	if errors.Is(err, apperr.ErrRateLimited) {
		resumeAt := p.quotaResumeTime()
		p.mu.Lock()
		p.status.QuotaPaused = true
		p.status.QuotaResumeAt = resumeAt.Format(time.RFC3339)
		p.mu.Unlock()
		p.logger.Warn("quota exhausted", "path", filePath, "code", code, "resumeAt", resumeAt)
	}

	cls := apperr.Classify(err)
	if cls == apperr.ClassPermanent || curAttempts >= maxAttempts {
		// Terminal failure: record in registry and increment FailedFiles.
		p.logger.Warn("file indexing failed", "path", filePath, "error", err, "code", code, "attempts", curAttempts)
		if p.registry != nil {
			p.registry.Record(filePath, code, msg, curAttempts)
		}
		p.mu.Lock()
		p.status.FailedFiles++
		p.mu.Unlock()
		return
	}

	// Transient failure within retry budget: schedule retry.
	if p.retryCoord != nil {
		p.retryCoord.Schedule(filePath, code, msg, curAttempts+1)
		p.mu.Lock()
		p.status.PendingRetryFiles++
		p.mu.Unlock()
	} else {
		// No retry coordinator: treat as terminal.
		p.logger.Warn("file indexing failed (no retry coord)", "path", filePath, "error", err, "code", code)
		if p.registry != nil {
			p.registry.Record(filePath, code, msg, curAttempts)
		}
		p.mu.Lock()
		p.status.FailedFiles++
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
		return err
	}

	return p.commit(fileID, hash)
}

// checkStale decides whether filePath needs (re-)indexing. It returns the stat
// info and the freshly computed content hash so later phases can reuse them
// without a second stat/open.
func (p *Pipeline) checkStale(path string, force bool) (bool, fs.FileInfo, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, nil, "", apperr.Wrap(apperr.ErrFileUnreadable.Code, "cannot read file", err)
	}
	hash, err := hashFile(path)
	if err != nil {
		return false, nil, "", apperr.Wrap(apperr.ErrFileUnreadable.Code, "cannot hash file", err)
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
			p.logger.Error("failed to update content hash", "path", path, "error", err, "code", apperr.ErrStoreWrite.Code)
			return nil, nil, apperr.Wrap(apperr.ErrStoreWrite.Code, "failed to commit content hash", err)
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
			p.logger.Warn("quota exhausted, pipeline paused", "resumeAt", resumeAt, "code", apperr.ErrRateLimited.Code)
			return nil, nil, apperr.Wrap(apperr.ErrRateLimited.Code, "rate limited by embedding provider", err)
		}
		p.logger.Warn("batch embedding failed", "path", path, "error", err, "code", apperr.ErrEmbedFailed.Code)
		return nil, nil, apperr.Wrap(apperr.ErrEmbedFailed.Code, "embedding request failed", err)
	}
	if len(vecs) != len(batchChunks) {
		p.logger.Warn("embedding count mismatch", "path", path, "chunks", len(batchChunks), "vecs", len(vecs), "code", apperr.ErrEmbedCountMismatch.Code)
		return nil, nil, apperr.Wrap(apperr.ErrEmbedCountMismatch.Code,
			fmt.Sprintf("embedding count mismatch: got %d vecs for %d chunks", len(vecs), len(batchChunks)), nil)
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
			p.logger.Warn("adding vector failed", "fileID", fileID, "chunk", chunk.Index, "error", err, "code", apperr.ErrHnswAdd.Code)
			return apperr.Wrap(apperr.ErrHnswAdd.Code, "failed to add vector to HNSW index", err)
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
		p.logger.Error("failed to update content hash", "fileID", fileID, "error", err, "code", apperr.ErrStoreWrite.Code)
		return apperr.Wrap(apperr.ErrStoreWrite.Code, "failed to write content hash", err)
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
