package nativeui

import (
	"bytes"
	"image"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	sdkui "github.com/codemodify/worldr/sdk/nativeui/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func TestSkinBridgeUsesPublicControlRecipesAndPreservesNativeEditing(t *testing.T) {
	selected, err := skin.Builtin("merrick")
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPainter(Cinematic())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	field := NewField(100)
	field.Set("base é")
	field.Handle(experience.Event{Kind: experience.TextPreedit, Text: "かな", PreeditBegin: 3, PreeditEnd: 6}, "")
	prior := *field
	beforeMetrics, err := p.Measure("base é", -1)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetSkin(selected); err != nil {
		t.Fatal(err)
	}
	afterMetrics, err := p.Measure("base é", -1)
	if err != nil || beforeMetrics != afterMetrics {
		t.Fatal("skin replaced the native text layout engine or document metrics", err)
	}
	actual, expected := image.NewRGBA(image.Rect(0, 0, 180, 50)), image.NewRGBA(image.Rect(0, 0, 180, 50))
	bounds := image.Rect(8, 8, 172, 42)
	if err := p.DrawButton(actual, Node{ID: "test", Role: RoleButton, Bounds: bounds}, false, false); err != nil {
		t.Fatal(err)
	}
	theme, err := sdkui.ThemeFromSkin(selected)
	if err != nil {
		t.Fatal(err)
	}
	portable, err := sdkui.NewPainter(theme)
	if err != nil {
		t.Fatal(err)
	}
	if err := portable.DrawControlBackground(expected, "button", bounds, sdkui.State{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual.Pix, expected.Pix) {
		t.Fatal("internal control silhouette differs from the public SDK skin recipe")
	}
	if err := p.DrawField(actual, bounds, field, "", true); err != nil {
		t.Fatal(err)
	}
	if field.text != prior.text || field.caret != prior.caret || field.anchor != prior.anchor || field.preedit != prior.preedit || field.preeditBegin != prior.preeditBegin || field.preeditEnd != prior.preeditEnd {
		t.Fatal("skin drawing changed field/IME state")
	}
	selected.Version = 999
	before := p.Theme
	if p.SetSkin(selected) == nil || p.Theme != before {
		t.Fatal("invalid skin changed native painter")
	}
}
