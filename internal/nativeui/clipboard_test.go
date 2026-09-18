package nativeui

import (
	"github.com/codemodify/worldr/internal/experience"
	"testing"
)

func TestFieldClipboardPreservesOriginalFocusAndSelectionLease(t *testing.T) {
	var c FieldClipboard
	first, second := NewField(128), NewField(128)
	c.Bind(first, "first:1")
	paste := experience.Event{Kind: experience.KeyInput, Keycode: 47, Pressed: true, Modifiers: experience.ModControl}
	if !c.Handle(paste) || !c.TakePasteRequest() {
		t.Fatal("paste not requested")
	}
	c.Bind(second, "second:1")
	c.Handle(paste)
	if c.TakePasteRequest() {
		t.Fatal("pending transfer leased a new field")
	}
	c.Paste("late")
	if first.Text() != "" || second.Text() != "" {
		t.Fatal("late clipboard changed a new target")
	}
	c.Handle(paste)
	if !c.TakePasteRequest() {
		t.Fatal("lease did not release")
	}
	c.Paste("貼り付け")
	if second.Text() != "貼り付け" {
		t.Fatal("Unicode paste missing")
	}
	c.Handle(paste)
	c.TakePasteRequest()
	c.Handle(experience.Event{Kind: experience.KeyInput, Keycode: 105, Pressed: true})
	c.Paste("stale selection")
	if second.Text() != "貼り付け" {
		t.Fatal("intervening caret movement kept a stale lease")
	}
}
