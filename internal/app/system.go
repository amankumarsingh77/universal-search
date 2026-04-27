package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"

	"findo/internal/apperr"
	"findo/internal/indexer"
	"findo/internal/platform"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

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
		return "", apperr.Wrap(apperr.ErrMediaProcessing.Code, "ffmpeg preview clip failed", err)
	}
	return clipPath, nil
}

// ShowWindow makes the search window visible and centers it on screen.
func (a *App) ShowWindow() {
	a.windowMu.Lock()
	defer a.windowMu.Unlock()
	a.logger.Info("showing window")
	runtime.WindowShow(a.ctx)
	runtime.WindowCenter(a.ctx)
	a.windowVisible = true
	runtime.EventsEmit(a.ctx, "window-shown")
}

// HideWindow hides the search window without quitting the app.
func (a *App) HideWindow() {
	a.windowMu.Lock()
	defer a.windowMu.Unlock()
	a.logger.Info("hiding window")
	runtime.WindowHide(a.ctx)
	a.windowVisible = false
}

// ToggleWindow shows the window if hidden, hides it if visible.
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

// EmitEvent fires a Wails runtime event with the given name.
func (a *App) EmitEvent(name string) {
	runtime.EventsEmit(a.ctx, name)
}

// Quit exits the application.
func (a *App) Quit() {
	runtime.Quit(a.ctx)
}
