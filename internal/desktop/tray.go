package desktop

import (
	"log/slog"

	"github.com/energye/systray"
)

type TrayManager struct {
	app    AppController
	icon   []byte
	logger *slog.Logger
	done   chan struct{}
}

func NewTrayManager(app AppController, icon []byte, logger *slog.Logger) *TrayManager {
	return &TrayManager{
		app:    app,
		icon:   icon,
		logger: logger,
		done:   make(chan struct{}),
	}
}

func (t *TrayManager) Start() {
	systray.Register(t.onReady, t.onExit)
}

func (t *TrayManager) Stop() {
	close(t.done)
	systray.Quit()
}

func (t *TrayManager) onReady() {
	systray.SetIcon(t.icon)
	systray.SetTitle("Universal Search")
	hotkeyStr := t.app.GetHotkeyString()
	systray.SetTooltip("Universal Search — " + hotkeyStr)

	showHide := systray.AddMenuItem("Show/Hide", "Toggle search window")
	systray.AddSeparator()
	reindex := systray.AddMenuItem("Re-index Now", "Re-index all folders")
	folders := systray.AddMenuItem("Manage Folders...", "Add or remove indexed folders")
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit", "Quit Universal Search")

	showHide.Click(func() { t.app.ToggleWindow() })
	reindex.Click(func() { t.app.ReindexNow() })
	folders.Click(func() {
		t.app.ShowWindow()
		t.app.EmitEvent("open-folder-manager")
	})
	quit.Click(func() { t.app.Quit() })
}

func (t *TrayManager) onExit() {
	t.logger.Info("system tray exited")
}
