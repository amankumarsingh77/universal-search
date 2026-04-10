package indexer

import (
	"log/slog"
	"os"
	"path/filepath"

	"universal-search/internal/chunker"
)

// ReconcileIndex queries all chunk vector IDs from SQLite and re-queues any file
// whose chunks are missing from the in-memory HNSW index. Designed to run as a
// background goroutine after startup.
func (p *Pipeline) ReconcileIndex() {
	log := p.logger.WithGroup("reconcile")
	log.Info("starting index reconciliation")

	chunks, err := p.store.GetAllChunks()
	if err != nil {
		log.Error("failed to load chunks for reconciliation", "error", err)
		return
	}

	if len(chunks) == 0 {
		log.Info("no chunks to reconcile")
		return
	}

	// Group by file_id, track whether any vector is missing per file.
	fileMissing := make(map[int64]bool)
	for _, c := range chunks {
		if !p.index.Has(c.VectorID) {
			fileMissing[c.FileID] = true
		} else if _, seen := fileMissing[c.FileID]; !seen {
			fileMissing[c.FileID] = false
		}
	}

	// Collect file IDs with missing vectors.
	var toReindex []int64
	for fileID, missing := range fileMissing {
		if missing {
			toReindex = append(toReindex, fileID)
		}
	}

	if len(toReindex) == 0 {
		log.Info("reconciliation complete — all vectors present")
		return
	}

	log.Info("reconciliation found files with missing vectors", "count", len(toReindex))

	p.mu.Lock()
	p.status.TotalFiles += len(toReindex)
	p.status.IsRunning = true
	p.mu.Unlock()

	for _, fileID := range toReindex {
		rec, err := p.store.GetFileByID(fileID)
		if err != nil {
			log.Warn("could not load file for reconciliation", "fileID", fileID, "error", err)
			continue
		}
		p.SubmitFile(rec.Path)
	}

	log.Info("reconciliation jobs submitted", "count", len(toReindex))
}

// StartupRescan walks all indexed folders and detects files whose mtime has changed
// since last indexing (re-queues them) and files that no longer exist on disk
// (removes them from SQLite and HNSW). Designed to run as a background goroutine
// after startup.
func (p *Pipeline) StartupRescan(folders []string) {
	log := p.logger.WithGroup("rescan")
	log.Info("starting startup rescan", "folders", len(folders))

	patterns, err := p.store.GetExcludedPatterns()
	if err != nil {
		log.Warn("could not load excluded patterns, proceeding without", "error", err)
	}

	for _, folder := range folders {
		info, err := os.Stat(folder)
		if err != nil || !info.IsDir() {
			log.Info("indexed folder not accessible, skipping", "path", folder)
			continue
		}

		err = filepath.WalkDir(folder, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip unreadable entries
			}
			if d.IsDir() {
				for _, pat := range patterns {
					if matched, _ := filepath.Match(pat, d.Name()); matched {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if chunker.Classify(path) == chunker.TypeUnknown {
				return nil
			}

			fsInfo, err := d.Info()
			if err != nil {
				return nil
			}

			existing, err := p.store.GetFileByPath(path)
			if err != nil {
				// File not in SQLite yet — submit for indexing.
				p.SubmitFile(path)
				return nil
			}

			// Re-index if mtime has changed.
			if !fsInfo.ModTime().Equal(existing.ModifiedAt) {
				log.Info("file modified since last index, queuing", "path", path)
				p.SubmitFile(path)
			}

			return nil
		})
		if err != nil {
			log.Warn("folder walk failed during rescan", "path", folder, "error", err)
		}
	}

	// Remove SQLite records for files that no longer exist on disk.
	p.cleanupDeletedFiles(log)

	log.Info("startup rescan complete")
}

func (p *Pipeline) cleanupDeletedFiles(log *slog.Logger) {
	files, err := p.store.GetAllFiles()
	if err != nil {
		log.Warn("could not load files for deleted-file cleanup", "error", err)
		return
	}

	for _, f := range files {
		if _, err := os.Stat(f.Path); os.IsNotExist(err) {
			log.Info("file deleted from disk, removing from index", "path", f.Path)
			vecIDs, err := p.store.RemoveFileByPath(f.Path)
			if err != nil {
				log.Warn("failed to remove deleted file", "path", f.Path, "error", err)
				continue
			}
			for _, vid := range vecIDs {
				p.index.Delete(vid)
			}
		}
	}
}
