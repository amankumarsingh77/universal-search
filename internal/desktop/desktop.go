package desktop

import (
	"fmt"
	"log/slog"
	goruntime "runtime"
	"strings"

	"golang.design/x/hotkey"
)

// AppController is the interface the hotkey manager uses to interact with
// the main application window.
type AppController interface {
	ToggleWindow()
	ShowWindow()
	HideWindow()
	ReindexNow()
	EmitEvent(name string)
	Quit()
}

// SettingsStore is what the hotkey manager needs for persistence.
type SettingsStore interface {
	GetSetting(key, defaultVal string) (string, error)
	SetSetting(key, value string) error
}

type HotkeyManager struct {
	app    AppController
	store  SettingsStore
	hk     *hotkey.Hotkey
	stopCh chan struct{}
	logger *slog.Logger
}

func NewHotkeyManager(app AppController, store SettingsStore, logger *slog.Logger) *HotkeyManager {
	return &HotkeyManager{
		app:    app,
		store:  store,
		logger: logger,
	}
}

func (h *HotkeyManager) Start() error {
	combo, err := h.store.GetSetting("global_hotkey", DefaultHotkey())
	if err != nil {
		return fmt.Errorf("read hotkey setting: %w", err)
	}

	mods, key, err := ParseHotkey(combo)
	if err != nil {
		h.logger.Warn("invalid hotkey setting, using default", "combo", combo, "error", err)
		mods, key, _ = ParseHotkey(DefaultHotkey())
	}

	h.hk = hotkey.New(mods, key)
	if err := h.hk.Register(); err != nil {
		h.logger.Warn("failed to register hotkey, another app may have it", "combo", combo, "error", err)
		return err
	}

	h.logger.Info("global hotkey registered", "combo", combo)
	h.stopCh = make(chan struct{})
	go h.listen()
	return nil
}

func (h *HotkeyManager) listen() {
	for {
		select {
		case <-h.hk.Keydown():
			h.app.ToggleWindow()
		case <-h.stopCh:
			return
		}
	}
}

func (h *HotkeyManager) Stop() {
	if h.stopCh != nil {
		close(h.stopCh)
	}
	if h.hk != nil {
		h.hk.Unregister()
	}
}

func (h *HotkeyManager) ChangeHotkey(combo string) error {
	mods, key, err := ParseHotkey(combo)
	if err != nil {
		return err
	}

	oldHk := h.hk
	if h.stopCh != nil {
		close(h.stopCh)
	}
	if oldHk != nil {
		oldHk.Unregister()
	}

	newHk := hotkey.New(mods, key)
	if err := newHk.Register(); err != nil {
		if oldHk != nil {
			oldHk.Register()
			h.stopCh = make(chan struct{})
			go h.listen()
		}
		return fmt.Errorf("register new hotkey: %w", err)
	}

	h.hk = newHk
	h.stopCh = make(chan struct{})
	go h.listen()

	if err := h.store.SetSetting("global_hotkey", combo); err != nil {
		h.logger.Warn("failed to persist hotkey setting", "error", err)
	}
	h.logger.Info("hotkey changed", "combo", combo)
	return nil
}

// DefaultHotkey returns the platform-appropriate default hotkey string.
func DefaultHotkey() string {
	if goruntime.GOOS == "darwin" {
		return "cmd+space"
	}
	return "ctrl+space"
}

// ParseHotkey converts a string like "cmd+space" or "ctrl+shift+a" to a
// modifier slice and key.
func ParseHotkey(combo string) ([]hotkey.Modifier, hotkey.Key, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(combo)), "+")
	if len(parts) < 2 {
		return nil, 0, fmt.Errorf("invalid hotkey: %s", combo)
	}

	var mods []hotkey.Modifier
	for _, part := range parts[:len(parts)-1] {
		switch part {
		case "cmd":
			mods = append(mods, hotkey.ModCmd)
		case "ctrl":
			mods = append(mods, hotkey.ModCtrl)
		case "shift":
			mods = append(mods, hotkey.ModShift)
		case "alt", "option":
			mods = append(mods, hotkey.ModOption)
		default:
			return nil, 0, fmt.Errorf("unknown modifier: %s", part)
		}
	}

	keyStr := parts[len(parts)-1]
	key, err := parseKey(keyStr)
	if err != nil {
		return nil, 0, err
	}
	return mods, key, nil
}

