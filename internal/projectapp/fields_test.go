package projectapp

import (
	"image"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

func TestFilesFieldsEditGraphemesAndSelectionWithoutLaunching(t *testing.T) {
	p, r := controlledProvider(t)
	listing := nextCall(t, r)
	listing.answer <- result{entries: []entry{{name: "café.png"}, {name: "研究.png"}}}
	pollUntil(t, p, func() bool { return p.selected == 0 })
	p.Focus(1)
	p.setQuery("e\u0301")
	p.searchActive = true
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 14, Pressed: true})
	if p.query != "" || p.searchField.Text() != "" {
		t.Fatal("search left a detached combining mark")
	}
	p.beginOperation(createFolder)
	p.dialog.field.Set("研究 e\u0301")
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 14, Pressed: true})
	if p.dialog.field.Text() != "研究 " {
		t.Fatal("filename editor split a grapheme")
	}
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 30, Pressed: true, Modifiers: experience.ModControl})
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 45, Pressed: true})
	if p.dialog.field.Text() != "x" {
		t.Fatal("typed name did not replace selection")
	}
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 1, Pressed: true})
	if p.dialog != nil || p.operationPending || len(r.calls) != 0 {
		t.Fatal("canceling field editing launched an operation")
	}
}

func TestShapedFilesLabelStaysInsideItsPaintClip(t *testing.T) {
	r, err := newBrowserRenderer(minWidth, minHeight)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	r.fill(r.image.Rect, 0x102030)
	before := append([]byte(nil), r.image.Pix...)
	clip := image.Rect(20, 30, 105, 51)
	r.label(clip, 20, 47, "研究 العربية e\u0301 a very long filename.png", 0xffffff, false)
	if r.paintErr != nil {
		t.Fatal(r.paintErr)
	}
	changed := false
	for y := 0; y < r.image.Rect.Dy(); y++ {
		for x := 0; x < r.image.Rect.Dx(); x++ {
			index := y*r.image.Stride + x*4
			differs := false
			for c := 0; c < 4; c++ {
				differs = differs || before[index+c] != r.image.Pix[index+c]
			}
			if differs && !image.Pt(x, y).In(clip) {
				t.Fatalf("shaped filename painted outside clip at %d,%d", x, y)
			}
			changed = changed || differs
		}
	}
	if !changed {
		t.Fatal("shaped filename painted no visible pixels")
	}
}
