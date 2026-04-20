package main

import (
	"embed"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"universal-search/internal/platform"

	"github.com/joho/godotenv"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// localFileHandler serves local filesystem files for preview/thumbnails.
// Requests with paths starting with "/localfile/" are served from the
// actual filesystem path after stripping the prefix. Access is restricted
// to the thumbnail directory and indexed folders.
type localFileHandler struct {
	app *App
}

func (h *localFileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// URL path: /localfile/<absolute-path>
	// e.g. /localfile/Users/amankumar/file.png -> /Users/amankumar/file.png
	filePath := strings.TrimPrefix(r.URL.Path, "/localfile")
	if filePath == "" || filePath == "/" {
		http.Error(w, "no file path", http.StatusBadRequest)
		return
	}

	// Security: ensure the path is absolute and cleaned
	filePath = filepath.Clean(filePath)
	if !filepath.IsAbs(filePath) {
		http.Error(w, "path must be absolute", http.StatusBadRequest)
		return
	}

	if !h.isAllowedPath(filePath) {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	// Set content type from extension
	ext := filepath.Ext(filePath)
	if ct := mime.TypeByExtension(ext); ct != "" {
		w.Header().Set("Content-Type", ct)
	}

	http.ServeFile(w, r, filePath)
}

// isAllowedPath checks if the requested path falls within the thumbnail
// directory or one of the user's indexed folders.
func (h *localFileHandler) isAllowedPath(filePath string) bool {
	thumbDir, err := platform.ThumbnailDir()
	if err == nil && strings.HasPrefix(filePath, thumbDir) {
		return true
	}

	if h.app.store != nil {
		folders, err := h.app.store.GetIndexedFolders()
		if err == nil {
			for _, folder := range folders {
				if strings.HasPrefix(filePath, folder) {
					return true
				}
			}
		}
	}

	return false
}

//go:embed all:frontend/dist
var assets embed.FS

//go:embed assets/tray-icon.png
var trayIcon []byte

func init() {
	// Load .env file if present (errors are ignored — file is optional).
	godotenv.Overload()

	// Prevent WebKit2GTK DMABUF renderer crashes on NVIDIA GPUs (Ubuntu 24.04+).
	// See: https://bugs.launchpad.net/ubuntu/+source/webkit2gtk/+bug/2062995
	if os.Getenv("WEBKIT_DISABLE_DMABUF_RENDERER") == "" {
		os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	}
}

func main() {
	app := NewApp()
	app.trayIcon = trayIcon

	// Build application menu.
	appMenu := menu.NewMenu()

	// "Universal Search" app menu
	appSubMenu := appMenu.AddSubmenu("Universal Search")
	appSubMenu.AddText("About Universal Search", nil, func(_ *menu.CallbackData) {
		runtime.MessageDialog(app.ctx, runtime.MessageDialogOptions{
			Type:    runtime.InfoDialog,
			Title:   "Universal Search",
			Message: "Universal Search — fast local file search powered by vector embeddings.",
		})
	})
	appSubMenu.AddSeparator()
	appSubMenu.AddText("Quit", keys.CmdOrCtrl("q"), func(_ *menu.CallbackData) {
		runtime.Quit(app.ctx)
	})

	// "Indexing" menu
	indexingMenu := appMenu.AddSubmenu("Indexing")
	indexingMenu.AddText("Re-index Now", keys.CmdOrCtrl("r"), func(_ *menu.CallbackData) {
		app.ReindexNow()
	})
	indexingMenu.AddText("Pause Indexing", nil, func(_ *menu.CallbackData) {
		app.PauseIndexing()
	})
	indexingMenu.AddText("Resume Indexing", nil, func(_ *menu.CallbackData) {
		app.ResumeIndexing()
	})

	// "Settings" menu
	settingsMenu := appMenu.AddSubmenu("Settings")
	settingsMenu.AddText("Manage Folders...", keys.CmdOrCtrl("o"), func(cd *menu.CallbackData) {
		runtime.EventsEmit(app.ctx, "open-folder-manager")
	})
	settingsMenu.AddText("Set API Key…", nil, func(_ *menu.CallbackData) {
		runtime.WindowShow(app.ctx)
		runtime.EventsEmit(app.ctx, "open-api-key-dialog")
	})

	// Add the native macOS Edit menu so that Cmd+V/C/A/Z work in all input fields.
	appMenu.Append(menu.EditMenu())

	err := wails.Run(&options.App{
		Title:     "Universal Search",
		Width:     960,
		Height:    600,
		MinWidth:  680,
		MinHeight: 80,
		Menu:      appMenu,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: &localFileHandler{app: app},
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
		Frameless:         true,
		AlwaysOnTop:       true,
		HideWindowOnClose: true,
		BackgroundColour:  &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		Mac: &mac.Options{
			TitleBar: &mac.TitleBar{
				TitlebarAppearsTransparent: true,
				HideTitle:                  true,
				HideToolbarSeparator:       true,
				FullSizeContent:            true,
				UseToolbar:                 false,
			},
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}
