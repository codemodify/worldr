package workspace

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

type skinApplications struct {
	themeApplications
	skins []skin.Skin
}

func (a *skinApplications) SetSkin(s skin.Skin) { a.skins = append(a.skins, s) }

func TestSkinOwnershipPropagationAndPersistence(t *testing.T) {
	w := desktop(t)
	selected, err := skin.Builtin("merrick")
	if err != nil {
		t.Fatal(err)
	}
	selected.ID = "custom.local"
	selected.Name = "Custom local skin"
	selected.Palette["accent"] = "#EA8833"
	before := w.Document()
	if err = w.SetSkin(selected); err != nil {
		t.Fatal(err)
	}
	selected.Palette["accent"] = "#000000"
	if w.CurrentSkin().Palette["accent"] != "#EA8833" {
		t.Fatal("SetSkin retained caller's mutable palette")
	}
	copy := w.CurrentSkin()
	copy.Palette["accent"] = "#FFFFFF"
	if w.CurrentSkin().Palette["accent"] != "#EA8833" {
		t.Fatal("CurrentSkin exposed mutable palette")
	}
	apps := &skinApplications{}
	w.SetApplications(apps)
	if len(apps.skins) != 1 || apps.skins[0].ID != "custom.local" {
		t.Fatal("new provider did not inherit custom skin")
	}
	apps.skins[0].Palette["accent"] = "#112233"
	if w.CurrentSkin().Palette["accent"] != "#EA8833" {
		t.Fatal("provider can mutate workspace skin")
	}
	if w.Document() != before || w.CanUndo() {
		t.Fatal("appearance changed workspace document/history")
	}
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	loaded := desktop(t)
	if err = loaded.LoadState(data); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.CurrentSkin(), w.CurrentSkin()) {
		t.Fatal("custom recipe did not round-trip")
	}
	var bad map[string]any
	if err = json.Unmarshal(data, &bad); err != nil {
		t.Fatal(err)
	}
	bad["skin"].(map[string]any)["version"] = 99
	invalid, _ := json.Marshal(bad)
	if err = loaded.LoadState(invalid); err == nil {
		t.Fatal("unsupported package accepted")
	}
	if !reflect.DeepEqual(loaded.CurrentSkin(), w.CurrentSkin()) {
		t.Fatal("rejected load changed skin")
	}
	legacy := desktop(t)
	legacyData, _ := legacy.SaveState()
	if err = loaded.LoadState(legacyData); err != nil {
		t.Fatal(err)
	}
	if loaded.CurrentSkin() != nil {
		t.Fatal("loading legacy state left an active custom skin")
	}
}

func TestSkinSettingsSelectAndRenderSDKPreviewAtScale(t *testing.T) {
	w := desktop(t)
	w.Draw(2880, 1800)
	w.openSettings()
	click := func(b box) {
		t.Helper()
		x, y := settingsPoint(w, b)
		pointer(w, experience.PointerDown, x, y)
		pointer(w, experience.PointerUp, x, y)
	}
	click(settingsSkinsButton)
	if w.settingsCategory != settingsSkins {
		t.Fatal("skin category did not open")
	}
	for i, id := range skinPresets {
		click(skinChoiceBox(0, i))
		if w.activeSkin == nil || w.activeSkin.ID != id {
			t.Fatalf("skin selection did not apply %s", id)
		}
		frame := w.Draw(2880, 1800)
		found := false
		for _, c := range frame.Commands {
			if c.Kind == render.ImageCommand && c.Image.Texture == w.skinPreview.texture {
				found = true
				if c.Image.Bounds != [4]float32{904, 884, 1580, 680} {
					t.Fatalf("preview did not scale with Settings: %v", c.Image.Bounds)
				}
			}
		}
		if !found || w.skinPreview.err != "" {
			t.Fatalf("SDK preview missing: %s", w.skinPreview.err)
		}
	}
	click(skinChoiceBox(1, 2))
	click(skinChoiceBox(2, 1))
	click(skinChoiceBox(3, 2))
	if w.activeSkin.Palette["accent"] != skinAccents[2] || w.activeSkin.Typography.Size != 16 {
		t.Fatal("palette/typography edits not applied")
	}
	for _, recipe := range w.activeSkin.Controls {
		if recipe.Layers[0].Geometry.Kind != "rounded" {
			t.Fatal("control shape override not applied")
		}
	}
	click(settingsWindowsButton)
	click(settingsWindowAperture)
	if w.activeSkin != nil || w.windows.Border != windowBorderAperture {
		t.Fatal("legacy border did not replace package")
	}
}
