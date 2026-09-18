//go:build linux && cgo

package native

import (
	"image"
	"os"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func openTextureGPU(t *testing.T) *VK {
	t.Helper()
	vk, err := OpenVK(false, 64, 64)
	if err != nil {
		if os.Getenv("WORLDR_TEST_GPU") == "1" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	t.Cleanup(vk.Close)
	return vk
}

func textureSolid(width, height int, rgba [4]byte) []byte {
	pixels := make([]byte, width*height*4)
	for i := 0; i < len(pixels); i += 4 {
		copy(pixels[i:i+4], rgba[:])
	}
	return pixels
}

func texturePixel(t *testing.T, pixels []byte, width, x, y int, rgba [4]byte) {
	t.Helper()
	got := pixels[(y*width+x)*4:][:4]
	want := [4]byte{rgba[2], rgba[1], rgba[0], rgba[3]}
	for i := range want {
		if delta := int(got[i]) - int(want[i]); delta < -2 || delta > 2 {
			t.Fatalf("pixel (%d,%d) BGRA=%v; want %v", x, y, got, want)
		}
	}
}

func textureView() (render.View, [16]float32) {
	identity := [16]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
	projection := identity
	projection[5] = -1 // World Y up; Vulkan framebuffer Y down.
	return render.View{Projection: projection, Viewport: [4]float32{0, 0, 64, 64}}, identity
}

func TestTextureContentUpdatesGPU(t *testing.T) {
	vk := openTextureGPU(t)
	red, green := [4]byte{255, 0, 0, 255}, [4]byte{0, 255, 0, 255}
	blue, yellow := [4]byte{0, 0, 255, 255}, [4]byte{255, 255, 0, 255}
	magenta, cyan := [4]byte{255, 0, 255, 255}, [4]byte{0, 255, 255, 255}
	pixels := make([]byte, 4*4*4)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			color := [][4]byte{red, green, blue, yellow}[y/2*2+x/2]
			color[3] = 0 // Surface coverage is opaque regardless of input alpha.
			copy(pixels[(y*4+x)*4:][:4], color[:])
		}
	}
	texture, err := render.NewTexture(4, 4, pixels)
	if err != nil {
		t.Fatal(err)
	}
	view, model := textureView()
	model[14] = .5
	frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{{Texture: texture, Model: model, Color: [4]float32{1, 1, 1, 0}}}}}}
	dst := make([]byte, 64*64*4)
	draw := func() {
		t.Helper()
		if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, dst); err != nil {
			t.Fatal(err)
		}
	}
	draw()
	texturePixel(t, dst, 64, 20, 20, red)
	texturePixel(t, dst, 64, 44, 20, green)
	texturePixel(t, dst, 64, 20, 44, blue)
	texturePixel(t, dst, 64, 44, 44, yellow)
	if vk.frame.textures[texture.ID()] != texture.Revision() {
		t.Fatal("texture revision was not retained")
	}
	if err := texture.Update(image.Rect(0, 0, 2, 2), textureSolid(2, 2, magenta)); err != nil {
		t.Fatal(err)
	}
	draw()
	texturePixel(t, dst, 64, 20, 20, magenta)
	texturePixel(t, dst, 64, 44, 20, green)
	texturePixel(t, dst, 64, 20, 44, blue)
	texturePixel(t, dst, 64, 44, 44, yellow)
	// Two updates before the next frame require a full fallback, preserving both.
	if err := texture.Update(image.Rect(2, 0, 4, 2), textureSolid(2, 2, cyan)); err != nil {
		t.Fatal(err)
	}
	if err := texture.Update(image.Rect(0, 2, 2, 4), textureSolid(2, 2, green)); err != nil {
		t.Fatal(err)
	}
	draw()
	texturePixel(t, dst, 64, 44, 20, cyan)
	texturePixel(t, dst, 64, 20, 44, green)
	if err := vk.ReleaseTexture(texture.ID()); err != nil {
		t.Fatal(err)
	}
	if len(vk.frame.textures) != 0 {
		t.Fatal("released texture remains cached")
	}
	draw()
	texturePixel(t, dst, 64, 20, 20, magenta)
	texturePixel(t, dst, 64, 44, 20, cyan)
	texturePixel(t, dst, 64, 20, 44, green)
	texturePixel(t, dst, 64, 44, 44, yellow)
	// Replacing the image can resize its storage without changing its ID.
	if err := texture.Replace(8, 2, textureSolid(8, 2, yellow)); err != nil {
		t.Fatal(err)
	}
	draw()
	texturePixel(t, dst, 64, 32, 32, yellow)
	// A new snapshot device must receive the full current image independently.
	snapshot := openTextureGPU(t)
	if err := snapshot.RenderFrame(frame, [4]float32{0, 0, 0, 1}, dst); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, dst, 64, 32, 32, yellow)
	// Target recreation preserves content resources and their revision cache.
	revision := vk.frame.textures[texture.ID()]
	if err := vk.Resize(80, 48); err != nil {
		t.Fatal(err)
	}
	dst = make([]byte, 80*48*4)
	frame.Commands[0].View.Viewport = [4]float32{0, 0, 80, 48}
	draw()
	texturePixel(t, dst, 80, 40, 24, yellow)
	if vk.frame.textures[texture.ID()] != revision {
		t.Fatal("target resize changed the texture revision cache")
	}
}

func TestTextureMeshDepthAndTransformGPU(t *testing.T) {
	vk := openTextureGPU(t)
	red, cyan, black := [4]byte{255, 0, 0, 255}, [4]byte{0, 255, 255, 255}, [4]byte{0, 0, 0, 255}
	texture, err := render.NewTexture(1, 1, red[:])
	if err != nil {
		t.Fatal(err)
	}
	geometry, err := render.NewGeometry([]render.MeshVertex{
		{X: -.4, Y: -.4, NZ: 1, R: 1, G: 1, B: 1, A: 1, BX: 1},
		{X: .4, Y: -.4, NZ: 1, R: 1, G: 1, B: 1, A: 1, BY: 1},
		{Y: .4, NZ: 1, R: 1, G: 1, B: 1, A: 1, BZ: 1},
	}, []uint32{0, 1, 2})
	if err != nil {
		t.Fatal(err)
	}
	view, model := textureView()
	model[14] = .5
	surface := render.Draw{Texture: texture, Model: model, Color: [4]float32{1, 1, 1, 1}}
	mesh := render.Draw{Geometry: geometry, Model: model, Color: [4]float32{0, 1, 1, 1}, Unlit: true}
	dst := make([]byte, 64*64*4)
	for _, meshDepth := range []float32{.25, .75} {
		mesh.Model[14] = meshDepth
		for _, draws := range [][]render.Draw{{surface, mesh}, {mesh, surface}} {
			frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: draws}}}
			if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, dst); err != nil {
				t.Fatal(err)
			}
			want := red
			if meshDepth < .5 {
				want = cyan
			}
			texturePixel(t, dst, 64, 32, 32, want)
			texturePixel(t, dst, 64, 20, 20, red)
		}
	}
	surface.Model[12] = .5
	frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{surface}}}}
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, dst); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, dst, 64, 20, 20, black)
	texturePixel(t, dst, 64, 48, 32, red)
	frame.Commands[0].Draws[0].Geometry = geometry
	if err := vk.RenderFrame(frame, [4]float32{}, nil); err == nil {
		t.Fatal("accepted an instance containing both texture and geometry")
	}
}
