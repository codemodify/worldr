//go:build linux && cgo

package native

import (
	"bytes"
	"math"
	"testing"

	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

func linearByte(value float64) byte {
	if value <= .0031308 {
		value *= 12.92
	} else {
		value = 1.055*math.Pow(value, 1/2.4) - .055
	}
	return byte(math.Round(max(0, min(1, value)) * 255))
}

func TestLinearColorBlendAndLegacyModeGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, back := materialPlane(t)
	back.Unlit = true
	back.Color = [4]float32{0, 0, 0, 1}
	front := back
	front.Color = [4]float32{1, 1, 1, .5}
	front.Translucent = true
	front.Model[14] = -.2
	frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{front, back}}}}
	pixels := make([]byte, 64*64*4)
	for _, linear := range []bool{false, true, true, false, true} {
		frame.LinearColor = linear
		if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
			t.Fatal(err)
		}
		want := byte(128)
		if linear {
			want = 188
		}
		texturePixel(t, pixels, 64, 32, 32, [4]byte{want, want, want, 255})
	}
	if len(vk.frame.uploaded) != 1 {
		t.Fatal("color mode changes discarded retained geometry")
	}
}

func TestLinearColorOpaquePixelsClearAndTextStayEncodedGPU(t *testing.T) {
	vk := openTextureGPU(t)
	canvas, err := scene.NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Close()
	if err := vk.SetSceneAtlas(canvas.Atlas()); err != nil {
		t.Fatal(err)
	}
	canvas.Reset(64, 64)
	canvas.SetLinearColor(true)
	canvas.Rect(0, 0, 32, 32, scene.ColorHex(0x4080c0, 1))
	texture, err := render.NewTexture(1, 1, []byte{27, 125, 229, 0})
	if err != nil {
		t.Fatal(err)
	}
	view, model := textureView()
	model[12], model[13], model[14] = .5, -.5, .5
	frame := canvas.Frame()
	frame.Commands = append(frame.Commands, render.Command{Kind: render.SceneCommand, View: view, Draws: []render.Draw{{Texture: texture, Model: model, Color: [4]float32{1, 1, 1, 1}}}})
	pixels := make([]byte, 64*64*4)
	if err := vk.RenderFrame(frame, [4]float32{.25, .5, .75, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, pixels, 64, 10, 10, [4]byte{64, 128, 192, 255})
	texturePixel(t, pixels, 64, 50, 50, [4]byte{27, 125, 229, 255})
	texturePixel(t, pixels, 64, 50, 10, [4]byte{64, 128, 191, 255})
	// Repeated resize keeps mode, atlas, mesh and image resource contracts intact.
	if err := vk.Resize(80, 72); err != nil {
		t.Fatal(err)
	}
	pixels = make([]byte, 80*72*4)
	if err := vk.RenderFrame(frame, [4]float32{.25, .5, .75, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, pixels, 80, 50, 50, [4]byte{27, 125, 229, 255})
}

func TestLinearColorTextureFilteringAndPremultipliedImageGPU(t *testing.T) {
	vk := openTextureGPU(t)
	texture, err := render.NewTexture(2, 1, []byte{0, 0, 0, 255, 255, 255, 255, 255})
	if err != nil {
		t.Fatal(err)
	}
	view, model := textureView()
	model[0], model[5], model[12], model[14] = 2, 2, 1.0/64, .5
	frame := render.Frame{LinearColor: true, Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{{Texture: texture, Model: model, Color: [4]float32{1, 1, 1, 1}}}}}}
	pixels := make([]byte, 64*64*4)
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, pixels, 64, 32, 32, [4]byte{188, 188, 188, 255}) // Linear midpoint, not gamma-space128.
	premult, err := render.NewTexture(1, 1, []byte{128, 64, 0, 128})
	if err != nil {
		t.Fatal(err)
	}
	frame.Commands = []render.Command{{Kind: render.ImageCommand, Image: render.Image{Texture: premult, Bounds: [4]float32{0, 0, 64, 64}}}}
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	wantGreen := linearByte(math.Pow((.5+.055)/1.055, 2.4) * 128 / 255)
	texturePixel(t, pixels, 64, 32, 32, [4]byte{188, wantGreen, 0, 255})
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 0}, pixels); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, pixels, 64, 32, 32, [4]byte{128, 64, 0, 128}) // Encoded premultiplication survives transparent readback.

	// Transparent input contributes no hidden RGB in the linear association path.
	empty, err := render.NewTexture(1, 1, []byte{0, 0, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	frame.Commands[0].Image.Texture = empty
	if err := vk.RenderFrame(frame, [4]float32{.25, .5, .75, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	frame.Commands = nil
	base := make([]byte, len(pixels))
	if err := vk.RenderFrame(frame, [4]float32{.25, .5, .75, 1}, base); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pixels, base) {
		t.Fatal("transparent image altered encoded clear pixels")
	}
	if err := vk.RenderFrame(frame, [4]float32{.5, .25, .75, .5}, pixels); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, pixels, 64, 32, 32, [4]byte{64, 32, 96, 128})
}
