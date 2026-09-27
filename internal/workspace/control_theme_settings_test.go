package workspace

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

type themeApplications struct {
	fakeApplications
	themes []experience.ControlTheme
}

func (a *themeApplications) SetControlTheme(theme experience.ControlTheme) {
	a.themes = append(a.themes, theme)
}

func TestControlThemeSettingsSelectEveryFamilyAndShapeAndPersist(t *testing.T) {
	for _, dimensions := range []struct{ width, height int }{{1440, 900}, {2880, 1800}} {
		w := desktop(t)
		w.Draw(dimensions.width, dimensions.height)
		beforeDocument, history, historyLength := w.Document(), w.historyPosition, len(w.history)
		beforeState, err := w.SaveState()
		if err != nil {
			t.Fatal(err)
		}
		w.openSettings()
		x, y := settingsPoint(w, settingsThemesButton)
		if !pointer(w, experience.PointerDown, x, y) || !pointer(w, experience.PointerUp, x, y) || w.settingsCategory != settingsThemes {
			t.Fatal("scaled Themes category click did not select its controls")
		}

		families := []struct {
			bounds box
			value  controlThemeFamily
		}{
			{settingsThemeAperture, controlThemeAperture},
			{settingsThemeGlass, controlThemeGlass},
			{settingsThemeInstrument, controlThemeInstrument},
			{settingsThemeTelemetry, controlThemeTelemetry},
		}
		for _, choice := range families {
			x, y = settingsPoint(w, choice.bounds)
			if !pointer(w, experience.PointerDown, x, y) || !pointer(w, experience.PointerUp, x, y) || w.controlTheme.Family != choice.value {
				t.Fatalf("theme family card did not apply %q", choice.value)
			}
		}
		shapes := []struct {
			bounds box
			value  controlShape
		}{
			{settingsShapeBracketed, controlShapeBracketed},
			{settingsShapeSlab, controlShapeSlab},
			{settingsShapeChamfered, controlShapeChamfered},
			{settingsShapeNotched, controlShapeNotched},
		}
		for _, choice := range shapes {
			x, y = settingsPoint(w, choice.bounds)
			if !pointer(w, experience.PointerDown, x, y) || !pointer(w, experience.PointerUp, x, y) || w.controlTheme.Shape != choice.value {
				t.Fatalf("shape card did not apply %q", choice.value)
			}
		}
		if w.Document() != beforeDocument || w.historyPosition != history || len(w.history) != historyLength || w.CanUndo() {
			t.Fatal("control theme preference entered the workspace document or Undo history")
		}
		afterState, err := w.SaveState()
		if err != nil || bytes.Equal(beforeState, afterState) {
			t.Fatal("control theme preference did not change persisted desktop state")
		}
		loaded := desktop(t)
		if err := loaded.LoadState(afterState); err != nil || loaded.controlTheme != (controlThemeSettings{Family: controlThemeTelemetry, Shape: controlShapeNotched}) {
			t.Fatalf("control theme did not round-trip: state=%+v err=%v", loaded.controlTheme, err)
		}
	}
}

func TestControlThemeBroadcastsOnAttachSelectionAndStateLoad(t *testing.T) {
	w := desktop(t)
	apps := &themeApplications{}
	w.SetApplications(apps)
	if want := (experience.ControlTheme{Family: "instrument", Shape: "chamfered"}); len(apps.themes) != 1 || apps.themes[0] != want {
		t.Fatalf("application attach did not receive the current theme: %v", apps.themes)
	}
	w.Draw(1440, 900)
	w.openSettings()
	x, y := settingsPoint(w, settingsThemesButton)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	x, y = settingsPoint(w, settingsThemeGlass)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	x, y = settingsPoint(w, settingsShapeNotched)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if want := (experience.ControlTheme{Family: "glass", Shape: "notched"}); len(apps.themes) != 3 || apps.themes[2] != want {
		t.Fatalf("live theme selection did not reach the provider: %v", apps.themes)
	}

	source := desktop(t)
	source.controlTheme = controlThemeSettings{Family: controlThemeAperture, Shape: controlShapeSlab}
	state, err := source.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.LoadState(state); err != nil {
		t.Fatal(err)
	}
	if want := (experience.ControlTheme{Family: "aperture", Shape: "slab"}); len(apps.themes) != 4 || apps.themes[3] != want {
		t.Fatalf("loaded theme did not reach the provider: %v", apps.themes)
	}
}

