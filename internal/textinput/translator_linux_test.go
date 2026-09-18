//go:build linux && cgo

package textinput

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

func TestHostKeymapComposeCancelAndRawModifiers(t *testing.T) {
	compose := filepath.Join(t.TempDir(), "Compose")
	if err := os.WriteFile(compose, []byte("<dead_acute> <e> : \"é\" eacute\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XCOMPOSEFILE", compose)
	translator, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer translator.Close()
	keymap := `xkb_keymap { xkb_keycodes { include "evdev+aliases(qwerty)" }; xkb_types { include "complete" }; xkb_compatibility { include "complete" }; xkb_symbols { include "pc+us(intl)+inet(evdev)" }; };`
	translator.Handle(experience.Event{Kind: experience.KeymapChanged, Keymap: keymap})
	key := func(code uint32) string {
		return translator.Handle(experience.Event{Kind: experience.KeyInput, Keycode: code, Pressed: true})
	}
	if key(40) != "" || key(18) != "é" {
		t.Fatal("host layout and compose did not commit accented text")
	}
	key(40)
	translator.Handle(experience.Event{Kind: experience.KeyboardCancel})
	if key(18) != "e" {
		t.Fatal("focus cancellation leaked pending compose text")
	}
	key(40)
	key(14)
	if key(18) != "e" {
		t.Fatal("backspace did not cancel compose")
	}
	if got := translator.Handle(experience.Event{Kind: experience.KeyInput, Keycode: 30, Pressed: true, Depressed: 1}); got != "A" {
		t.Fatal("raw shift mask was ignored", got)
	}
	if got := translator.Handle(experience.Event{Kind: experience.KeyInput, Keycode: 30, Pressed: true, Locked: 2}); got != "A" {
		t.Fatal("raw lock mask was ignored", got)
	}
	if got := translator.Handle(experience.Event{Kind: experience.KeyInput, Keycode: 30, Pressed: true, Depressed: 4}); got != "" {
		t.Fatal("raw control mask emitted text", got)
	}
	translator.Handle(experience.Event{Kind: experience.KeymapChanged, Keymap: "invalid\x00keymap"})
	if key(40) != "" || key(18) != "é" {
		t.Fatal("invalid keymap discarded the prior working layout")
	}
}
