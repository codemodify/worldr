//go:build linux && cgo

package native

import (
	"bytes"
	"math"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func shadowPlanes(t *testing.T) (render.View, render.Draw, render.Draw) {
	t.Helper()
	view, model := textureView()
	texture, err := render.NewTexture(1, 1, []byte{255, 255, 255, 255})
	if err != nil {
		t.Fatal(err)
	}
	model[0], model[5], model[14] = 1.8, 1.8, .7
	receiver := render.Draw{Texture: texture, Model: model, Color: [4]float32{1, 1, 1, 1}, ReceiveShadow: true}
	caster := receiver
	caster.Model[0], caster.Model[5], caster.Model[12], caster.Model[14] = .25, .25, .5, .3
	caster.ReceiveShadow, caster.CastShadow = false, true
	projection := view.Projection
	projection[8], projection[12] = 1.25, -.7
	view.Shadow = render.Shadow{Projection: projection, Strength: .8, Bias: .001}
	return view, receiver, caster
}

func TestDirectionalShadowMovesWithCasterAndDoesNotChangeUnmarkedContentGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, receiver, caster := shadowPlanes(t)
	initial := renderMaterial(t, vk, view, receiver, caster)
	texturePixel(t, initial, 64, 32, 32, [4]byte{51, 51, 51, 255})
	texturePixel(t, initial, 64, 48, 32, [4]byte{255, 255, 255, 255})
	caster.Model[12] = -.5
	moved := renderMaterial(t, vk, view, receiver, caster)
	texturePixel(t, moved, 64, 32, 32, [4]byte{255, 255, 255, 255})
	caster.Model[12] = .5
	caster.CastShadow = false
	texturePixel(t, renderMaterial(t, vk, view, receiver, caster), 64, 32, 32, [4]byte{255, 255, 255, 255})
	caster.CastShadow = true
	receiver.ReceiveShadow = false
	unmarked := renderMaterial(t, vk, view, receiver, caster)
	view.Shadow.Strength = 0
	if !bytes.Equal(unmarked, renderMaterial(t, vk, view, receiver, caster)) {
		t.Fatal("shadow pass altered content that did not opt in")
	}
	if len(vk.frame.textures) != 1 {
		t.Fatal("shadow movement re-uploaded app texture")
	}
}

func TestDirectionalShadowLinearColorAndResizeGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, receiver, caster := shadowPlanes(t)
	frame := render.Frame{LinearColor: true, Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{receiver, caster}}}}
	pixels := make([]byte, 64*64*4)
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	expected := linearByte(.2)
	texturePixel(t, pixels, 64, 32, 32, [4]byte{expected, expected, expected, 255})
	if err := vk.Resize(80, 72); err != nil {
		t.Fatal(err)
	}
	pixels = make([]byte, 80*72*4)
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, pixels, 80, 32, 32, [4]byte{expected, expected, expected, 255})
}

func TestDirectionalShadowValidationIsBoundedGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, receiver, caster := shadowPlanes(t)
	for _, bad := range []float32{-1, 1.1, float32(math.NaN()), float32(math.Inf(1))} {
		invalid := view
		invalid.Shadow.Strength = bad
		frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: invalid, Draws: []render.Draw{receiver, caster}}}}
		if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, nil); err == nil {
			t.Fatalf("accepted invalid shadow strength %v", bad)
		}
	}
	frame := render.Frame{}
	for range 5 {
		frame.Commands = append(frame.Commands, render.Command{Kind: render.SceneCommand, View: view, Draws: []render.Draw{receiver, caster}})
	}
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, nil); err == nil {
		t.Fatal("accepted more than four shadowed cameras")
	}
	caster.Translucent = true
	frame.Commands = frame.Commands[:1]
	frame.Commands[0].Draws = []render.Draw{caster}
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, nil); err == nil {
		t.Fatal("accepted unsupported translucent shadow casting")
	}
	// Rejected inputs do not poison a later valid frame.
	caster.Translucent = false
	renderMaterial(t, vk, view, receiver, caster)
}

func TestDirectionalShadowsComposeWithTranslucencyAndIndependentCamerasGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, receiver, caster := shadowPlanes(t)
	blue, err := render.NewTexture(1, 1, []byte{0, 0, 128, 128})
	if err != nil {
		t.Fatal(err)
	}
	panel := receiver
	panel.Texture = blue
	panel.Model[14] = .2
	panel.Translucent = true
	panel.ReceiveShadow = false
	view.Viewport[2] = 32
	other := view
	other.Viewport[0] = 32
	moved := caster
	moved.Model[12] = -.5
	frame := render.Frame{LinearColor: true, Commands: []render.Command{
		{Kind: render.SceneCommand, View: view, Draws: []render.Draw{panel, receiver, caster}},
		{Kind: render.SceneCommand, View: other, Draws: []render.Draw{receiver, moved, panel}},
	}}
	pixels := make([]byte, 64*64*4)
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, pixels, 64, 16, 32, [4]byte{89, 89, 203, 255})
	texturePixel(t, pixels, 64, 48, 32, [4]byte{187, 187, 255, 255})
	// Resource slots survive an intermediate non-shadow frame without a stale
	// descriptor crossing between camera scopes when the shadowed views return.
	opaque := frame
	opaque.Commands = nil
	if err := vk.RenderFrame(opaque, [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, pixels, 64, 16, 32, [4]byte{89, 89, 203, 255})
	texturePixel(t, pixels, 64, 48, 32, [4]byte{187, 187, 255, 255})
}
