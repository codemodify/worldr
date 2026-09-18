//go:build linux && cgo

package native

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func renderCinematic(t *testing.T, vk *VK, frame render.Frame, clear [4]float32) []byte {
	t.Helper()
	pixels := make([]byte, 64*64*4)
	if err := vk.RenderFrame(frame, clear, pixels); err != nil {
		t.Fatal(err)
	}
	return pixels
}

func pixelLumaBGRA(pixels []byte, x, y int) int {
	offset := (y*64 + x) * 4
	return int(pixels[offset]) + int(pixels[offset+1]) + int(pixels[offset+2])
}

func TestSDROutputTransformZeroIsExactAndGradeIsBoundedGPU(t *testing.T) {
	vk := openTextureGPU(t)
	frame := render.Frame{LinearColor: true}
	baseline := renderCinematic(t, vk, frame, [4]float32{.35, .5, .8, 1})
	if again := renderCinematic(t, vk, frame, [4]float32{.35, .5, .8, 1}); !bytes.Equal(baseline, again) {
		t.Fatal("zero SDR output transform changed the established linear-sRGB result")
	}
	frame.Output = render.OutputTransform{Exposure: 1, Saturation: .2, Contrast: .15, ToneMap: render.ToneMapFilmic}
	graded := renderCinematic(t, vk, frame, [4]float32{.35, .5, .8, 1})
	if bytes.Equal(baseline, graded) {
		t.Fatal("active SDR grade did not change output")
	}
	for i := 0; i < len(graded); i += 4 {
		if graded[i+3] != 255 {
			t.Fatal("SDR grade changed opaque framebuffer alpha")
		}
	}
}

func TestHighlightBloomSpillsFromFinalComposedFrameGPU(t *testing.T) {
	vk := openTextureGPU(t)
	texture, err := render.NewTexture(1, 1, []byte{255, 245, 225, 255})
	if err != nil {
		t.Fatal(err)
	}
	frame := render.Frame{LinearColor: true, Commands: []render.Command{{
		Kind:  render.ImageCommand,
		Image: render.Image{Texture: texture, Bounds: [4]float32{28, 28, 8, 8}},
	}}}
	baseline := renderCinematic(t, vk, frame, [4]float32{0, 0, 0, 1})
	frame.Output = render.OutputTransform{BloomStrength: 1.1, BloomThreshold: .72, BloomRadius: 9, ToneMap: render.ToneMapFilmic}
	bloomed := renderCinematic(t, vk, frame, [4]float32{0, 0, 0, 1})
	if pixelLumaBGRA(baseline, 23, 32) != 0 || pixelLumaBGRA(bloomed, 23, 32) == 0 {
		t.Fatalf("highlight bloom did not leave the source footprint: baseline=%d bloom=%d", pixelLumaBGRA(baseline, 23, 32), pixelLumaBGRA(bloomed, 23, 32))
	}
	if pixelLumaBGRA(bloomed, 2, 2) != 0 {
		t.Fatal("bounded bloom reached unrelated pixels")
	}
}

func TestPointFillLightsFollowWorldPositionAndRemainViewLocalGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, draw := materialPlane(t)
	view.Light = [3]float32{0, 0, -1}
	draw.Material = render.Material{Specular: .7, Roughness: .25}
	dark := renderMaterial(t, vk, view, draw)
	view.PointLights[0], view.PointLightCount = render.PointLight{Position: [3]float32{-.55, 0, 1}, Color: [3]float32{0, .7, 1}, Intensity: 3, Radius: 2}, 1
	left := renderMaterial(t, vk, view, draw)
	view.PointLights[0].Position[0] = .55
	right := renderMaterial(t, vk, view, draw)
	if pixelLumaBGRA(left, 18, 32) <= pixelLumaBGRA(dark, 18, 32)+20 || pixelLumaBGRA(right, 46, 32) <= pixelLumaBGRA(dark, 46, 32)+20 {
		t.Fatal("point light produced no visible local fill")
	}
	if pixelLumaBGRA(left, 18, 32) <= pixelLumaBGRA(left, 46, 32) || pixelLumaBGRA(right, 46, 32) <= pixelLumaBGRA(right, 18, 32) {
		t.Fatal("point fill did not follow its world-space position")
	}
	if len(vk.frame.uploaded) != 1 {
		t.Fatal("moving a point light re-uploaded retained geometry")
	}
}

func TestThinGlassTransmissionUsesDepthPeeledMaterialGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, glass := materialPlane(t)
	glass.Color = [4]float32{.12, .65, .9, .32}
	glass.Translucent = true
	glass.Material = render.Material{Specular: .9, Roughness: .16, RimStrength: .5, RimColor: [3]float32{.3, .9, 1}}
	solidBody := renderMaterial(t, vk, view, glass)
	glass.Material.Transmission = .9
	transmitting := renderMaterial(t, vk, view, glass)
	if bytes.Equal(solidBody, transmitting) || pixelLumaBGRA(transmitting, 32, 32) >= pixelLumaBGRA(solidBody, 32, 32) {
		t.Fatal("glass transmission did not reduce diffuse body color")
	}
}