func TestControlThemeGalleryChangesWithFamilyAndShape(t *testing.T) {
	w := desktop(t)
	w.Draw(1440, 900)
	w.openSettings()
	x, y := settingsPoint(w, settingsThemesButton)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	seen := make([][]interface{}, 0, len(controlThemeChoices)*len(controlShapeChoices))
	for _, family := range controlThemeChoices {
		for _, shape := range controlShapeChoices {
			w.controlTheme = controlThemeSettings{Family: family.family, Shape: shape.shape}
			vertices := copiedVertices(w.Draw(1440, 900))
			if len(vertices) == 0 {
				t.Fatal("theme gallery rendered no vertices")
			}
			for _, previous := range seen {
				if reflect.DeepEqual(previous[0], vertices) {
					t.Fatalf("theme gallery did not distinguish %s/%s", family.family, shape.shape)
				}
			}
			seen = append(seen, []interface{}{vertices})
		}
	}
}

func TestControlThemeStateLegacyDefaultAndInvalidLoadAreTransactional(t *testing.T) {
	w := desktop(t)
	w.controlTheme = controlThemeSettings{Family: controlThemeGlass, Shape: controlShapeBracketed}
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatal(err)
	}
	themes, ok := encoded["themes"].(map[string]any)
	if !ok || themes["family"] != "glass" || themes["shape"] != "bracketed" {
		t.Fatalf("desktop state omitted control theme settings: %#v", encoded["themes"])
	}

	delete(encoded, "themes")
	legacy, err := json.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	legacyLoaded := desktop(t)
	if err := legacyLoaded.LoadState(legacy); err != nil || legacyLoaded.controlTheme != defaultControlThemeSettings() {
		t.Fatalf("legacy state did not receive control theme defaults: state=%+v err=%v", legacyLoaded.controlTheme, err)
	}

	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatal(err)
	}
	encoded["themes"].(map[string]any)["family"] = "unknown"
	malformed, err := json.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	beforeDocument, beforeEnvironment, beforeWindows, beforeTheme, history := w.Document(), w.environment, w.windows, w.controlTheme, w.historyPosition
	if err := w.LoadState(malformed); err == nil || w.Document() != beforeDocument || w.environment != beforeEnvironment || w.windows != beforeWindows || w.controlTheme != beforeTheme || w.historyPosition != history {
		t.Fatal("invalid control theme changed the live desktop")
	}
}

func TestControlThemeCardsRequireVisibleCompletedUnmovedClicks(t *testing.T) {
	w := desktop(t)
	w.Draw(1440, 900)
	w.openSettings()
	x, y := settingsPoint(w, settingsThemesButton)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)

	x, y = settingsPoint(w, settingsThemeGlass)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerMove, x+12, y)
	pointer(w, experience.PointerMove, x, y)
	pointer(w, experience.PointerUp, x, y)
	if w.controlTheme != defaultControlThemeSettings() {
		t.Fatal("dragging away and back activated a theme card")
	}
	x, y = settingsPoint(w, settingsShapeBracketed)
	pointer(w, experience.PointerDown, x, y)
	w.Handle(experience.Event{Kind: experience.PointerCancel})
	pointer(w, experience.PointerUp, x, y)
	if w.controlTheme != defaultControlThemeSettings() {
		t.Fatal("cancelled shape press activated on a later release")
	}

	x, y = settingsPoint(w, settingsTerminalButton)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	x, y = settingsPoint(w, settingsThemeTelemetry)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if w.controlTheme != defaultControlThemeSettings() {
		t.Fatal("an invisible theme card activated from another category")
	}
}
