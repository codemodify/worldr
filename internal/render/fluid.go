package render

import fluid "github.com/codemodify/worldr/sdk/fluid/v1"

// TranslateFluid copies the small surface array before moving a field into a
// physical output's coordinate space. The source frame may feed several outputs
// and its borrowed application-owned surface coordinates must remain unchanged.
func TranslateFluid(field fluid.Field, x, y float32) fluid.Field {
	field.Surfaces = append([]fluid.Surface(nil), field.Surfaces...)
	field.Bounds.X += x
	field.Bounds.Y += y
	field.Pointer.X += x
	field.Pointer.Y += y
	for i := range field.Surfaces {
		field.Surfaces[i].Bounds.X += x
		field.Surfaces[i].Bounds.Y += y
	}
	return field
}
