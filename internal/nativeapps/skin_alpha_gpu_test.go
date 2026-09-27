//go:build linux && cgo

package nativeapps

import (
	"os"
	"testing"

	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/terminal"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func TestFuturePanelsTextureComposesInTranslucentSceneGPU(t *testing.T) {
	p, backend := testProvider(t)
	for i := range backend.snapshot.Cells {
		backend.snapshot.Cells[i].Background = terminal.Color{R: 8, G: 16, B: 23}
		backend.snapshot.Cells[i].Foreground = terminal.Color{R: 220, G: 235, B: 238}
	}
	backend.snapshot.Cells[0].Chars[0] = 'M'
	backend.snapshot.Cells[0].Underline = true
	backend.snapshot.Cells[1].Background = terminal.Color{R: 170, G: 20, B: 40}
	selected, err := skin.Builtin("future-panels")
	if err != nil {
		t.Fatal(err)
	}
	p.SetSkin(selected)
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	width, height := p.renderer.image.Rect.Dx(), p.renderer.image.Rect.Dy()
	gpu, err := native.OpenVK(false, uint32(width), uint32(height))
	if err != nil {
		if os.Getenv("WORLDR_TEST_GPU") == "1" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	defer gpu.Close()
	projection := [16]float32{1, 0, 0, 0, 0, -1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
	model := [16]float32{2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 1, 0, 0, 0, .5, 1}
	frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand,
		View:  render.View{Projection: projection, Viewport: [4]float32{0, 0, float32(width), float32(height)}},
		Draws: []render.Draw{{Texture: p.renderer.texture, Model: model, Color: [4]float32{1, 1, 1, 1}, Translucent: true}},
	}}}
	red, blue := make([]byte, width*height*4), make([]byte, width*height*4)
	if err := gpu.RenderFrame(frame, [4]float32{1, 0, 0, 1}, red); err != nil {
		t.Fatal(err)
	}
	if err := gpu.RenderFrame(frame, [4]float32{0, 0, 1, 1}, blue); err != nil {
		t.Fatal(err)
	}
	background := ((contentTop+5)*width + contentLeft + 4*cellWidth + 5) * 4
	if int(red[background+2])-int(blue[background+2]) < 20 || int(blue[background])-int(red[background]) < 20 {
		t.Fatalf("scene behind terminal background was hidden: red=%v blue=%v", red[background:background+4], blue[background:background+4])
	}
	for _, point := range [][2]int{{contentLeft + 1, contentTop + cellHeight - 3}, {contentLeft + cellWidth + 5, contentTop + 5}} {
		i := (point[1]*width + point[0]) * 4
		for channel := 0; channel < 4; channel++ {
			delta := int(red[i+channel]) - int(blue[i+channel])
			if delta < -2 || delta > 2 {
				t.Fatalf("opaque text or ANSI background blended scene at %v: %v vs %v", point, red[i:i+4], blue[i:i+4])
			}
		}
	}
}
