package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"universal-search/internal/apperr"
	"universal-search/internal/chunker"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// PickAndAddFolder opens the native directory picker and, if the user chooses
// a folder, runs the same logic as AddFolder. Returning the selected path (or
// an empty string if the user cancelled) lets the frontend await the entire
// dialog lifetime, which is what the blur-to-hide suppression relies on — an
// event-based flow would race the modal picker and close it along with the
// parent window when focus leaves.
func (a *App) PickAndAddFolder() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("app context not initialized")
	}
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select folder to index",
	})
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", nil
	}
	if err := a.AddFolder(dir); err != nil {
		return "", err
	}
	runtime.EventsEmit(a.ctx, "folders-changed")
	return dir, nil
}

// AddFolder adds a folder to the indexed folders list, starts watching it,
// and triggers indexing.
func (a *App) AddFolder(path string) error {
	if a.store == nil {
		return apperr.New(apperr.ErrInternal.Code, "store not initialized")
	}
	if strings.TrimSpace(path) == "" {
		return apperr.New(apperr.ErrFolderDenied.Code, "folder path must not be empty")
	}
	if !filepath.IsAbs(path) {
		return apperr.New(apperr.ErrFolderDenied.Code, "folder path must be absolute")
	}
	a.logger.Info("adding folder", "path", path)
	if err := a.store.AddIndexedFolder(path); err != nil {
		return apperr.Wrap(apperr.ErrFolderDenied.Code, "could not add folder", err)
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
		return apperr.New(apperr.ErrInternal.Code, "store not initialized")
	}
	a.logger.Info("removing folder", "path", path, "deleteData", deleteData)

	vecIDs, err := a.store.RemoveIndexedFolder(path, deleteData)
	if err != nil {
		return apperr.Wrap(apperr.ErrStoreLocked.Code, "could not remove folder", err)
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
		return nil, apperr.New(apperr.ErrInternal.Code, "store not initialized")
	}
	folders, err := a.store.GetIndexedFolders()
	if err != nil {
		return nil, apperr.Wrap(apperr.ErrStoreLocked.Code, "could not list folders", err)
	}
	return folders, nil
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

// GetIgnoredFolders returns the list of folder name patterns excluded from indexing.
func (a *App) GetIgnoredFolders() ([]string, error) {
	if a.store == nil {
		return nil, apperr.New(apperr.ErrInternal.Code, "store not initialized")
	}
	patterns, err := a.store.GetExcludedPatterns()
	if err != nil {
		return nil, apperr.Wrap(apperr.ErrStoreLocked.Code, "could not list ignored patterns", err)
	}
	return patterns, nil
}

// AddIgnoredFolder adds a folder name pattern to the exclusion list.
func (a *App) AddIgnoredFolder(pattern string) error {
	if a.store == nil {
		return apperr.New(apperr.ErrInternal.Code, "store not initialized")
	}
	if strings.TrimSpace(pattern) == "" {
		return apperr.New(apperr.ErrConfigInvalid.Code, "pattern must not be empty")
	}
	if err := a.store.AddExcludedPattern(strings.TrimSpace(pattern)); err != nil {
		return apperr.Wrap(apperr.ErrStoreLocked.Code, "could not add ignored pattern", err)
	}
	return nil
}

// RemoveIgnoredFolder removes a folder name pattern from the exclusion list.
func (a *App) RemoveIgnoredFolder(pattern string) error {
	if a.store == nil {
		return apperr.New(apperr.ErrInternal.Code, "store not initialized")
	}
	if err := a.store.RemoveExcludedPattern(pattern); err != nil {
		return apperr.Wrap(apperr.ErrStoreLocked.Code, "could not remove ignored pattern", err)
	}
	return nil
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