var keyMap = map[string]hotkey.Key{
	"space": hotkey.KeySpace, "return": hotkey.KeyReturn, "enter": hotkey.KeyReturn,
	"escape": hotkey.KeyEscape, "esc": hotkey.KeyEscape,
	"tab": hotkey.KeyTab, "delete": hotkey.KeyDelete, "backspace": hotkey.KeyDelete,
	"a": hotkey.KeyA, "b": hotkey.KeyB, "c": hotkey.KeyC, "d": hotkey.KeyD,
	"e": hotkey.KeyE, "f": hotkey.KeyF, "g": hotkey.KeyG, "h": hotkey.KeyH,
	"i": hotkey.KeyI, "j": hotkey.KeyJ, "k": hotkey.KeyK, "l": hotkey.KeyL,
	"m": hotkey.KeyM, "n": hotkey.KeyN, "o": hotkey.KeyO, "p": hotkey.KeyP,
	"q": hotkey.KeyQ, "r": hotkey.KeyR, "s": hotkey.KeyS, "t": hotkey.KeyT,
	"u": hotkey.KeyU, "v": hotkey.KeyV, "w": hotkey.KeyW, "x": hotkey.KeyX,
	"y": hotkey.KeyY, "z": hotkey.KeyZ,
	"f1": hotkey.KeyF1, "f2": hotkey.KeyF2, "f3": hotkey.KeyF3, "f4": hotkey.KeyF4,
	"f5": hotkey.KeyF5, "f6": hotkey.KeyF6, "f7": hotkey.KeyF7, "f8": hotkey.KeyF8,
	"f9": hotkey.KeyF9, "f10": hotkey.KeyF10, "f11": hotkey.KeyF11, "f12": hotkey.KeyF12,
	"f13": hotkey.KeyF13, "f14": hotkey.KeyF14, "f15": hotkey.KeyF15, "f16": hotkey.KeyF16,
	"f17": hotkey.KeyF17, "f18": hotkey.KeyF18, "f19": hotkey.KeyF19, "f20": hotkey.KeyF20,
}

func parseKey(s string) (hotkey.Key, error) {
	if k, ok := keyMap[s]; ok {
		return k, nil
	}
	return 0, fmt.Errorf("unknown key: %s", s)
}

// FormatHotkey converts modifiers and key back to a string.
func FormatHotkey(mods []hotkey.Modifier, key hotkey.Key) string {
	var parts []string
	for _, m := range mods {
		switch m {
		case hotkey.ModCmd:
			parts = append(parts, "cmd")
		case hotkey.ModCtrl:
			parts = append(parts, "ctrl")
		case hotkey.ModShift:
			parts = append(parts, "shift")
		case hotkey.ModOption:
			parts = append(parts, "alt")
		}
	}
	parts = append(parts, formatKey(key))
	return strings.Join(parts, "+")
}

var reverseKeyMap map[hotkey.Key]string

func init() {
	reverseKeyMap = make(map[hotkey.Key]string, len(keyMap))
	for name, key := range keyMap {
		// Prefer canonical names over aliases (skip "enter", "esc", "backspace").
		if name == "enter" || name == "esc" || name == "backspace" {
			continue
		}
		reverseKeyMap[key] = name
	}
}

func formatKey(k hotkey.Key) string {
	if name, ok := reverseKeyMap[k]; ok {
		return name
	}
	return fmt.Sprintf("key(%d)", k)
}
