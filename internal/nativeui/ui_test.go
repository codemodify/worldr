package nativeui

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/experience"
)

func TestControllerCaptureTraversalAndImmutableSemantics(t *testing.T) {
	nodes := []Node{{ID: "first", Role: RoleButton, Label: "First", Bounds: image.Rect(0, 0, 40, 20)}, {ID: "disabled", Role: RoleButton, Disabled: true, Bounds: image.Rect(40, 0, 80, 20)}, {ID: "query", Role: RoleTextField, Label: "Query", Bounds: image.Rect(80, 0, 180, 20)}, {ID: "last", Role: RoleMenuItem, Bounds: image.Rect(0, 20, 180, 40)}}
	var c Controller
	c.SetNodes(nodes)
	press := experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 10, Y: 10}
	if action := c.Handle(press); !action.Consumed || action.Activated || !action.ChangedFocus {
		t.Fatalf("press = %+v", action)
	}
	release := press
	release.Kind = experience.PointerUp
	release.X = 190
	if action := c.Handle(release); !action.Consumed || action.Activated {
		t.Fatal("release outside activated a button")
	}
	c.Handle(press)
	release.X = 10
	if action := c.Handle(release); !action.Activated || action.ID != "first" {
		t.Fatal("release inside did not activate")
	}
	next := experience.Event{Kind: experience.KeyInput, Keycode: 15, Pressed: true}
	if action := c.Handle(next); action.ID != "query" {
		t.Fatal("Tab did not skip disabled control")
	}
	if action := c.Handle(experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: true}); action.Activated {
		t.Fatal("Enter activated a text field")
	}
	if action := c.Handle(next); action.ID != "last" {
		t.Fatal("Tab did not reach menu item")
	}
	if action := c.Handle(experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: true}); !action.Activated {
		t.Fatal("Enter did not activate menu item")
	}
	if action := c.Handle(next); action.ID != "first" {
		t.Fatal("Tab did not wrap")
	}
	next.Modifiers = experience.ModShift
	if action := c.Handle(next); action.ID != "last" {
		t.Fatal("reverse Tab did not wrap")
	}
	tree := c.Semantics()
	tree.Nodes[0].Label = "mutated"
	if c.Semantics().Nodes[0].Label != "First" {
		t.Fatal("semantic tree shared mutable storage")
	}
	c.Handle(experience.Event{Kind: experience.KeyboardCancel})
	if c.FocusedID() != "" {
		t.Fatal("cancel retained focus")
	}
}

func TestFieldEditingPreservesGraphemesAndClipboardSelection(t *testing.T) {
	f := NewField(200)
	f.Commit("a é 👩‍💻")
	back := experience.Event{Kind: experience.KeyInput, Keycode: 14, Pressed: true}
	if !f.Handle(back, "") || f.Text() != "a é " {
		t.Fatalf("deleted only part of ZWJ grapheme: %q", f.Text())
	}
	f.Handle(back, "")
	f.Handle(back, "")
	if f.Text() != "a " {
		t.Fatalf("deleted only part of combining grapheme: %q", f.Text())
	}
	f.Set("hello world")
	f.SelectAll()
	f.Handle(experience.Event{Kind: experience.KeyInput, Keycode: 105, Pressed: true}, "")
	if f.Caret() != 0 || f.Selection() != "" {
		t.Fatal("Left did not collapse selection to its visual edge")
	}
	f.SetCaret(5, false)
	if !f.Commit("界") || f.Text() != "hello界 world" {
		t.Fatal("insertion ignored caret")
	}
	f.SetCaret(5, false)
	f.SetCaret(8, true)
	if f.Selection() != "界" {
		t.Fatal("selection split UTF-8")
	}
	if !f.Commit("there") || f.Text() != "hellothere world" {
		t.Fatal("commit did not replace selection")
	}
	f.SelectAll()
	f.Commit("new\ntext\x00")
	if f.Text() != "newtext" {
		t.Fatal("single-line commit accepted controls")
	}
	limited := NewField(4)
	limited.Commit("€€")
	if limited.Text() != "€" || !utf8.ValidString(limited.Text()) {
		t.Fatal("byte cap split a rune")
	}
	limited = NewField(2)
	limited.Commit("é")
	if limited.Text() != "" {
		t.Fatal("byte cap split a grapheme")
	}
}

func TestPainterClippingCachingAndFieldHitTesting(t *testing.T) {
	p, err := NewPainter(Cinematic())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	dst := image.NewRGBA(image.Rect(0, 0, 240, 100))
	clip := image.Rect(20, 15, 120, 50)
	if err := p.DrawLabel(dst, clip, "Unicode العربية é 世界", color.RGBA{255, 255, 255, 255}); err != nil {
		t.Fatal(err)
	}
	visible := 0
	for y := 0; y < 100; y++ {
		for x := 0; x < 240; x++ {
			if dst.RGBAAt(x, y).A != 0 {
				if !image.Pt(x, y).In(clip) {
					t.Fatal("text painted outside its clip")
				}
				visible++
			}
		}
	}
	if visible == 0 {
		t.Fatal("shaped label produced no pixels")
	}
	field := NewField(100)
	field.Set("hello world")
	rect := image.Rect(10, 60, 230, 95)
	if err := p.DrawField(dst, rect, field, "", true); err != nil {
		t.Fatal(err)
	}
	if err := p.PlaceCaret(field, rect, image.Pt(rect.Min.X+4, 75), false); err != nil || field.Caret() != 0 {
		t.Fatalf("field hit test = %d %v", field.Caret(), err)
	}
	for i := 0; i < 200; i++ {
		if _, err := p.Measure(strings.Repeat("x", i), 200); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.cache) > 128 {
		t.Fatal("layout cache is unbounded")
	}
	if _, err := p.Measure("overflow", 1<<30); err == nil {
		t.Fatal("unbounded Pango width accepted")
	}
	p.Close()
	if _, err := p.Measure("closed", 20); err == nil {
		t.Fatal("closed painter accepted work")
	}
}
