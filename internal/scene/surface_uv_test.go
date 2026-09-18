package scene

import (
	"github.com/codemodify/worldr/internal/render"
	"testing"
)

func TestSurfaceCropsStayInDrawContract(t *testing.T) {
	texture, err := render.NewTexture(1, 1, []byte{255, 0, 0, 255})
	if err != nil {
		t.Fatal(err)
	}
	canvas, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Close()
	world := NewScene()
	uv := [4]float32{.1, .2, .5, .6}
	world.Add(0, Node{Surface: texture, SurfaceUV: uv, Color: Color{1, 1, 1, 1}})
	canvas.Reset(100, 100)
	world.Draw(canvas, testCamera(), Viewport{Width: 100, Height: 100})
	frame := canvas.Frame()
	if len(frame.Commands) != 1 || len(frame.Commands[0].Draws) != 1 || frame.Commands[0].Draws[0].UV != uv {
		t.Fatal("surface crop missing from retained draw")
	}
}
