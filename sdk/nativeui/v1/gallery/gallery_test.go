package gallery

import (
	"crypto/sha256"
	"testing"
	"time"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
	nativeui "github.com/codemodify/worldr/sdk/nativeui/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func TestGalleryLiveControlsSurviveSkinSwitch(t *testing.T) {
	selected, err := skin.Builtin("merrick")
	if err != nil {
		t.Fatal(err)
	}
	theme, err := nativeui.ThemeFromSkin(selected)
	if err != nil {
		t.Fatal(err)
	}
	g, err := New(theme)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	if _, err = g.Render(960, 600); err != nil {
		t.Fatal(err)
	}
	click := func(id string) {
		t.Helper()
		for _, node := range g.Semantics().Nodes {
			if node.ID == id {
				x, y := float32(node.Bounds.X+node.Bounds.Width/2), float32(node.Bounds.Y+node.Bounds.Height/2)
				_, _ = g.Handle(nativeapp.Event{Kind: nativeapp.PointerDown, Button: nativeapp.ButtonPrimary, X: x, Y: y})
				_, _ = g.Handle(nativeapp.Event{Kind: nativeapp.PointerUp, Button: nativeapp.ButtonPrimary, X: x, Y: y})
				return
			}
		}
		t.Fatalf("missing control %s", id)
	}
	click("run")
	if !g.Update(time.Second) || g.Values.Progress <= 42 {
		t.Fatal("run button did not start analysis")
	}
	click("query")
	_, err = g.Handle(nativeapp.Event{Kind: nativeapp.TextCommit, Text: "field"})
	if err != nil {
		t.Fatal(err)
	}
	if g.Values.Query != "field" {
		t.Fatal("search text was not accepted")
	}
	want := g.Values
	seen := map[[32]byte]bool{}
	for _, s := range skin.Builtins() {
		theme, err = nativeui.ThemeFromSkin(s)
		if err != nil {
			t.Fatal(err)
		}
		if err = g.SetTheme(theme); err != nil {
			t.Fatal(err)
		}
		pixels, err := g.Render(960, 600)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(pixels.Pix)
		if seen[hash] {
			t.Fatalf("skin %s did not change rendering", s.ID)
		}
		seen[hash] = true
		if g.Values != want || g.Semantics().FocusedID != "query" {
			t.Fatal("skin switch lost application state/focus")
		}
	}
	if _, err = g.Render(790, 340); err != nil {
		t.Fatalf("settings preview: %v", err)
	}
}
