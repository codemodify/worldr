//go:build linux && cgo

package native

import (
	"os"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func TestFrameResidentGeometryGPU(t *testing.T) {
	vk, err := OpenVK(false, 64, 64)
	if err != nil {
		if os.Getenv("WORLDR_TEST_GPU") == "1" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	defer vk.Close()
	geometry, err := render.NewGeometry([]render.MeshVertex{
		{X: -.75, Y: -.75, Z: .5, NZ: 1, R: 1, G: 1, B: 1, A: 1, BX: 1},
		{X: .75, Y: -.75, Z: .5, NZ: 1, R: 1, G: 1, B: 1, A: 1, BY: 1},
		{X: -.75, Y: .75, Z: .5, NZ: 1, R: 1, G: 1, B: 1, A: 1, BZ: 1},
	}, []uint32{0, 1, 2})
	if err != nil {
		t.Fatal(err)
	}
	identity := [16]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
	view := render.View{Projection: identity, Eye: [3]float32{0, 0, 2}, Light: [3]float32{0, 0, 1}, Viewport: [4]float32{0, 0, 64, 64}}
	draw := render.Draw{Geometry: geometry, Model: identity, Color: [4]float32{1, 0, 0, 1}}
	quad := func(x, y, w, h, z float32, color [4]float32) []render.Vertex {
		result := make([]render.Vertex, 0, 6)
		for _, p := range [][2]float32{{x, y}, {x + w, y}, {x, y + h}, {x + w, y}, {x + w, y + h}, {x, y + h}} {
			result = append(result, render.Vertex{X: p[0], Y: p[1], Z: z, U: .5, V: .5, R: color[0], G: color[1], B: color[2], A: color[3]})
		}
		return result
	}
	background := quad(0, 0, 64, 64, 0, [4]float32{0, 1, 0, 1})
	vertices := append(background, quad(12, 12, 8, 8, 1, [4]float32{0, 0, 1, 1})...)
	frame := render.Frame{Vertices: vertices, Commands: []render.Command{
		{Kind: render.OverlayCommand, First: 0, Count: 6},
		{Kind: render.SceneCommand, View: view, Draws: []render.Draw{draw}},
		{Kind: render.OverlayCommand, First: 6, Count: 6},
	}}
	dst := make([]byte, 64*64*4)
	width := 64
	pixel := func(x, y int, want [4]byte) {
		t.Helper()
		got := dst[(y*width+x)*4:][:4]
		for i, value := range want {
			d := int(got[i]) - int(value)
			if d < -2 || d > 2 {
				t.Fatalf("(%d,%d) BGRA=%v, want %v", x, y, got, want)
			}
		}
	}
	renderFrame := func() {
		t.Helper()
		if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, dst); err != nil {
			t.Fatal(err)
		}
	}
	renderFrame()
	pixel(10, 10, [4]byte{0, 0, 255, 255}) // GPU transforms/lighting; background's depth0 cannot occlude.
	pixel(14, 14, [4]byte{255, 0, 0, 255}) // Overlay depth1 still covers the mesh.
	pixel(58, 58, [4]byte{0, 255, 0, 255})
	if len(vk.frame.uploaded) != 1 {
		t.Fatalf("resident resources=%d", len(vk.frame.uploaded))
	}
	// Change only the instance transform. Geometry identity and residency persist.
	frame.Commands = frame.Commands[:2]
	frame.Commands[1].Draws[0].Model[12] = .5
	renderFrame()
	pixel(10, 10, [4]byte{0, 255, 0, 255})
	pixel(28, 10, [4]byte{0, 0, 255, 255})
	if len(vk.frame.uploaded) != 1 {
		t.Fatal("transform change uploaded a second mesh")
	}
	// Lighting is evaluated by the shader using the per-view world-space light.
	frame.Commands[1].View.Light = [3]float32{0, 0, -1}
	renderFrame()
	pixel(28, 10, [4]byte{0, 0, 71, 255})
	// Distinct camera passes reset depth; a later far object can cover an earlier near one.
	near, far := draw, draw
	near.Model[14] = -.3
	far.Model[14] = .3
	far.Color = [4]float32{0, 0, 1, 1}
	frame.Commands = []render.Command{
		{Kind: render.SceneCommand, View: view, Draws: []render.Draw{near}},
		{Kind: render.SceneCommand, View: view, Draws: []render.Draw{far}},
	}
	renderFrame()
	pixel(12, 12, [4]byte{255, 0, 0, 255})
	// Within one camera pass, depth rejects a farther instance submitted later.
	frame.Commands = []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{near, far}}}
	renderFrame()
	pixel(12, 12, [4]byte{0, 0, 255, 255})
	// Hardware viewport clips this scene and leaves the rest of the image clear.
	frame.Commands[0].View.Viewport = [4]float32{16, 16, 32, 32}
	renderFrame()
	pixel(12, 12, [4]byte{0, 0, 0, 255})
	pixel(22, 22, [4]byte{0, 0, 255, 255})
	if err := vk.ReleaseGeometry(geometry.ID()); err != nil {
		t.Fatal(err)
	}
	if len(vk.frame.uploaded) != 0 {
		t.Fatal("released geometry retained in upload cache")
	}
	renderFrame() // Referencing a released CPU resource safely uploads it again.
	pixel(22, 22, [4]byte{0, 0, 255, 255})
	if len(vk.frame.uploaded) != 1 {
		t.Fatal("released geometry was not restored")
	}
	// Swapchain/offscreen target resize must preserve both independent resource
	// classes: immutable meshes and the R8 atlas used by dynamic overlays.
	if err := vk.SetSceneAtlas(render.Atlas{Width: 1, Height: 1, Pixels: []byte{128}}); err != nil {
		t.Fatal(err)
	}
	if err := vk.Resize(80, 48); err != nil {
		t.Fatal(err)
	}
	if w, h := vk.Size(); w != 80 || h != 48 {
		t.Fatalf("resize extent %dx%d", w, h)
	}
	width = 80
	dst = make([]byte, 80*48*4)
	view.Viewport = [4]float32{0, 0, 80, 48}
	frame.Vertices = quad(0, 0, 80, 48, 0, [4]float32{0, 1, 0, 1})
	frame.Commands = []render.Command{
		{Kind: render.OverlayCommand, First: 0, Count: 6},
		{Kind: render.SceneCommand, View: view, Draws: []render.Draw{draw}},
	}
	renderFrame()
	pixel(12, 8, [4]byte{0, 0, 255, 255})
	pixel(76, 46, [4]byte{0, 128, 0, 255})
	if len(vk.frame.uploaded) != 1 {
		t.Fatal("resize lost immutable resource cache")
	}
	if err := vk.ReleaseGeometry(geometry.ID()); err != nil {
		t.Fatal(err)
	}
	renderFrame()
	pixel(12, 8, [4]byte{0, 0, 255, 255})
	pixel(76, 46, [4]byte{0, 128, 0, 255})
}
