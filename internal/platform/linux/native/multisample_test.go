//go:build linux && cgo

package native

import (
	"bytes"
	"testing"

	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

func TestMultisampleMeshAndOverlayEdgeCoverageGPU(t *testing.T) {
	vk := openTextureGPU(t)
	if vk.SampleCount() != 0 {
		t.Fatal("scene sample count initialized before any scene resources")
	}
	positions := [][2]float32{{7.3, 9.1}, {57.2, 13.7}, {13.4, 54.6}}
	overlay := make([]render.Vertex, 3)
	meshVertices := make([]render.MeshVertex, 3)
	for i, p := range positions {
		overlay[i] = render.Vertex{X: p[0], Y: p[1], U: .5, V: .5, R: 1, G: 1, B: 1, A: 1}
		meshVertices[i] = render.MeshVertex{X: p[0]/32 - 1, Y: p[1]/32 - 1, Z: .5, NZ: 1, R: 1, G: 1, B: 1, A: 1}
	}
	geometry, err := render.NewGeometry(meshVertices, []uint32{0, 1, 2})
	if err != nil {
		t.Fatal(err)
	}
	identity := [16]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
	view := render.View{Projection: identity, Viewport: [4]float32{0, 0, 64, 64}}
	frames := []render.Frame{
		{Vertices: overlay, Commands: []render.Command{{Kind: render.OverlayCommand, Count: 3}}},
		{Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{{Geometry: geometry, Model: identity, Color: [4]float32{1, 1, 1, 1}, Unlit: true}}}}},
	}
	pixels := make([]byte, 64*64*4)
	for i, frame := range frames {
		if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
			t.Fatal(err)
		}
		samples := vk.SampleCount()
		if samples != 1 && samples != 4 {
			t.Fatalf("unexpected scene sample count %d", samples)
		}
		fractional := 0
		for pixel := 0; pixel < len(pixels); pixel += 4 {
			if pixels[pixel] != pixels[pixel+1] || pixels[pixel] != pixels[pixel+2] || pixels[pixel+3] != 255 {
				t.Fatalf("resolve changed neutral color or opaque alpha at byte %d", pixel)
			}
			if pixels[pixel] > 0 && pixels[pixel] < 255 {
				fractional++
			}
		}
		if samples == 4 && fractional < 40 {
			t.Fatalf("pipeline %d has only %d partially covered edge pixels with 4x MSAA", i, fractional)
		}
		if samples == 1 && fractional != 0 {
			t.Fatalf("single-sample fallback unexpectedly blended opaque triangle edges: %d", fractional)
		}
		texturePixel(t, pixels, 64, 20, 20, [4]byte{255, 255, 255, 255})
		texturePixel(t, pixels, 64, 60, 60, [4]byte{0, 0, 0, 255})
		t.Logf("pipeline %d: %dx samples, %d fractionally covered edge pixels", i, samples, fractional)
	}
}

func TestMultisamplingPreservesPixelSharpSurfaceContentThroughResizeGPU(t *testing.T) {
	vk := openTextureGPU(t)
	// A one-texel checkerboard rendered at exactly one texel per pixel detects
	// any resolve-time blur of terminal-sized content inside an opaque surface.
	content := make([]byte, 32*32*4)
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			value := byte(0)
			if (x+y)%2 == 0 {
				value = 255
			}
			offset := (y*32 + x) * 4
			copy(content[offset:offset+4], []byte{value, value, value, 255})
		}
	}
	texture, err := render.NewTexture(32, 32, content)
	if err != nil {
		t.Fatal(err)
	}
	view, model := textureView()
	model[14] = .5
	frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{{Texture: texture, Model: model, Color: [4]float32{1, 1, 1, 1}}}}}}
	width, height := 64, 64
	for iteration := 0; iteration < 2; iteration++ {
		pixels := make([]byte, width*height*4)
		if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
			t.Fatal(err)
		}
		for y := 1; y < 31; y++ {
			for x := 1; x < 31; x++ {
				value := byte(0)
				if (x+y)%2 == 0 {
					value = 255
				}
				texturePixel(t, pixels, width, x+16, y+16, [4]byte{value, value, value, 255})
			}
		}
		if vk.frame.textures[texture.ID()] != texture.Revision() {
			t.Fatal("resolving lost retained texture revision")
		}
		if iteration == 0 {
			count := vk.SampleCount()
			width, height = 96, 80
			if err := vk.Resize(uint32(width), uint32(height)); err != nil {
				t.Fatal(err)
			}
			if vk.SampleCount() != count {
				t.Fatal("target resize changed format-compatible sample count")
			}
		}
	}
}

// MSAA must keep a static overlay stable across frame reuse. Narrow alpha
// fringes at the circle/crosshair intersections previously flickered despite
// identical vertices, which broke application-exit snapshot restoration.
func TestMultisampleRepeatedOverlayCoverageGPU(t *testing.T) {
	vk := openTextureGPU(t)
	if err := vk.Resize(1440, 900); err != nil {
		t.Fatal(err)
	}
	canvas, err := scene.NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Close()
	if err := vk.SetSceneAtlas(canvas.Atlas()); err != nil {
		t.Fatal(err)
	}
	canvas.Reset(1440, 900)
	canvas.Rect(0, 0, 1440, 900, scene.ColorHex(0x101717, 1))
	canvas.Circle(54, 47, 12, 1.6, scene.ColorHex(0xa5d9c6, 1))
	canvas.Line(42, 47, 66, 47, 1.2, scene.ColorHex(0xa5d9c6, 1))
	canvas.Line(54, 35, 54, 59, 1.2, scene.ColorHex(0xa5d9c6, 1))
	frame := canvas.Frame()
	baseline := make([]byte, 1440*900*4)
	pixels := make([]byte, len(baseline))
	for i := 0; i < 64; i++ {
		if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			copy(baseline, pixels)
			continue
		}
		if !bytes.Equal(baseline, pixels) {
			changes := 0
			for k := 0; k < len(pixels); k++ {
				if pixels[k] != baseline[k] {
					changes++
					if changes < 12 {
						t.Logf("%d,%d channel %d %d -> %d", k/4%1440, k/4/1440, k%4, baseline[k], pixels[k])
					}
				}
			}
			t.Fatalf("identical overlay changed on frame %d: %d channels", i, changes)
		}
	}
}
