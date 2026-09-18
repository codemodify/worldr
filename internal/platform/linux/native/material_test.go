//go:build linux && cgo

package native

import (
	"bytes"
	"math"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func materialPlane(t *testing.T) (render.View, render.Draw) {
	t.Helper()
	geometry, err := render.NewGeometry([]render.MeshVertex{
		{X: -.95, Y: -.95, Z: .5, NZ: 1, R: 1, G: 1, B: 1, A: 1},
		{X: .95, Y: -.95, Z: .5, NZ: 1, R: 1, G: 1, B: 1, A: 1},
		{X: .95, Y: .95, Z: .5, NZ: 1, R: 1, G: 1, B: 1, A: 1},
		{X: -.95, Y: .95, Z: .5, NZ: 1, R: 1, G: 1, B: 1, A: 1},
	}, []uint32{0, 1, 2, 0, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	view, model := textureView()
	view.Eye, view.Light = [3]float32{0, 0, 1.5}, [3]float32{0, 0, 1}
	return view, render.Draw{Geometry: geometry, Model: model, Color: [4]float32{.28, .36, .48, 1}}
}

func renderMaterial(t *testing.T, vk *VK, view render.View, draws ...render.Draw) []byte {
	t.Helper()
	pixels := make([]byte, 64*64*4)
	frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: draws}}}
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	return pixels
}

func TestMaterialSpecularFollowsCameraAndLightGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, draw := materialPlane(t)
	draw.Material = render.Material{Specular: 1, Roughness: .12, Metallic: 1}
	peak := func(pixels []byte) int {
		x, maximum := 0, byte(0)
		for i := 5; i < 59; i++ {
			if value := pixels[(32*64+i)*4+2]; value > maximum {
				x, maximum = i, value
			}
		}
		if maximum < 110 {
			t.Fatalf("no visible specular highlight: peak red=%d", maximum)
		}
		return x
	}
	center := peak(renderMaterial(t, vk, view, draw))
	view.Eye[0] = -.4
	left := peak(renderMaterial(t, vk, view, draw))
	view.Eye[0], view.Light[0] = 0, .45
	right := peak(renderMaterial(t, vk, view, draw))
	if center < 28 || center > 35 || left > center-8 || right < center+8 {
		t.Fatalf("highlights did not follow the camera/light: center=%d moved-eye=%d moved-light=%d", center, left, right)
	}
	if len(vk.frame.uploaded) != 1 {
		t.Fatal("changing the camera or light re-uploaded material geometry")
	}
}

func TestMaterialRoughnessBroadensHighlightGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, draw := materialPlane(t)
	draw.Material = render.Material{Specular: 1, Roughness: .12, Metallic: 1}
	narrow := renderMaterial(t, vk, view, draw)
	draw.Material.Roughness = .8
	broad := renderMaterial(t, vk, view, draw)
	count := func(pixels []byte) int {
		bright := 0
		for y := 5; y < 59; y++ {
			for x := 5; x < 59; x++ {
				if pixels[(y*64+x)*4+2] > 90 {
					bright++
				}
			}
		}
		return bright
	}
	if small, large := count(narrow), count(broad); small == 0 || large < small*3 {
		t.Fatalf("roughness did not broaden the highlight: narrow=%d broad=%d pixels", small, large)
	}
	// The zero roughness setting is safely bounded and means the sharpest
	// supported highlight, rather than a division by zero or a vanished spot.
	draw.Material.Roughness = 0
	sharpest := renderMaterial(t, vk, view, draw)
	draw.Material.Roughness = .08
	if !bytes.Equal(sharpest, renderMaterial(t, vk, view, draw)) {
		t.Fatal("roughness below the minimum changed the bounded highlight")
	}
}

func TestMaterialRimAndDegenerateLightingGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, draw := materialPlane(t)
	view.Light = [3]float32{0, 0, -1}
	draw.Material = render.Material{RimStrength: .5, RimColor: [3]float32{0, 0, 1}}
	front := renderMaterial(t, vk, view, draw)
	view.Eye = [3]float32{3, 0, .8}
	grazing := renderMaterial(t, vk, view, draw)
	center := (32*64 + 32) * 4
	if int(grazing[center])-int(front[center]) < 70 || grazing[center+1] != front[center+1] || grazing[center+2] != front[center+2] {
		t.Fatalf("blue rim did not follow grazing view angle: front=%v grazing=%v", front[center:][:4], grazing[center:][:4])
	}
	// Eye exactly at the center fragment and absent light are valid finite
	// inputs; avoid zero-length view or halfway normalization producing NaNs.
	view.Eye, view.Light = [3]float32{.015625, -.015625, .5}, [3]float32{}
	draw.Material.Specular = 1
	zero := renderMaterial(t, vk, view, draw)
	texturePixel(t, zero, 64, 32, 32, [4]byte{20, 26, 34, 255})
	view.Eye, view.Light = [3]float32{0, 0, 1.5}, [3]float32{0, 0, -1}
	opposite := renderMaterial(t, vk, view, draw)
	texturePixel(t, opposite, 64, 32, 32, [4]byte{20, 26, 34, 255})
}

