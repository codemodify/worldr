package noteapp

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/experience"
)

func TestEditorMultilineUTF8IMEUndoAndRedo(t *testing.T) {
	e := newEditor("alpha\nβeta\n研究", len("alpha\nβ"), len("alpha\nβ"))
	if line, column := e.lineColumn(e.caret); line != 1 || column != 1 {
		t.Fatal("wrong UTF-8 caret coordinates", line, column)
	}
	e.Handle(experience.Event{Kind: experience.KeyInput, Keycode: 108, Pressed: true}, "")
	if line, column := e.lineColumn(e.caret); line != 2 || column != 1 {
		t.Fatal("vertical navigation lost the rune column", line, column)
	}
	changed, _ := e.Handle(experience.Event{Kind: experience.TextPreedit, Text: "資料", PreeditBegin: 0, PreeditEnd: 6}, "")
	if changed || e.text != "alpha\nβeta\n研究" || e.preedit != "資料" {
		t.Fatal("preedit changed committed text", e.text, e.preedit)
	}
	changed, _ = e.Handle(experience.Event{Kind: experience.TextCommit, Text: "資"}, "")
	if !changed || !utf8.ValidString(e.text) || e.preedit != "" || !strings.Contains(e.text, "研資究") {
		t.Fatal("IME commit was not inserted at the UTF-8 caret", e.text)
	}
	committed := e.text
	if !e.Undo() || e.text == committed || !e.Redo() || e.text != committed {
		t.Fatal("Unicode edit did not round-trip through undo/redo", e.text)
	}

	e.setCaret(len(e.text), false)
	changed, _ = e.Handle(experience.Event{Kind: experience.TextCommit, DeleteBefore: uint32(len("究")), Text: "察"}, "")
	if !changed || !strings.HasSuffix(e.text, "察") || !utf8.ValidString(e.text) {
		t.Fatal("IME byte deletion split a rune", e.text)
	}
}

func TestEditorBoundsInsertionWithoutSplittingUTF8(t *testing.T) {
	prefix := strings.Repeat("a", MaxDocumentBytes-1)
	e := newEditor(prefix, len(prefix), len(prefix))
	changed, truncated := e.Insert("研究")
	if changed || !truncated || e.text != prefix || !utf8.ValidString(e.text) {
		t.Fatal("bounded insert partially committed a multibyte rune")
	}
	e.setCaret(len(e.text)-2, false)
	e.anchor = len(e.text)
	changed, truncated = e.Insert("界")
	if !changed || truncated || len(e.text) != MaxDocumentBytes || !strings.HasSuffix(e.text, "界") || !utf8.ValidString(e.text) {
		t.Fatal("selection replacement did not account for the removed byte", len(e.text), truncated)
	}
}

func TestEditorNavigationAndDeletionPreserveGraphemeClusters(t *testing.T) {
	first, second := "e\u0301", "👩‍💻"
	e := newEditor(first+second, 0, 0)
	e.moveHorizontal(1, false)
	if e.caret != len(first) {
		t.Fatal("right arrow entered a combining cluster", e.caret)
	}
	e.setCaret(len(e.text), false)
	if !e.delete(-1) || e.text != first {
		t.Fatal("backspace split an emoji ZWJ cluster", e.text)
	}
	if !e.delete(-1) || e.text != "" {
		t.Fatal("backspace split a combining cluster", e.text)
	}
	state := SessionState{Key: "native:note", Text: first, Caret: 1, Anchor: 0}
	if err := state.Validate(); err == nil || !strings.Contains(err.Error(), "grapheme") {
		t.Fatal("session accepted a selection inside a grapheme", err)
	}
}

func TestSessionStateRejectsSplitSelectionAndBroadMarkdown(t *testing.T) {
	state := SessionState{Key: "native:note", Text: "λ", Caret: 1, Anchor: 0}
	if err := state.Validate(); err == nil {
		t.Fatal("accepted a caret inside a UTF-8 sequence")
	}
	if IsNotePath("README.md") || !IsNotePath("design.worldr-note.md") {
		t.Fatal("native note extension claimed broad Markdown or missed its explicit format")
	}
}
