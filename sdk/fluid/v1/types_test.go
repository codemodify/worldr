package fluid

import (
	"math"
	"testing"
)

func testField() Field {
	return Field{Bounds: Rect{-40, -20, 800, 600}, Style: DefaultStyle(), Surfaces: []Surface{{Bounds: Rect{-10, 20, 140, 90}, Radius: 18, Tint: [4]float32{.8, .4, .2, .3}, Fuse: true}}}
}
func TestValidationDefaultsAndOwnedClone(t *testing.T) {
	f := testField()
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	// Straight tint channels may exceed alpha; zero material values are valid.
	zero := f.Clone()
	zero.Style = Style{}
	if err := zero.Validate(); err != nil {
		t.Fatal(err)
	}
	empty := f.Clone()
	empty.Surfaces = nil
	if err := empty.Validate(); err != nil {
		t.Fatal(err)
	}
	outside := f.Clone()
	outside.Surfaces[0].Bounds.X = 1000
	if err := outside.Validate(); err != nil {
		t.Fatal("partially/offscreen surfaces should remain valid", err)
	}
	copy := f.Clone()
	copy.Surfaces[0].Bounds.X = 7
	copy.Surfaces[0].Tint[0] = 0
	copy.Style.Background[0][0] = .8
	if f.Surfaces[0].Bounds.X != -10 || f.Surfaces[0].Tint[0] != .8 || f.Style.Background[0][0] == .8 {
		t.Fatal("clone aliases mutable input")
	}
	a, b := DefaultStyle(), DefaultStyle()
	a.Background[0][0] = 1
	if a == b {
		t.Fatal("default styles share state")
	}
	f.Style.Blend = 384
	if err := f.Validate(); err != nil {
		t.Fatal("DPI-scaled blend rejected", err)
	}
}
func TestValidationRejectsMalformedAndUnboundedFields(t *testing.T) {
	nan, inf := float32(math.NaN()), float32(math.Inf(1))
	tests := map[string]func(*Field){
		"negative width": func(f *Field) { f.Bounds.Width = -1 }, "empty height": func(f *Field) { f.Bounds.Height = 0 },
		"infinite extent": func(f *Field) { f.Bounds.Width = inf }, "nan coordinate": func(f *Field) { f.Bounds.X = nan },
		"far coordinate": func(f *Field) { f.Bounds.X = MaxCoordinate }, "huge dimension": func(f *Field) { f.Bounds.Height = MaxDimension + 1 },
		"negative radius": func(f *Field) { f.Surfaces[0].Radius = -1 }, "oversize radius": func(f *Field) { f.Surfaces[0].Radius = 46 },
		"nan radius": func(f *Field) { f.Surfaces[0].Radius = nan }, "invalid surface": func(f *Field) { f.Surfaces[0].Bounds.Width = 0 },
		"tint low": func(f *Field) { f.Surfaces[0].Tint[0] = -.01 }, "tint high": func(f *Field) { f.Surfaces[0].Tint[3] = 1.01 },
		"tint nan": func(f *Field) { f.Surfaces[0].Tint[2] = nan }, "too many surfaces": func(f *Field) { f.Surfaces = make([]Surface, MaxSurfaces+1) },
		"negative blend": func(f *Field) { f.Style.Blend = -1 }, "huge blend": func(f *Field) { f.Style.Blend = MaxBlend + 1 },
		"nan blend": func(f *Field) { f.Style.Blend = nan }, "rim high": func(f *Field) { f.Style.Rim = 2 },
		"refraction nan": func(f *Field) { f.Style.Refraction = nan }, "frost low": func(f *Field) { f.Style.Frost = -1 },
		"glow infinite": func(f *Field) { f.Style.Glow = inf }, "opacity high": func(f *Field) { f.Style.Opacity = 2 },
		"background invalid": func(f *Field) { f.Style.Background[2][3] = inf }, "negative time": func(f *Field) { f.Time = -1 },
		"huge time": func(f *Field) { f.Time = MaxTime + 1 }, "nan time": func(f *Field) { f.Time = nan },
		"inactive pointer nan": func(f *Field) { f.Pointer.X = nan }, "active pointer far": func(f *Field) { f.PointerActive = true; f.Pointer.Y = -MaxCoordinate - 1 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			f := testField()
			mutate(&f)
			if err := f.Validate(); err == nil {
				t.Fatal("invalid field accepted")
			}
		})
	}
}
