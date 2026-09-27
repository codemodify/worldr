package skin

import (
	"bytes"
	"reflect"
	"testing"
)

func TestFuturePanelsCustomPackageRetainsSceneTraitAndReadableAlpha(t *testing.T) {
	s, err := Builtin("future-panels")
	if err != nil {
		t.Fatal(err)
	}
	if s.Desktop.Backdrop != "panel-field" || s.Desktop.Chrome != "" {
		t.Fatal("panel field must retain ordinary desktop navigation")
	}
	for _, token := range []string{"background", "surface"} {
		if alpha := s.Color(token).A; alpha == 0 || alpha == 255 {
			t.Fatalf("%s must preserve authored sheet transparency: %d", token, alpha)
		}
	}
	for _, token := range []string{"text", "muted", "chrome-text"} {
		if s.Color(token).A != 255 {
			t.Fatalf("%s loses reading contrast", token)
		}
	}
	base, _ := Builtin("advanced")
	for name := range base.Controls {
		if _, ok := s.Controls[name]; !ok {
			t.Fatalf("missing native control recipe %q", name)
		}
	}
	// The scene treatment survives export under a user's own ID and palette.
	s.ID, s.Name = "org.example.sheet-workspace", "My sheets"
	s.Palette["field-panel"] = "#293B5750"
	var data bytes.Buffer
	if err := Encode(&data, s); err != nil {
		t.Fatal(err)
	}
	loaded, err := Decode(&data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, loaded) {
		t.Fatal("custom panel-field package lost authored content or alpha")
	}
}

func TestBuiltinPanelPackagesOwnMutableRecipes(t *testing.T) {
	first, second := Builtins(), Builtins()
	var changed *Skin
	for i := range first {
		if first[i].ID == "future-panels" {
			changed = &first[i]
		}
	}
	if changed == nil {
		t.Fatal("future-panels missing from selectable packages")
	}
	before := changed.Clone()
	changed.Palette["text"] = "#FFFFFF"
	changed.Controls["button"].Layers[0].Fill = "danger"
	changed.Controls["button"].States["hovered"] = StateStyle{Fill: "danger"}
	changed.Window.Layout.ButtonOrder[0] = "close"
	changed.Icons["window-close"].Paths[0].Points[0].X = .1
	for _, s := range second {
		if s.ID == "future-panels" && !reflect.DeepEqual(s, before) {
			t.Fatal("separate Builtins calls share future-panels package data")
		}
	}
	again, err := Builtin("future-panels")
	if err != nil || !reflect.DeepEqual(again, before) {
		t.Fatal("mutating a returned package altered the builtin source")
	}
	if changed.Controls["icon-button"].Layers[0].Fill == "danger" {
		t.Fatal("independent control recipes share mutable layers")
	}
}
