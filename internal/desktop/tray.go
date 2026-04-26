package desktop

import (
	"log/slog"

	"github.com/energye/systray"
)

// TrayManager owns the system tray icon and its menu lifecycle.
type TrayManager struct {
	app      AppController
	icon     []byte
	iconTmpl []byte
	logger   *slog.Logger
	done     chan struct{}
}

// NewTrayManager constructs a TrayManager. icon is the colored fallback used
// on Windows and Linux. iconTmpl is the black + alpha template image used on
// macOS so the OS can tint it for light/dark menu bars; pass nil to fall back
// to the regular icon on all platforms.
func NewTrayManager(app AppController, icon, iconTmpl []byte, logger *slog.Logger) *TrayManager {
	return &TrayManager{
		app:      app,
		icon:     icon,
		iconTmpl: iconTmpl,
		logger:   logger,
		done:     make(chan struct{}),
	}
}

// Start registers the tray icon and wires up its menu actions.
func (t *TrayManager) Start() {
	systray.Register(t.onReady, t.onExit)
}

// Stop removes the tray icon and releases its resources.
func (t *TrayManager) Stop() {
	close(t.done)
	systray.Quit()
}

func (t *TrayManager) onReady() {
	if len(t.iconTmpl) > 0 {
		// On macOS, marks the image as a template so the OS tints it dark in
		// light mode and white in dark mode. On Windows/Linux, falls back to
		// the regular colored icon.
		systray.SetTemplateIcon(t.iconTmpl, t.icon)
	} else {
		systray.SetIcon(t.icon)
	}
	systray.SetTitle("")
	hotkeyStr := t.app.GetHotkeyString()
	systray.SetTooltip("Findo — " + hotkeyStr)

	systray.SetOnClick(func(systray.IMenu) { t.app.ToggleWindow() })

	showHide := systray.AddMenuItem("Show/Hide", "Toggle search window")
	reindex := systray.AddMenuItem("Re-index Now", "Re-index all folders")
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit", "Quit Findo")

	showHide.Click(func() { t.app.ToggleWindow() })
	reindex.Click(func() { t.app.ReindexNow() })
	quit.Click(func() { t.app.Quit() })
}

func (t *TrayManager) onExit() {
	t.logger.Info("system tray exited")
}
