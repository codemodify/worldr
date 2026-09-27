package noteapp

import (
	"bytes"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func TestSkinRefreshesExistingNoteAndNewNoteInheritsWithoutEditing(t *testing.T) {
	m, view, _ := noteFixture(t)
	m.Focus(view.id)
	context := m.TextInput(view.id).ContextID
	m.Send(view.id, experience.Event{Kind: experience.TextPreedit, TextContext: context, Text: "compose", PreeditBegin: 0, PreeditEnd: 7})
	state := m.SessionStates()[0]
	before := append([]byte(nil), view.renderer.image.Pix...)
	selected, err := skin.Builtin("plasma")
	if err != nil {
		t.Fatal(err)
	}
	m.SetSkin(selected)
	if !view.dirty {
		t.Fatal("existing surface not invalidated")
	}
	if err := m.Poll(); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, view.renderer.image.Pix) {
		t.Fatal("existing controls retained their old pixels")
	}
	if m.SessionStates()[0] != state || m.TextInput(view.id).ContextID != context {
		t.Fatal("skin changed document or IME lease")
	}
	if _, err := m.LaunchApplication("note"); err != nil {
		t.Fatal(err)
	}
	if m.slots[1].renderer.painter.Theme.Accent != view.renderer.painter.Theme.Accent {
		t.Fatal("new note did not inherit skin")
	}
}
