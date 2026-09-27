package render

import (
	"reflect"
	"testing"

	fluid "github.com/codemodify/worldr/sdk/fluid/v1"
)

func TestTranslateFluidPreservesBorrowedSourceAndLocalDistances(t *testing.T) {
	field := fluid.Field{Bounds: fluid.Rect{X: 10, Y: 20, Width: 300, Height: 200}, Pointer: fluid.Point{X: 90, Y: 70}, PointerActive: true,
		Style: fluid.DefaultStyle(), Surfaces: []fluid.Surface{{Bounds: fluid.Rect{X: 70, Y: 40, Width: 90, Height: 60}, Radius: 12, Fuse: true, Tint: [4]float32{.1, .2, .3, .8}}}}
	original := field
	original.Surfaces = append([]fluid.Surface(nil), field.Surfaces...)
	translated := TranslateFluid(field, -64, -32)
	if !reflect.DeepEqual(field, original) {
		t.Fatal("output translation modified borrowed source")
	}
	if translated.Bounds.X != -54 || translated.Bounds.Y != -12 || translated.Pointer != (fluid.Point{X: 26, Y: 38}) || translated.Surfaces[0].Bounds.X != 6 || translated.Surfaces[0].Bounds.Y != 8 {
		t.Fatal("output translation lost a viewport, pointer, or surface coordinate")
	}
	translated.Surfaces[0].Radius = 0
	if field.Surfaces[0].Radius != 12 || translated.Style != field.Style || !translated.PointerActive {
		t.Fatal("output copy shares application state or changed style")
	}
}
