package scene

import (
	"testing"

	"github.com/codemodify/worldr/internal/render"
	fluid "github.com/codemodify/worldr/sdk/fluid/v1"
)

func TestFluidCanvasPreservesOrderedOverlayBatches(t *testing.T) {
	c, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	c.Reset(200, 120)
	c.Rect(0, 0, 10, 10, ColorHex(0xff0000, 1))
	field := fluid.Field{Bounds: fluid.Rect{Width: 200, Height: 120}, Style: fluid.DefaultStyle()}
	c.Fluid(field)
	c.Rect(20, 20, 10, 10, ColorHex(0x00ff00, 1))
	frame := c.Frame()
	if len(frame.Commands) != 3 || frame.Commands[0].Kind != render.OverlayCommand || frame.Commands[1].Kind != render.FluidCommand || frame.Commands[2].Kind != render.OverlayCommand || frame.Commands[2].First != frame.Commands[0].Count {
		t.Fatal("fluid field reordered or merged preceding/following overlays")
	}
	if frame.Commands[1].Fluid.Bounds != field.Bounds || frame.Commands[1].Fluid.Style != field.Style {
		t.Fatal("canvas changed authored fluid field")
	}
}
