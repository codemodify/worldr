//go:build linux && cgo

package native

import (
	"bytes"
	"image"
	"math"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func TestImageOverlayPremultipliedAlphaOrientationAndRetentionGPU(t *testing.T) {
	vk := openTextureGPU(t)
	pixels := make([]byte, 4*4*4)
	colors := [][4]byte{{240, 20, 10, 255}, {0, 128, 0, 128}, {0, 0, 64, 64}, {0, 0, 0, 0}}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			copy(pixels[(y*4+x)*4:][:4], colors[y/2*2+x/2][:])
		}
	}
	texture, err := render.NewTexture(4, 4, pixels)
	if err != nil {
		t.Fatal(err)
	}
	frame := render.Frame{Commands: []render.Command{{Kind: render.ImageCommand, Image: render.Image{Texture: texture, Bounds: [4]float32{16, 16, 32, 32}}}}}
	clearColor := [4]float32{20.0 / 255, 40.0 / 255, 80.0 / 255, 1}
	dst := make([]byte, 64*64*4)
	draw := func() {
		t.Helper()
		if err := vk.RenderFrame(frame, clearColor, dst); err != nil {
			t.Fatal(err)
		}
	}
	draw()
	texturePixel(t, dst, 64, 20, 20, colors[0])
	texturePixel(t, dst, 64, 44, 20, [4]byte{10, 148, 40, 255})
	texturePixel(t, dst, 64, 20, 44, [4]byte{15, 30, 124, 255})
	texturePixel(t, dst, 64, 44, 44, [4]byte{20, 40, 80, 255})
	texturePixel(t, dst, 64, 8, 8, [4]byte{20, 40, 80, 255})
	baseline := append([]byte(nil), dst...)
	for i := 0; i < 32; i++ {
		draw()
		if !bytes.Equal(dst, baseline) {
			t.Fatalf("unchanged image overlay changed at frame %d", i)
		}
	}
	if len(vk.frame.textures) != 1 || vk.frame.textures[texture.ID()] != texture.Revision() {
		t.Fatal("overlay image was not retained")
	}
	if err := texture.Update(image.Rect(2, 2, 4, 4), textureSolid(2, 2, [4]byte{80, 0, 80, 128})); err != nil {
		t.Fatal(err)
	}
	draw()
	texturePixel(t, dst, 64, 44, 44, [4]byte{90, 20, 120, 255})
	texturePixel(t, dst, 64, 20, 20, colors[0])
	if err := vk.Resize(80, 48); err != nil {
		t.Fatal(err)
	}
	dst = make([]byte, 80*48*4)
	draw()
	texturePixel(t, dst, 80, 44, 44, [4]byte{90, 20, 120, 255})
	texturePixel(t, dst, 80, 20, 20, colors[0])
	if err := vk.ReleaseTexture(texture.ID()); err != nil {
		t.Fatal(err)
	}
	if len(vk.frame.textures) != 0 {
		t.Fatal("released overlay image still cached")
	}
	draw()
	texturePixel(t, dst, 80, 44, 44, [4]byte{90, 20, 120, 255})
}

func TestImageOverlayOrderingClippingAndOpaqueContentGPU(t *testing.T) {
	vk := openTextureGPU(t)
	texture, err := render.NewTexture(1, 1, []byte{100, 20, 0, 128})
	if err != nil {
		t.Fatal(err)
	}
	view, model := textureView()
	view.Viewport = [4]float32{16, 16, 32, 32}
	model[0], model[5], model[14] = 2, 2, .1
	content := render.Command{Kind: render.SceneCommand, View: view, Draws: []render.Draw{{Texture: texture, Model: model, Color: [4]float32{1, 1, 1, 1}}}}
	imageCommand := render.Command{Kind: render.ImageCommand, Image: render.Image{Texture: texture, Bounds: [4]float32{-8, 24, 40, 24}}}
	frame := render.Frame{Commands: []render.Command{content, imageCommand}}
	dst := make([]byte, 64*64*4)
	clearColor := [4]float32{0, 0, 80.0 / 255, 1}
	draw := func() {
		t.Helper()
		if err := vk.RenderFrame(frame, clearColor, dst); err != nil {
			t.Fatal(err)
		}
	}
	draw()
	// The image restores a full framebuffer viewport after the inset camera.
	texturePixel(t, dst, 64, 4, 32, [4]byte{100, 20, 40, 255})
	texturePixel(t, dst, 64, 20, 32, [4]byte{150, 30, 0, 255})
	// The same texture keeps the scene's existing opaque-alpha contract.
	texturePixel(t, dst, 64, 40, 32, [4]byte{100, 20, 0, 255})
	texturePixel(t, dst, 64, 20, 20, [4]byte{100, 20, 0, 255})
	texturePixel(t, dst, 64, 4, 20, [4]byte{0, 0, 80, 255})
	frame.Commands = []render.Command{imageCommand, content}
	draw()
	texturePixel(t, dst, 64, 20, 32, [4]byte{100, 20, 0, 255})
	texturePixel(t, dst, 64, 4, 32, [4]byte{100, 20, 40, 255})
}

func TestImageOverlayRejectsInvalidBoundsGPU(t *testing.T) {
	vk := openTextureGPU(t)
	texture, err := render.NewTexture(1, 1, []byte{255, 255, 255, 255})
	if err != nil {
		t.Fatal(err)
	}
	invalid := [][4]float32{{0, 0, 0, 1}, {0, 0, 1, -1}, {0, 0, float32(math.Inf(1)), 1}, {0, float32(math.NaN()), 1, 1}, {math.MaxFloat32, 0, math.MaxFloat32, 1}}
	for _, bounds := range invalid {
		frame := render.Frame{Commands: []render.Command{{Kind: render.ImageCommand, Image: render.Image{Texture: texture, Bounds: bounds}}}}
		if err := vk.RenderFrame(frame, [4]float32{}, nil); err == nil {
			t.Fatalf("accepted invalid image bounds %v", bounds)
		}
		if len(vk.frame.textures) != 0 {
			t.Fatal("invalid image uploaded before validation")
		}
	}
	if err := vk.RenderFrame(render.Frame{Commands: []render.Command{{Kind: render.ImageCommand, Image: render.Image{Bounds: [4]float32{0, 0, 1, 1}}}}}, [4]float32{}, nil); err == nil {
		t.Fatal("accepted missing overlay image")
	}
}
