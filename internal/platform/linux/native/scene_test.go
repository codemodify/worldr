package native

import (
	"os"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func TestSceneClosedSession(t *testing.T) {
	var vk *VK
	if err := vk.SetSceneAtlas(render.Atlas{Width: 1, Height: 1, Pixels: []byte{255}}); err == nil {
		t.Fatal("closed session accepted atlas")
	}
	if err := vk.RenderFrame(render.Frame{}, [4]float32{}, nil); err == nil {
		t.Fatal("closed session accepted render")
	}
}

// This test executes the checked-in SPIR-V on a real Vulkan ICD. CI without an
// ICD skips; WORLDR_TEST_GPU=1 makes an unavailable device a test failure.
func TestOverlayGPUReadback(t *testing.T) {
	vk, err := OpenVK(false, 64, 32)
	if err != nil {
		if os.Getenv("WORLDR_TEST_GPU") == "1" {
			t.Fatal(err)
		}
		t.Skipf("Vulkan unavailable: %v", err)
	}
	defer vk.Close()
	t.Logf("scene GPU: %s", vk.DeviceName())
	renderVertices := func(vertices []render.Vertex, clear [4]float32, dst []byte) error {
		return vk.RenderFrame(render.Frame{Vertices: vertices, Commands: []render.Command{{Kind: render.OverlayCommand, Count: len(vertices)}}}, clear, dst)
	}
	atlas := render.Atlas{Width: 4, Height: 1, Pixels: []byte{255, 128, 0, 255}}
	if err := vk.SetSceneAtlas(atlas); err != nil {
		t.Fatal(err)
	}
	if err := vk.SetSceneAtlas(render.Atlas{Width: 2, Height: 2, Pixels: []byte{255}}); err == nil {
		t.Fatal("short atlas accepted")
	}
	if err := renderVertices(make([]render.Vertex, 1), [4]float32{}, nil); err == nil {
		t.Fatal("incomplete triangle accepted")
	}
	if err := renderVertices(nil, [4]float32{}, make([]byte, 3)); err == nil {
		t.Fatal("short readback accepted")
	}
	var vertices []render.Vertex
	vertex := func(x, y, z, u float32, c [4]float32) render.Vertex {
		return render.Vertex{X: x, Y: y, Z: z, U: u, V: .5, R: c[0], G: c[1], B: c[2], A: c[3]}
	}
	triangle := func(z float32, c [4]float32) {
		vertices = append(vertices, vertex(0, 0, z, .125, c), vertex(16, 0, z, .125, c), vertex(0, 16, z, .125, c))
	}
	quad := func(x, y, w, h, z, u float32, c [4]float32) {
		vertices = append(vertices, vertex(x, y, z, u, c), vertex(x+w, y, z, u, c), vertex(x, y+h, z, u, c),
			vertex(x+w, y, z, u, c), vertex(x+w, y+h, z, u, c), vertex(x, y+h, z, u, c))
	}
	red, blue, green := [4]float32{1, 0, 0, 1}, [4]float32{0, 0, 1, 1}, [4]float32{0, 1, 0, 1}
	// 2D overlays follow painter order independently of per-vertex depth.
	triangle(.2, red)
	triangle(.8, blue)
	quad(20, 2, 16, 16, .8, .125, blue)
	quad(20, 2, 16, 16, .2, .125, [4]float32{1, 0, 0, .5})
	quad(40, 2, 16, 16, .2, .375, green)
	// Zero atlas coverage must not write depth and hide a later, farther object.
	quad(0, 20, 16, 10, .1, .625, red)
	quad(0, 20, 16, 10, .9, .125, blue)
	dst := make([]byte, 64*32*4)
	assertPixel := func(x, y int, want [4]byte) {
		t.Helper()
		got := dst[(y*64+x)*4:][:4]
		for i, value := range want {
			delta := int(got[i]) - int(value)
			if delta < -1 || delta > 1 {
				t.Fatalf("pixel(%d,%d) BGRA=%v; want %v", x, y, got, want)
			}
		}
	}
	for frame := 0; frame < 3; frame++ {
		if err := renderVertices(vertices, [4]float32{0, 0, 0, 1}, dst); err != nil {
			t.Fatal(err)
		}
		assertPixel(2, 2, [4]byte{255, 0, 0, 255})
		assertPixel(14, 14, [4]byte{0, 0, 0, 255})
		assertPixel(22, 4, [4]byte{128, 0, 128, 255})
		assertPixel(42, 4, [4]byte{0, 128, 0, 255})
		assertPixel(2, 25, [4]byte{255, 0, 0, 255})
		assertPixel(62, 29, [4]byte{0, 0, 0, 255})
	}
	if err := renderVertices(vertices, [4]float32{0, 0, 0, 1}, nil); err != nil {
		t.Fatal(err)
	}
	// Replacing the atlas updates the cached descriptor and remains renderable.
	if err := vk.SetSceneAtlas(render.Atlas{Width: 1, Height: 1, Pixels: []byte{255}}); err != nil {
		t.Fatal(err)
	}
	if err := renderVertices(vertices, [4]float32{0, 0, 0, 1}, dst); err != nil {
		t.Fatal(err)
	}
	assertPixel(42, 4, [4]byte{0, 255, 0, 255})
	// Empty frames still clear and read back through the render pass.
	if err := renderVertices(nil, [4]float32{.25, .5, .75, 1}, dst); err != nil {
		t.Fatal(err)
	}
	assertPixel(0, 0, [4]byte{191, 128, 64, 255})
	assertPixel(63, 31, [4]byte{191, 128, 64, 255})
}
