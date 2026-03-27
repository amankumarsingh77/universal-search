package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	// Prevent WebKit2GTK DMABUF renderer crashes on NVIDIA GPUs (Ubuntu 24.04+).
	// See: https://bugs.launchpad.net/ubuntu/+source/webkit2gtk/+bug/2062995
	if os.Getenv("WEBKIT_DISABLE_DMABUF_RENDERER") == "" {
		os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	}
}

func main() {
	app := NewApp()

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
	settingsMenu.AddText("Add Folder...", keys.CmdOrCtrl("o"), func(cd *menu.CallbackData) {
		dir, err := runtime.OpenDirectoryDialog(app.ctx, runtime.OpenDialogOptions{
			Title: "Select folder to index",
		})
		if err == nil && dir != "" {
			app.AddFolder(dir)
		}
	})

	err := wails.Run(&options.App{
		Title:     "Universal Search",
		Width:     800,
		Height:    550,
		MinWidth:  600,
		MinHeight: 400,
		Menu:      appMenu,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
		Frameless:        true,
		AlwaysOnTop:      true,
		BackgroundColour: &options.RGBA{R: 10, G: 10, B: 10, A: 255},
	})

	if err != nil {
		log.Fatal(err)
	}
}
