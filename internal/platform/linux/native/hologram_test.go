//go:build linux && cgo

package native

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func TestHologramIsDepthCompositedAnimatedAndRetainedGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, projection := materialPlane(t)
	projection.Translucent = true
	projection.Unlit = true
	projection.Color = [4]float32{.08, .52, .82, .72}
	baseline := renderMaterial(t, vk, view, projection)
	view.EffectPhase = .73
	projection.Material.RimColor = [3]float32{.08, .8, 1}
	if phasedZero := renderMaterial(t, vk, view, projection); !bytes.Equal(baseline, phasedZero) {
		t.Fatal("effect phase or zero hologram strength changed established translucent pixels")
	}

	projection.Material = render.Material{Hologram: .9, RimColor: [3]float32{.08, .8, 1}}
	view.EffectPhase = 0
	phase0 := renderMaterial(t, vk, view, projection)
	view.EffectPhase = .25
	phase1 := renderMaterial(t, vk, view, projection)
	if bytes.Equal(baseline, phase0) || bytes.Equal(phase0, phase1) {
		t.Fatal("hologram treatment or its bounded phase produced no visible change")
	}
	if len(vk.frame.uploaded) != 1 {
		t.Fatal("animating hologram constants re-uploaded retained geometry")
	}

	// An opaque plane in front must fully hide the projection at the center.
	opaque := projection
	opaque.Translucent, opaque.Unlit, opaque.Material = false, true, render.Material{}
	opaque.Color, opaque.Model[14] = [4]float32{.42, .18, .08, 1}, -.08
	projection.Model[14] = 0
	covered := renderMaterial(t, vk, view, opaque, projection)
	opaqueOnly := renderMaterial(t, vk, view, opaque)
	center := (32*64 + 32) * 4
	if !bytes.Equal(covered[center:center+4], opaqueOnly[center:center+4]) {
		t.Fatalf("opaque depth did not occlude hologram: covered=%v opaque=%v", covered[center:center+4], opaqueOnly[center:center+4])
	}

	// Moving the projection just in front exposes it and activates the narrow
	// opaque-depth contact band without changing either retained resource.
	projection.Model[14] = -.081
	contact := renderMaterial(t, vk, view, opaque, projection)
	projection.Model[14] = -.18
	distant := renderMaterial(t, vk, view, opaque, projection)
	if bytes.Equal(contact[center:center+4], opaqueOnly[center:center+4]) || pixelLumaBGRA(contact, 32, 32) <= pixelLumaBGRA(distant, 32, 32) {
		t.Fatalf("hologram depth contact was absent: contact=%v distant=%v", contact[center:center+4], distant[center:center+4])
	}
}

func TestHologramCannotAlterLaterOpaqueOverlayPixelsGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, projection := materialPlane(t)
	projection.Translucent, projection.Unlit = true, true
	projection.Color = [4]float32{.1, .7, 1, .8}
	projection.Material = render.Material{Hologram: 1, RimColor: [3]float32{.05, .8, 1}}
	overlay, err := render.NewTexture(1, 1, []byte{37, 91, 143, 255})
	if err != nil {
		t.Fatal(err)
	}
	frame := render.Frame{Commands: []render.Command{
		{Kind: render.SceneCommand, View: view, Draws: []render.Draw{projection}},
		{Kind: render.ImageCommand, Image: render.Image{Texture: overlay, Bounds: [4]float32{20, 20, 24, 24}}},
	}}
	pixels := renderCinematic(t, vk, frame, [4]float32{0, 0, 0, 1})
	texturePixel(t, pixels, 64, 32, 32, [4]byte{37, 91, 143, 255})
	if pixelLumaBGRA(pixels, 16, 32) == 0 {
		t.Fatal("fixture did not render hologram outside the overlay")
	}
}

func TestHologramFrameBoundaryRejectsInvalidUseBeforeUploadGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, draw := materialPlane(t)
	draw.Material.Hologram = .5
	frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{draw}}}}
	if err := vk.RenderFrame(frame, [4]float32{}, nil); err == nil || !strings.Contains(err.Error(), "translucent mesh") {
		t.Fatalf("accepted hologram on an opaque mesh: %v", err)
	}
	draw.Translucent = true
	frame.Commands[0].Draws[0] = draw
	for _, phase := range []float32{-.01, 1.01, float32(math.NaN()), float32(math.Inf(1))} {
		frame.Commands[0].View.EffectPhase = phase
		if err := vk.RenderFrame(frame, [4]float32{}, nil); err == nil {
			t.Fatalf("accepted invalid native-effect phase %v", phase)
		}
	}
	if vk.frame != nil && len(vk.frame.uploaded) != 0 {
		t.Fatal("invalid hologram settings uploaded geometry")
	}
}
