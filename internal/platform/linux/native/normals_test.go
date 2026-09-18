//go:build linux && cgo

package native

import (
	"testing"

	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

func TestExplicitNormalsSmoothLightingAcrossSharedEdgeGPU(t *testing.T) {
	vk := openTextureGPU(t)
	vertices := []scene.Vec3{{X: -.9, Y: -.9, Z: .25}, {Y: -.9, Z: .5}, {Y: .9, Z: .5}, {X: .9, Y: -.9, Z: .25}}
	normals := []scene.Vec3{{X: -6, Z: 8}, {Z: 10}, {Z: 10}, {X: 6, Z: 8}}
	indices := []uint32{0, 1, 2, 1, 3, 2}
	smooth, err := scene.NewMeshWithNormals(vertices, normals, indices, nil)
	if err != nil {
		t.Fatal(err)
	}
	hard, err := scene.NewMesh(vertices, indices, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, model := textureView()
	view.Eye, view.Light = [3]float32{0, 0, 2}, [3]float32{.7, 0, 1}
	draw := render.Draw{Geometry: hard.Geometry(), Model: model, Color: [4]float32{.5, .5, .5, 1}}
	hardPixels := renderMaterial(t, vk, view, draw)
	draw.Geometry = smooth.Geometry()
	smoothPixels := renderMaterial(t, vk, view, draw)
	left, right := (44*64+30)*4+2, (44*64+33)*4+2
	hardStep := int(hardPixels[right]) - int(hardPixels[left])
	smoothStep := int(smoothPixels[right]) - int(smoothPixels[left])
	if hardStep < 15 || smoothStep < 0 || smoothStep > hardStep/3 {
		t.Fatalf("interpolated normals failed to soften the shared crease: hard=%d smooth=%d", hardStep, smoothStep)
	}
	for i := 0; i < len(smoothPixels); i += 4 {
		if (smoothPixels[i] != 0) != (hardPixels[i] != 0) {
			t.Fatal("lighting normals altered geometric coverage")
		}
	}
}
