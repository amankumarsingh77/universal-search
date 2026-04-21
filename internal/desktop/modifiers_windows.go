//go:build windows

package desktop

import (
	"strings"

	"golang.design/x/hotkey"
)

// modCmd maps to the Windows key (ModWin) on Windows.
var modCmd = hotkey.ModWin

// modAlt maps to the Alt key on Windows.
var modAlt = hotkey.ModAlt

// HumanReadableHotkey formats a modifier+key combination as a user-facing string.
func HumanReadableHotkey(mods []hotkey.Modifier, key hotkey.Key) string {
	var parts []string
	for _, m := range mods {
		switch m {
		case modCmd:
			parts = append(parts, "Win+")
		case hotkey.ModCtrl:
			parts = append(parts, "Ctrl+")
		case hotkey.ModShift:
			parts = append(parts, "Shift+")
		case modAlt:
			parts = append(parts, "Alt+")
		}
	}
	if name, ok := reverseKeyMap[key]; ok && len(name) > 0 {
		parts = append(parts, strings.ToUpper(name[:1])+name[1:])
	}
	return strings.Join(parts, "")
}
