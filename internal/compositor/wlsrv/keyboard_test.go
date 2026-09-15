package wlsrv

import (
	"strings"
	"testing"
)

func TestUSKeymapHasTOPSymbols(t *testing.T) {
	s := string(xkbKeymap)
	if !strings.HasPrefix(strings.TrimSpace(s), "xkb_keymap") {
		t.Fatal("expected compiled xkb_keymap")
	}
	for _, c := range []struct{ key, sym string }{
		{"AD05", "t"},
		{"AD09", "o"},
		{"AD10", "p"},
	} {
		i := strings.Index(s, "key <"+c.key+">")
		if i < 0 {
			t.Fatalf("no key <%s> — stub keymap cannot type %q", c.key, c.sym)
		}
		window := s[i:]
		if len(window) > 280 {
			window = window[:280]
		}
		if !strings.Contains(window, c.sym) {
			t.Fatalf("key <%s> missing symbol %q in %q", c.key, c.sym, window)
		}
	}
}

func TestKeymapBytesTrailingNUL(t *testing.T) {
	b := keymapBytes()
	if len(b) == 0 || b[len(b)-1] != 0 {
		t.Fatal("wl_keyboard.keymap xkb_v1 requires a trailing NUL")
	}
}

func TestToXKBKeycode(t *testing.T) {
	// evdev KEY_T=20 → XKB 28 (AD05)
	if got := ToXKBKeycode(20, false); got != 28 {
		t.Fatalf("evdev T → %d", got)
	}
	if got := ToXKBKeycode(28, true); got != 28 {
		t.Fatalf("nested XKB T passed through → %d", got)
	}
}
