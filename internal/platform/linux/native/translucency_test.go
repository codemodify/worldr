//go:build linux && cgo

package native

import (
	"bytes"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func TestTranslucentPanelsComposeAndPreserveOpaqueDepthGPU(t *testing.T) {
	vk := openTextureGPU(t)
	texture := func(pixel [4]byte) *render.Texture {
		t.Helper()
		value, err := render.NewTexture(1, 1, pixel[:])
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	view, model := textureView()
	model[0], model[5], model[14] = 1.8, 1.8, .8
	opaque := render.Draw{Texture: texture([4]byte{255, 0, 0, 0}), Model: model, Color: [4]float32{1, 1, 1, 1}}
	back := render.Draw{Texture: texture([4]byte{0, 128, 0, 128}), Model: model, Color: [4]float32{1, 1, 1, 1}, Translucent: true}
	back.Model[14] = .6
	front := render.Draw{Texture: texture([4]byte{0, 0, 128, 128}), Model: model, Color: [4]float32{1, 1, 1, 1}, Translucent: true}
	front.Model[14] = .3
	first := renderMaterial(t, vk, view, front, opaque, back)
	texturePixel(t, first, 64, 32, 32, [4]byte{63, 64, 128, 255})
	for _, draws := range [][]render.Draw{{opaque, back, front}, {back, front, opaque}, {front, back, opaque}} {
		if !bytes.Equal(first, renderMaterial(t, vk, view, draws...)) {
			t.Fatal("spatial transparency depends on submission order")
		}
	}
	// Opaque depth rejects a layer behind it even though all translucent draws
	// run after opaque work. Moving just that opaque plane cannot recreate textures.
	opaque.Model[14] = .45
	texturePixel(t, renderMaterial(t, vk, view, front, back, opaque), 64, 32, 32, [4]byte{127, 0, 128, 255})
	opaque.Model[14] = .1
	texturePixel(t, renderMaterial(t, vk, view, back, front, opaque), 64, 32, 32, [4]byte{255, 0, 0, 255})
	opaque.Model[14] = .8
	front.Model[14], back.Model[14] = .7, .2
	texturePixel(t, renderMaterial(t, vk, view, front, opaque, back), 64, 32, 32, [4]byte{63, 128, 64, 255})
	if len(vk.frame.textures) != 3 {
		t.Fatal("moving panels changed retained texture resources")
	}
}

func TestTranslucentTextureTintAndTransparentHolesGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, model := textureView()
	model[0], model[5], model[14] = 1.8, 1.8, .7
	opaque := render.Draw{Model: model, Color: [4]float32{1, 1, 1, 1}}
	var err error
	opaque.Texture, err = render.NewTexture(1, 1, []byte{240, 160, 80, 255})
	if err != nil {
		t.Fatal(err)
	}
	front := opaque
	front.Translucent = true
	front.Model[14] = .2
	front.Color = [4]float32{.5, 1, .25, .5}
	front.Texture, err = render.NewTexture(2, 1, []byte{128, 64, 32, 128, 0, 0, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	pixels := renderMaterial(t, vk, view, front, opaque)
	texturePixel(t, pixels, 64, 12, 32, [4]byte{212, 152, 64, 255})
	texturePixel(t, pixels, 64, 52, 32, [4]byte{240, 160, 80, 255})
	// Fully transparent content cannot hide another translucent layer either.
	front.Color[3] = 0
	if !bytes.Equal(renderMaterial(t, vk, view, opaque), renderMaterial(t, vk, view, front, opaque)) {
		t.Fatal("zero-alpha panel changed the image")
	}
}

func TestTranslucentMeshesUsePerPixelCameraDepthGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, back := materialPlane(t)
	back.Unlit, back.Translucent = true, true
	back.Color = [4]float32{1, 0, 0, .5}
	back.Model[14] = .2
	front := back
	front.Color = [4]float32{0, 0, 1, .5}
	front.Model[14] = -.2
	texturePixel(t, renderMaterial(t, vk, view, front, back), 64, 32, 32, [4]byte{64, 0, 128, 255})
	// Reversing the view's depth direction reverses blend order independently
	// of insertion order, without changing either object or retained geometry.
	view.Projection[10], view.Projection[14] = -1, 1
	texturePixel(t, renderMaterial(t, vk, view, front, back), 64, 32, 32, [4]byte{128, 0, 64, 255})
	if len(vk.frame.uploaded) != 1 {
		t.Fatal("sorting re-uploaded a mesh")
	}
}

func TestTranslucentIntersectingPanelsComposePerPixelGPU(t *testing.T) {
	vk := openTextureGPU(t)
	texture := func(pixel [4]byte) *render.Texture {
		t.Helper()
		value, err := render.NewTexture(1, 1, pixel[:])
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	view, model := textureView()
	model[0], model[5], model[14] = 1.8, 1.8, .5
	red := render.Draw{Texture: texture([4]byte{128, 0, 0, 128}), Model: model, Color: [4]float32{1, 1, 1, 1}, Translucent: true}
	red.Model[2] = .7
	blue := red
	blue.Texture = texture([4]byte{0, 0, 128, 128})
	blue.Model[2] = -.7
	first := renderMaterial(t, vk, view, red, blue)
	texturePixel(t, first, 64, 16, 32, [4]byte{128, 0, 64, 255})
	texturePixel(t, first, 64, 48, 32, [4]byte{64, 0, 128, 255})
	if !bytes.Equal(first, renderMaterial(t, vk, view, blue, red)) {
		t.Fatal("intersecting planes still depend on a single object sort order")
	}
	// Each side has the opposite front panel. Their overlap therefore cannot be
	// composed correctly by any one global order of the two draw calls.
	frame := render.Frame{LinearColor: true, Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{blue, red}}}}
	pixels := make([]byte, 64*64*4)
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, pixels, 64, 16, 32, [4]byte{188, 0, 137, 255})
	texturePixel(t, pixels, 64, 48, 32, [4]byte{137, 0, 188, 255})
}

