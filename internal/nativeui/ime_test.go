package nativeui

import (
	"bytes"
	"image"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/experience"
)

func TestIMEPreeditIsTransientAndCommitDeletionIsAtomic(t *testing.T) {
	f := NewField(100)
	f.Set("ab界cd")
	f.SetCaret(len("ab界"), false)
	if f.Handle(experience.Event{Kind: experience.TextPreedit, Text: "かな", PreeditBegin: 3, PreeditEnd: 6}, "") {
		t.Fatal("preedit changed committed document text")
	}
	if f.Text() != "ab界cd" {
		t.Fatal("preedit leaked into the document")
	}
	state := f.TextInput("test", image.Rect(1, 2, 101, 30))
	if state.Surrounding != "ab界cd" || state.Cursor != len("ab界") {
		t.Fatal("preedit leaked into surrounding text")
	}
	if f.Handle(experience.Event{Kind: experience.TextCommit, Text: "wrong", DeleteBefore: 1}, "") || f.Text() != "ab界cd" {
		t.Fatal("middle-byte deletion partially changed text")
	}
	if !f.Handle(experience.Event{Kind: experience.TextCommit, Text: "漢", DeleteBefore: 3, DeleteAfter: 1}, "") || f.Text() != "ab漢d" {
		t.Fatalf("commit/delete transaction = %q", f.Text())
	}
	if text, _, _ := f.Preedit(); text != "" {
		t.Fatal("commit retained old preedit")
	}
	f.Handle(experience.Event{Kind: experience.TextPreedit, Text: "字", PreeditBegin: -1, PreeditEnd: -1}, "")
	f.Handle(experience.Event{Kind: experience.KeyboardCancel}, "")
	if text, _, _ := f.Preedit(); text != "" || f.Text() != "ab漢d" {
		t.Fatal("focus cancellation lost committed text or retained composition")
	}
	f.SelectAll()
	f.Handle(experience.Event{Kind: experience.TextPreedit, Text: "new", PreeditBegin: 3, PreeditEnd: 3}, "")
	if f.Text() != "" {
		t.Fatal("new preedit failed to replace existing selection")
	}
}

func TestIMEPreeditPaintsAndSurroundingWindowKeepsUTF8Boundaries(t *testing.T) {
	f := NewField(12000)
	f.Set(strings.Repeat("界", 3000))
	f.SetCaret(4500, false)
	state := f.TextInput("large", image.Rect(0, 0, 200, 30))
	if !state.Enabled || len(state.Surrounding) > 4000 || !utf8.ValidString(state.Surrounding) || state.Cursor%3 != 0 || state.Anchor != state.Cursor {
		t.Fatalf("invalid bounded surrounding window: %+v", state)
	}
	f.SelectAll()
	if f.TextInput("large", image.Rect(0, 0, 200, 30)).Enabled {
		t.Fatal("oversized selection was misrepresented as an empty document")
	}
	f.Set("base")
	p, err := NewPainter(Cinematic())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	before, after := image.NewRGBA(image.Rect(0, 0, 240, 40)), image.NewRGBA(image.Rect(0, 0, 240, 40))
	if err := p.DrawField(before, before.Bounds(), f, "", true); err != nil {
		t.Fatal(err)
	}
	f.Handle(experience.Event{Kind: experience.TextPreedit, Text: "かな", PreeditBegin: 3, PreeditEnd: 6}, "")
	if err := p.DrawField(after, after.Bounds(), f, "", true); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before.Pix, after.Pix) || f.Text() != "base" {
		t.Fatal("preedit was not separately painted")
	}
}