func TestMaterialDefaultsUnlitAndContentRemainUnchangedGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, draw := materialPlane(t)
	base := renderMaterial(t, vk, view, draw)
	texturePixel(t, base, 64, 32, 32, [4]byte{71, 92, 122, 255})
	view.Light = [3]float32{0, 0, -1}
	texturePixel(t, renderMaterial(t, vk, view, draw), 64, 32, 32, [4]byte{20, 26, 34, 255})
	view.Light = [3]float32{0, 0, 1}
	draw.Material = render.Material{Roughness: 1, Metallic: 1, RimColor: [3]float32{1, 1, 1}}
	if !bytes.Equal(base, renderMaterial(t, vk, view, draw)) {
		t.Fatal("inactive material changed existing diffuse lighting")
	}
	active := render.Material{Specular: 1, Roughness: .24, Metallic: .7, RimStrength: 1, RimColor: [3]float32{.1, .8, 1}}
	draw.Unlit, draw.Material = true, render.Material{}
	unlit := renderMaterial(t, vk, view, draw)
	draw.Material = active
	if !bytes.Equal(unlit, renderMaterial(t, vk, view, draw)) {
		t.Fatal("material altered an unlit mesh")
	}
	texture, err := render.NewTexture(2, 2, []byte{20, 60, 100, 0, 80, 120, 160, 255, 140, 180, 220, 0, 100, 40, 60, 255})
	if err != nil {
		t.Fatal(err)
	}
	draw.Geometry, draw.Texture, draw.Unlit, draw.Material = nil, texture, false, render.Material{}
	draw.Model[14], draw.Color = .5, [4]float32{1, 1, 1, 1}
	view.Eye, view.Light = [3]float32{}, [3]float32{}
	content := renderMaterial(t, vk, view, draw)
	draw.Material = active
	if !bytes.Equal(content, renderMaterial(t, vk, view, draw)) {
		t.Fatal("material altered opaque content pixels")
	}
}

func TestMaterialConstantsStayIndependentBetweenInstancesGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, left := materialPlane(t)
	left.Model[0], left.Model[12] = .4, -.5
	left.Material = render.Material{Specular: 1, Roughness: .2, Metallic: 1}
	right := left
	right.Model[12] = .5
	right.Material = render.Material{RimStrength: .7, RimColor: [3]float32{.1, .2, 1}}
	one := renderMaterial(t, vk, view, left)
	two := renderMaterial(t, vk, view, right)
	together := renderMaterial(t, vk, view, left, right)
	for i := 0; i < len(together); i += 4 {
		want := two[i : i+4]
		if one[i] != 0 || one[i+1] != 0 || one[i+2] != 0 {
			want = one[i : i+4]
		}
		if !bytes.Equal(together[i:i+4], want) {
			t.Fatalf("per-instance material constants crossed at pixel (%d,%d): combined=%v separate=%v", i/4%64, i/4/64, together[i:i+4], want)
		}
	}
}

func TestMaterialFrameBoundaryRejectsInvalidChannelsGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, draw := materialPlane(t)
	for channel := 0; channel < 8; channel++ {
		for _, bad := range []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1)), -.01, 1.01} {
			material := render.Material{}
			channels := []*float32{&material.Specular, &material.Roughness, &material.Metallic, &material.RimStrength, &material.RimColor[0], &material.RimColor[1], &material.RimColor[2], &material.Hologram}
			*channels[channel] = bad
			draw.Material = material
			frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{draw}}}}
			if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, nil); err == nil {
				t.Fatalf("frame accepted material channel %d=%v", channel, bad)
			}
		}
	}
	if len(vk.frame.uploaded) != 0 {
		t.Fatal("invalid material allocated mesh resources before validation")
	}
	draw.Material = render.Material{Specular: 1, Roughness: 1, Metallic: 1, RimStrength: 1, RimColor: [3]float32{1, 1, 1}}
	renderMaterial(t, vk, view, draw) // Boundary values remain valid after errors.
}