func TestThinGlassRefractionBendsAndFrostsOpaqueBackdropGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, model := textureView()
	view.Eye, view.Light = [3]float32{0, 0, 1.5}, [3]float32{0, 0, 1}
	model[0], model[5], model[14] = 1.9, 1.9, .7
	backgroundPixels := make([]byte, 16*4)
	for x := 0; x < 16; x++ {
		color := [4]byte{255, 0, 0, 255}
		if x >= 8 {
			color = [4]byte{0, 0, 255, 255}
		}
		copy(backgroundPixels[x*4:], color[:])
	}
	background, err := render.NewTexture(16, 1, backgroundPixels)
	if err != nil {
		t.Fatal(err)
	}
	backdrop := render.Draw{Texture: background, Model: model, Color: [4]float32{1, 1, 1, 1}}
	vertices := []render.MeshVertex{
		{X: -.95, Y: -.95, Z: .2, NX: .70710677, NZ: .70710677, R: 1, G: 1, B: 1, A: 1},
		{X: .95, Y: -.95, Z: .2, NX: .70710677, NZ: .70710677, R: 1, G: 1, B: 1, A: 1},
		{X: .95, Y: .95, Z: .2, NX: .70710677, NZ: .70710677, R: 1, G: 1, B: 1, A: 1},
		{X: -.95, Y: .95, Z: .2, NX: .70710677, NZ: .70710677, R: 1, G: 1, B: 1, A: 1},
	}
	geometry, err := render.NewGeometry(vertices, []uint32{0, 1, 2, 0, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	glass := render.Draw{
		Geometry: geometry, Model: [16]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1},
		Color: [4]float32{.18, .55, .7, 1}, Translucent: true,
		Material: render.Material{Transmission: 1},
	}
	baseline := renderMaterial(t, vk, view, backdrop, glass)
	glass.Material.Refraction = 1
	refracted := renderMaterial(t, vk, view, backdrop, glass)
	// The tilted smooth normal bends a point just left of the red/blue divide
	// into the blue half while a more distant point remains red.
	left := refracted[(32*64+18)*4:][:4]
	bent := refracted[(32*64+28)*4:][:4]
	if int(left[2])-int(left[0]) < 120 || int(bent[0])-int(bent[2]) < 30 {
		t.Fatalf("glass did not bend the opaque backdrop: left BGRA=%v bent BGRA=%v", left, bent)
	}
	glass.Material.RefractionBlur = 1
	frosted := renderMaterial(t, vk, view, backdrop, glass)
	soft := frosted[(32*64+23)*4:][:4]
	sharp := refracted[(32*64+23)*4:][:4]
	if bytes.Equal(frosted, refracted) || soft[0] <= sharp[0]+15 || soft[2] >= sharp[2]-15 {
		t.Fatalf("frosted refraction did not soften the color boundary: sharp=%v frosted=%v", sharp, soft)
	}
	glass.Material.Refraction, glass.Material.RefractionBlur = 0, 0
	if zero := renderMaterial(t, vk, view, backdrop, glass); !bytes.Equal(zero, baseline) {
		t.Fatal("zero refraction did not restore exact thin-glass pixels after enabled rendering")
	}
	if len(vk.frame.uploaded) != 1 || len(vk.frame.textures) != 1 {
		t.Fatal("changing glass optics recreated retained resources")
	}
}

func TestCinematicFrameBoundaryRejectsInvalidSettingsBeforeUploadGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, draw := materialPlane(t)
	tests := []render.Frame{
		{Output: render.OutputTransform{Exposure: 1}},
		{LinearColor: true, Output: render.OutputTransform{BloomStrength: 2.1}},
		{LinearColor: true, Output: render.OutputTransform{Exposure: float32(math.NaN())}},
		{Commands: []render.Command{{Kind: render.SceneCommand, View: render.View{Projection: view.Projection, Viewport: view.Viewport, PointLightCount: 5}, Draws: []render.Draw{draw}}}},
	}
	for i, frame := range tests {
		if err := vk.RenderFrame(frame, [4]float32{}, nil); err == nil {
			t.Fatalf("accepted invalid cinematic frame %d", i)
		}
	}
	draw.Material.Transmission = .5
	frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{draw}}}}
	if err := vk.RenderFrame(frame, [4]float32{}, nil); err == nil || !strings.Contains(err.Error(), "translucent mesh") {
		t.Fatalf("accepted transmission on opaque mesh: %v", err)
	}
	draw.Translucent = true
	draw.Material.Refraction = .5
	draw.Material.Transmission = 0
	frame.Commands[0].Draws[0] = draw
	if err := vk.RenderFrame(frame, [4]float32{}, nil); err == nil || !strings.Contains(err.Error(), "transmitted translucent mesh") {
		t.Fatalf("accepted refraction without transmission: %v", err)
	}
	draw.Material.Transmission = .8
	draw.Material.Refraction = 0
	draw.Material.RefractionBlur = .3
	frame.Commands[0].Draws[0] = draw
	if err := vk.RenderFrame(frame, [4]float32{}, nil); err == nil || !strings.Contains(err.Error(), "blur requires refraction") {
		t.Fatalf("accepted frosted blur without refraction: %v", err)
	}
	if len(vk.frame.uploaded) != 0 {
		t.Fatal("invalid cinematic settings uploaded geometry")
	}
}
