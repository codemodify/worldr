package workspace

import "testing"

func TestControlThemeCatalogHasStableValidPairs(t *testing.T) {
	defaults := defaultControlThemeSettings()
	if err := defaults.validate(); err != nil {
		t.Fatal(err)
	}
	if got := (controlThemeSettings{}).normalized(); got != defaults {
		t.Fatalf("zero theme normalized to %+v, want %+v", got, defaults)
	}

	seenFamilies := make(map[controlThemeFamily]bool)
	for _, choice := range controlThemeChoices {
		if !choice.family.valid() || choice.title == "" || choice.description == "" || seenFamilies[choice.family] {
			t.Fatalf("invalid or duplicate theme choice: %+v", choice)
		}
		seenFamilies[choice.family] = true
	}
	seenShapes := make(map[controlShape]bool)
	for _, choice := range controlShapeChoices {
		if !choice.shape.valid() || choice.title == "" || choice.description == "" || seenShapes[choice.shape] {
			t.Fatalf("invalid or duplicate shape choice: %+v", choice)
		}
		seenShapes[choice.shape] = true
	}
	if len(seenFamilies) != 4 || len(seenShapes) != 4 {
		t.Fatalf("theme catalog = %d families and %d shapes, want 4 and 4", len(seenFamilies), len(seenShapes))
	}
}

func TestControlThemeSettingsRejectUnknownValues(t *testing.T) {
	for _, settings := range []controlThemeSettings{
		{Family: "unknown", Shape: controlShapeChamfered},
		{Family: controlThemeInstrument, Shape: "unknown"},
	} {
		if err := settings.validate(); err == nil {
			t.Fatalf("accepted invalid control theme settings: %+v", settings)
		}
	}
}
