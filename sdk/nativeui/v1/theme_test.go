package nativeui

import (
	"image/color"
	"reflect"
	"testing"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
)

func TestBuiltinThemeCatalogIsValidatedDistinctAndOwned(t *testing.T) {
	gotFamilies, gotShapes := Families(), ShapeGrammars()
	if len(gotFamilies) != 4 || len(gotShapes) != 4 {
		t.Fatalf("catalog sizes = %d families, %d shapes", len(gotFamilies), len(gotShapes))
	}
	gotFamilies[0], gotShapes[0] = "mutated", "mutated"
	if reflect.DeepEqual(gotFamilies, Families()) || reflect.DeepEqual(gotShapes, ShapeGrammars()) {
		t.Fatal("catalog returned shared mutable storage")
	}
	accents := map[color.RGBA]Family{}
	for _, family := range Families() {
		for _, shape := range ShapeGrammars() {
			theme, err := Builtin(family, shape)
			if err != nil || theme.Validate() != nil || theme.Family != family || theme.Shape != shape {
				t.Fatalf("Builtin(%q,%q) = %+v, %v", family, shape, theme, err)
			}
		}
		theme, _ := Builtin(family, Chamfered)
		if previous, duplicate := accents[theme.Palette.Accent]; duplicate {
			t.Fatalf("%q and %q share the same primary accent", previous, family)
		}
		accents[theme.Palette.Accent] = family
	}
}

func TestNativeAppControlThemeRoundTrip(t *testing.T) {
	preference := nativeapp.ControlTheme{Family: "glass", Shape: "notched"}
	theme, err := FromControlTheme(preference)
	if err != nil || theme.Family != Glass || theme.Shape != Notched || theme.ControlTheme() != preference {
		t.Fatalf("control theme conversion: theme=%+v preference=%+v err=%v", theme, theme.ControlTheme(), err)
	}
	if _, err := FromControlTheme(nativeapp.ControlTheme{Family: "glass", Shape: "round"}); err == nil {
		t.Fatal("accepted an invalid host control theme")
	}
}

func TestThemeValidationAndPainterUpdateAreTransactional(t *testing.T) {
	painter, err := NewPainter(DefaultTheme())
	if err != nil {
		t.Fatal(err)
	}
	before := painter.Theme()
	invalid := before
	invalid.Metrics.ControlHeight = 0
	if err := painter.SetTheme(invalid); err == nil {
		t.Fatal("accepted invalid metrics")
	}
	if painter.Theme() != before {
		t.Fatal("invalid update partially changed the painter")
	}
	if _, err := Builtin("unknown", Chamfered); err == nil {
		t.Fatal("accepted an unknown family")
	}
	if _, err := before.WithShape("unknown"); err == nil {
		t.Fatal("accepted an unknown shape")
	}
	invalid = before
	invalid.Palette.Text.A = 0
	if err := invalid.Validate(); err == nil {
		t.Fatal("accepted a fully transparent semantic color")
	}
}
