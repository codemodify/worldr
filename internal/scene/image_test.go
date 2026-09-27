package scene

import (
	"math"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func TestCanvasImagePreservesPainterOrderAndOffscreenBounds(t *testing.T) {
	c, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	texture, err := render.NewTexture(1, 1, []byte{255, 255, 255, 255})
	if err != nil {
		t.Fatal(err)
	}
	c.Rect(0, 0, 30, 30, ColorHex(0xffffff, 1))
	c.Image(texture, -10, -20, 50, 60)
	c.Rect(0, 0, 10, 10, ColorHex(0, 1))
	f := c.Frame()
	if len(f.Commands) != 3 || f.Commands[0].Kind != render.OverlayCommand || f.Commands[1].Kind != render.ImageCommand || f.Commands[2].Kind != render.OverlayCommand {
		t.Fatalf("image reordered canvas operations: %+v", f.Commands)
	}
	if f.Commands[1].Image.Texture != texture || f.Commands[1].Image.Bounds != [4]float32{-10, -20, 50, 60} || f.Commands[2].First != f.Commands[0].Count {
		t.Fatal("image changed bounds or replayed earlier vertices")
	}
	for _, bounds := range [][4]float32{{0, 0, 0, 1}, {0, 0, 1, -1}, {float32(math.NaN()), 0, 1, 1}, {math.MaxFloat32, 0, math.MaxFloat32, 1}} {
		c.Image(texture, bounds[0], bounds[1], bounds[2], bounds[3])
	}
	c.Image(nil, 0, 0, 1, 1)
	if len(c.Frame().Commands) != 3 {
		t.Fatal("invalid images changed the frame")
	}
}
