package desktop

import (
	"testing"

	"golang.design/x/hotkey"
)

func TestParseHotkey_CmdSpace(t *testing.T) {
	mods, key, err := ParseHotkey("cmd+space")
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 1 || mods[0] != hotkey.ModCmd {
		t.Fatalf("expected [ModCmd], got %v", mods)
	}
	if key != hotkey.KeySpace {
		t.Fatalf("expected KeySpace, got %v", key)
	}
}

func TestParseHotkey_CtrlShiftA(t *testing.T) {
	mods, key, err := ParseHotkey("ctrl+shift+a")
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 2 || mods[0] != hotkey.ModCtrl || mods[1] != hotkey.ModShift {
		t.Fatalf("expected [ModCtrl, ModShift], got %v", mods)
	}
	if key != hotkey.KeyA {
		t.Fatalf("expected KeyA, got %v", key)
	}
}

func TestParseHotkey_Invalid(t *testing.T) {
	_, _, err := ParseHotkey("invalid")
	if err == nil {
		t.Fatal("expected error for single-part hotkey")
	}
}

func TestParseHotkey_UnknownModifier(t *testing.T) {
	_, _, err := ParseHotkey("win+space")
	if err == nil {
		t.Fatal("expected error for unknown modifier")
	}
}

func TestParseHotkey_UnknownKey(t *testing.T) {
	_, _, err := ParseHotkey("ctrl+$")
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestFormatHotkey_Roundtrip(t *testing.T) {
	combos := []string{
		"cmd+space",
		"ctrl+shift+a",
		"alt+f1",
		"ctrl+z",
		"shift+return",
	}
	for _, combo := range combos {
		mods, key, err := ParseHotkey(combo)
		if err != nil {
			t.Fatalf("ParseHotkey(%q): %v", combo, err)
		}
		got := FormatHotkey(mods, key)
		if got != combo {
			t.Errorf("roundtrip failed: %q -> %q", combo, got)
		}
	}
}

func TestDefaultHotkey(t *testing.T) {
	d := DefaultHotkey()
	if d == "" {
		t.Fatal("DefaultHotkey returned empty string")
	}
	_, _, err := ParseHotkey(d)
	if err != nil {
		t.Fatalf("DefaultHotkey %q is not parseable: %v", d, err)
	}
}

func TestParseHotkey_OptionModifier(t *testing.T) {
	mods, key, err := ParseHotkey("option+space")
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 1 || mods[0] != hotkey.ModOption {
		t.Fatalf("expected [ModOption], got %v", mods)
	}
	if key != hotkey.KeySpace {
		t.Fatalf("expected KeySpace, got %v", key)
	}
}

func TestParseHotkey_FunctionKeys(t *testing.T) {
	tests := []struct {
		input string
		want  hotkey.Key
	}{
		{"ctrl+f1", hotkey.KeyF1},
		{"ctrl+f5", hotkey.KeyF5},
		{"ctrl+f12", hotkey.KeyF12},
		{"ctrl+f20", hotkey.KeyF20},
	}
	for _, tt := range tests {
		_, key, err := ParseHotkey(tt.input)
		if err != nil {
			t.Fatalf("ParseHotkey(%q): %v", tt.input, err)
		}
		if key != tt.want {
			t.Errorf("ParseHotkey(%q) key = %v, want %v", tt.input, key, tt.want)
		}
	}
}

func TestParseHotkey_CaseInsensitive(t *testing.T) {
	mods, key, err := ParseHotkey("CMD+SPACE")
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 1 || mods[0] != hotkey.ModCmd {
		t.Fatalf("expected [ModCmd], got %v", mods)
	}
	if key != hotkey.KeySpace {
		t.Fatalf("expected KeySpace, got %v", key)
	}
}

func TestParseHotkey_SpecialKeys(t *testing.T) {
	tests := []struct {
		input string
		want  hotkey.Key
	}{
		{"ctrl+enter", hotkey.KeyReturn},
		{"ctrl+esc", hotkey.KeyEscape},
		{"ctrl+backspace", hotkey.KeyDelete},
		{"ctrl+tab", hotkey.KeyTab},
	}
	for _, tt := range tests {
		_, key, err := ParseHotkey(tt.input)
		if err != nil {
			t.Fatalf("ParseHotkey(%q): %v", tt.input, err)
		}
		if key != tt.want {
			t.Errorf("ParseHotkey(%q) key = %v, want %v", tt.input, key, tt.want)
		}
	}
}
