package projectapp

import (
	"github.com/codemodify/worldr/internal/experience"
	"testing"
)

func TestFieldClipboardSelectionPasteAndOriginalDestination(t *testing.T) {
	p, r := controlledProvider(t)
	listing := nextCall(t, r)
	listing.answer <- result{entries: []entry{{name: "notes.png"}}}
	pollUntil(t, p, func() bool { return p.selected == 0 })
	p.Focus(1)
	key := func(code uint32) {
		p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: code, Pressed: true, Modifiers: experience.ModControl})
	}
	key(33)
	first := p.TextInput(1)
	if !first.Enabled || first.ContextID == "" {
		t.Fatal("search did not publish text-input context")
	}
	key(47)
	if !p.TakePasteRequest() || p.TakePasteRequest() {
		t.Fatal("clipboard request was lost or repeated")
	}
	if err := p.Paste("notes"); err != nil {
		t.Fatal(err)
	}
	if p.query != "notes" {
		t.Fatal("paste missed original search field")
	}
	key(30)
	key(46)
	if text, ok := p.TakeCopy(); !ok || text != "notes" {
		t.Fatal("copy did not use field selection", text, ok)
	}
	key(45)
	if p.query != "" {
		t.Fatal("cut did not remove selected field text")
	}
	key(47)
	if !p.TakePasteRequest() {
		t.Fatal("second paste was not requested")
	}
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 1, Pressed: true})
	key(33)
	if p.TextInput(1).ContextID == first.ContextID {
		t.Fatal("reopened field reused an expired text context")
	}
	if err := p.Paste("stale"); err != nil || p.query != "" {
		t.Fatal("delayed paste entered a reopened field", err)
	}
	key(47)
	if !p.TakePasteRequest() {
		t.Fatal("third paste was not requested")
	}
	p.Focus(0)
	p.Focus(1)
	key(33)
	if err := p.Paste("late focus"); err != nil || p.query != "" {
		t.Fatal("focus return revived paste lease", err)
	}
	key(47)
	if !p.TakePasteRequest() {
		t.Fatal("fourth paste was not requested")
	}
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 45, Pressed: true})
	if err := p.Paste("late edit"); err != nil || p.query != "x" {
		t.Fatal("intervening editing did not invalidate paste", err)
	}
	p.beginOperation(createFolder)
	key(47)
	if !p.TakePasteRequest() {
		t.Fatal("dialog paste was not requested")
	}
	if err := p.Paste("研究 folder"); err != nil || p.dialog.field.Text() != "研究 folder" {
		t.Fatal("paste missed filename field", err)
	}
	if p.query != "x" {
		t.Fatal("dialog paste altered the hidden search")
	}
	if err := p.Paste("unsolicited"); err != nil || p.dialog.field.Text() != "研究 folder" {
		t.Fatal("unsolicited clipboard completion altered field", err)
	}
}