func TestTranslucencyLayerBudgetRetainsNearestAndTransparentHolesGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, back := materialPlane(t)
	back.Unlit = true
	back.Color = [4]float32{1, 0, 0, 1}
	back.Model[14] = .3
	green := back
	green.Color = [4]float32{0, 1, 0, .5}
	green.Translucent = true
	green.Model[14] = 0
	blue := green
	blue.Color = [4]float32{0, 0, 1, .5}
	blue.Model[14] = -.3
	view.TransparencyLayers = 1
	texturePixel(t, renderMaterial(t, vk, view, green, blue, back), 64, 32, 32, [4]byte{128, 0, 128, 255})
	view.TransparencyLayers = 2
	texturePixel(t, renderMaterial(t, vk, view, green, blue, back), 64, 32, 32, [4]byte{64, 64, 128, 255})
	// A fully transparent layer never consumes the finite peel budget.
	blue.Color[3] = 0
	view.TransparencyLayers = 1
	texturePixel(t, renderMaterial(t, vk, view, green, blue, back), 64, 32, 32, [4]byte{128, 128, 0, 255})
}

func TestTranslucencyComposesSelfOverlappingMeshGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, model := textureView()
	var vertices []render.MeshVertex
	var indices []uint32
	for layer := 0; layer < 2; layer++ {
		offset := uint32(len(vertices))
		slope := float32(.35)
		if layer == 1 {
			slope = -slope
		}
		for _, xy := range [][2]float32{{-.9, -.9}, {.9, -.9}, {.9, .9}, {-.9, .9}} {
			v := render.MeshVertex{X: xy[0], Y: xy[1], Z: .5 + xy[0]*slope, NZ: 1, A: .5}
			if layer == 0 {
				v.R = 1
			} else {
				v.B = 1
			}
			vertices = append(vertices, v)
		}
		for _, i := range []uint32{0, 1, 2, 0, 2, 3} {
			indices = append(indices, offset+i)
		}
	}
	geometry, err := render.NewGeometry(vertices, indices)
	if err != nil {
		t.Fatal(err)
	}
	draw := render.Draw{Geometry: geometry, Model: model, Color: [4]float32{1, 1, 1, 1}, Unlit: true, Translucent: true}
	pixels := renderMaterial(t, vk, view, draw)
	texturePixel(t, pixels, 64, 16, 32, [4]byte{128, 0, 64, 255})
	texturePixel(t, pixels, 64, 48, 32, [4]byte{64, 0, 128, 255})
}

func TestTranslucencySeparateViewsAndResizeDoNotLeakGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, left := materialPlane(t)
	left.Unlit, left.Translucent = true, true
	left.Color = [4]float32{1, 0, 0, .5}
	view.Viewport[2] = 32
	other := view
	other.Viewport[0] = 32
	right := left
	right.Color = [4]float32{0, 0, 1, .5}
	frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{left}}, {Kind: render.SceneCommand, View: other, Draws: []render.Draw{right}}}}
	for pass := 0; pass < 3; pass++ {
		width, height := vk.Size()
		pixels := make([]byte, int(width*height)*4)
		if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
			t.Fatal(err)
		}
		texturePixel(t, pixels, int(width), 16, 32, [4]byte{128, 0, 0, 255})
		texturePixel(t, pixels, int(width), 48, 32, [4]byte{0, 0, 128, 255})
		if pass == 0 {
			if err := vk.Resize(80, 72); err != nil {
				t.Fatal(err)
			}
		}
	}
	frame.Commands = frame.Commands[:1]
	frame.Commands[0].Draws = nil
	pixels := make([]byte, 80*72*4)
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, pixels, 80, 16, 32, [4]byte{0, 0, 0, 255})
	texturePixel(t, pixels, 80, 48, 32, [4]byte{0, 0, 0, 255})
}
