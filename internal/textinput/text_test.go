package textinput

import (
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

func TestTranslatorCommitsPrintableKeysAndPreservesShortcutOwnership(t *testing.T) {
	translator, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer translator.Close()
	for _, test := range []struct {
		event experience.Event
		want  string
	}{
		{experience.Event{Kind: experience.KeyInput, Keycode: 30, Pressed: true}, "a"},
		{experience.Event{Kind: experience.KeyInput, Keycode: 30, Pressed: true, Repeat: true}, "a"},
		{experience.Event{Kind: experience.KeyInput, Keycode: 30, Pressed: true, Modifiers: experience.ModShift}, "A"},
		{experience.Event{Kind: experience.KeyInput, Keycode: 30}, ""},
		{experience.Event{Kind: experience.KeyInput, Keycode: 30, Pressed: true, Modifiers: experience.ModControl}, ""},
		{experience.Event{Kind: experience.KeyInput, Keycode: 30, Pressed: true, Modifiers: experience.ModAlt}, ""},
		{experience.Event{Kind: experience.KeyInput, Keycode: 30, Pressed: true, Modifiers: experience.ModSuper}, ""},
		{experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: true}, ""},
		{experience.Event{Kind: experience.KeyInput, Keycode: 14, Pressed: true}, ""},
		{experience.Event{Kind: experience.KeyInput, Keycode: 15, Pressed: true}, ""},
		{experience.Event{Kind: experience.KeyInput, Keycode: 99999, Pressed: true}, ""},
	} {
		if got := translator.Handle(test.event); got != test.want {
			t.Fatalf("key %d/mods %d: got %q want %q", test.event.Keycode, test.event.Modifiers, got, test.want)
		}
	}
	translator.Close()
	translator.Close()
	if got := translator.Handle(experience.Event{Kind: experience.KeyInput, Keycode: 30, Pressed: true}); got != "" {
		t.Fatal("closed translator emitted text")
	}
}

func TestPrintableRejectsControlsAndInvalidText(t *testing.T) {
	if got := printable("hello\n\t\x00\u202e world λ"); got != "hello world λ" {
		t.Fatal(got)
	}
	if printable("invalid\xff") != "" {
		t.Fatal("invalid UTF-8 committed")
	}
}
