package projectapp

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/experience"
)

func TestFilesCompositionIsTransientAndCommitsOnlyToCurrentField(t *testing.T) {
	p, r := controlledProvider(t)
	listing := nextCall(t, r)
	listing.answer <- result{entries: []entry{{name: "研究.png"}, {name: "BETA.png"}}}
	pollUntil(t, p, func() bool { return p.selected == 0 })
	p.Focus(1)
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 33, Pressed: true, Modifiers: experience.ModControl})
	context := p.TextInput(1).ContextID
	p.Send(1, experience.Event{Kind: experience.TextPreedit, TextContext: context, Text: "研究", PreeditBegin: 0, PreeditEnd: 6})
	if text, _, _ := p.searchField.Preedit(); text != "研究" || p.query != "" || len(r.calls) != 0 {
		t.Fatal("preedit altered the search or activated a file")
	}
	p.Send(1, experience.Event{Kind: experience.TextCommit, TextContext: context, Text: "研究"})
	if p.query != "研究" || len(p.entries) != 1 || p.selectionName() != "研究.png" {
		t.Fatal("IME did not commit to filename search")
	}
	p.Send(1, experience.Event{Kind: experience.TextCommit, TextContext: context, DeleteBefore: 6, Text: "BETA"})
	if p.query != "BETA" || p.selectionName() != "BETA.png" {
		t.Fatal("IME replacement did not delete UTF-8 surrounding bytes")
	}
	p.Send(1, experience.Event{Kind: experience.TextPreedit, TextContext: context, Text: "pending", PreeditBegin: 0, PreeditEnd: 7})
	p.Focus(0)
	if p.TextInput(1).Enabled {
		t.Fatal("unfocused Files exposed an IME destination")
	}
	if text, _, _ := p.searchField.Preedit(); text != "" {
		t.Fatal("focus loss retained composition")
	}
	p.Focus(1)
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 33, Pressed: true, Modifiers: experience.ModControl})
	if p.TextInput(1).ContextID == context {
		t.Fatal("focus return reused stale IME context")
	}
	p.Send(1, experience.Event{Kind: experience.TextCommit, TextContext: context, Text: "stale"})
	if p.query != "BETA" {
		t.Fatal("stale IME commit entered reopened search")
	}
	p.beginOperation(moveItem)
	p.dialog.field.Set(strings.Repeat("λ", 2048))
	state := p.TextInput(1)
	if !state.Enabled || len(state.Surrounding) > 4000 || !utf8.ValidString(state.Surrounding) || state.Cursor < 0 || state.Cursor > len(state.Surrounding) {
		t.Fatal("Files exposed invalid/unbounded surrounding text", state)
	}
	p.Send(1, experience.Event{Kind: experience.TextCommit, TextContext: state.ContextID, DeleteBefore: 2, Text: "文"})
	// The byte limit may omit the replacement, but must never split the original.
	if !utf8.ValidString(p.dialog.field.Text()) {
		t.Fatal("IME filename replacement split a UTF-8 character")
	}
	if p.query != "BETA" {
		t.Fatal("dialog IME altered the hidden search")
	}
}
