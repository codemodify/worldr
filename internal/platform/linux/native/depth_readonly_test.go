//go:build linux && cgo

package native

import (
	"math"
	"testing"

	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

func TestDepthReadOnlyMeshesBlendOverOpaqueContentGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, opaque := materialPlane(t)
	opaque.Unlit, opaque.Color = true, [4]float32{.8, .5, .2, 1}
	line := opaque
	line.Model[5], line.Model[14] = .12, -.15
	line.Color, line.DepthReadOnly = [4]float32{0, .7, 1, .15}, true
	pixels := renderMaterial(t, vk, view, opaque, line)
	texturePixel(t, pixels, 64, 32, 32, [4]byte{173, 135, 82, 255})
	texturePixel(t, pixels, 64, 32, 20, [4]byte{204, 128, 51, 255})
	// Read-only means depth is still tested: a guide behind opaque geometry
	// remains invisible, rather than becoming an always-on-top decoration.
	line.Model[14] = .15
	texturePixel(t, renderMaterial(t, vk, view, opaque, line), 64, 32, 32, [4]byte{204, 128, 51, 255})
	// Two translucent guides retain the caller's blend order. The nearer first
	// guide cannot write depth and suppress the farther guide submitted next.
	opaque.Model[14] = .25
	line.Model[14], line.Color = -.2, [4]float32{0, 0, 1, .2}
	farther := line
	farther.Model[14], farther.Color = 0, [4]float32{1, 0, 0, .3}
	texturePixel(t, renderMaterial(t, vk, view, opaque, line, farther), 64, 32, 32, [4]byte{191, 72, 64, 255})
}

func TestDepthReadOnlyStageFromBelowPreservesOpaqueSurfaceGPU(t *testing.T) {
	vk := openTextureGPU(t)
	canvas, err := scene.NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Close()
	guide, err := scene.NewMesh([]scene.Vec3{
		{X: -1.25, Y: -.5, Z: -.15}, {X: 1.25, Y: -.5, Z: -.15},
		{X: 1.25, Y: -.5, Z: .15}, {X: -1.25, Y: -.5, Z: .15},
	}, []uint32{0, 1, 2, 0, 2, 3}, nil)
	if err != nil {
		t.Fatal(err)
	}
	texture, err := render.NewTexture(2, 2, textureSolid(2, 2, [4]byte{200, 100, 50, 0}))
	if err != nil {
		t.Fatal(err)
	}
	s := scene.NewScene()
	// Register the guide first, as the workspace does. Scene submission must
	// defer it until after the opaque application surface in this same pass.
	s.Add(0, scene.Node{Mesh: guide, Color: scene.Color{G: .8, B: 1, A: .1}, Unlit: true, Unpickable: true, DepthReadOnly: true})
	s.Add(0, scene.Node{Surface: texture, Transform: scene.RotateX(math.Pi / 2).Mul(scene.Scale(3, 3, 1))})
	renderStage := func(eyeY float32) []byte {
		t.Helper()
		canvas.Reset(64, 64)
		s.Draw(canvas, scene.Camera{Eye: scene.Vec3{Y: eyeY}, Up: scene.Vec3{Z: 1}, FOV: 1, Near: .1, Far: 10}, scene.Viewport{Width: 64, Height: 64})
		width, height := vk.Size()
		pixels := make([]byte, int(width*height)*4)
		if err := vk.RenderFrame(canvas.Frame(), [4]float32{0, 0, 0, 1}, pixels); err != nil {
			t.Fatal(err)
		}
		return pixels
	}
	for iteration := 0; iteration < 2; iteration++ {
		width, _ := vk.Size()
		below := renderStage(-4)
		texturePixel(t, below, int(width), 32, 32, [4]byte{180, 110, 71, 255})
		texturePixel(t, below, int(width), 32, 24, [4]byte{200, 100, 50, 255})
		texturePixel(t, renderStage(4), int(width), 32, 32, [4]byte{200, 100, 50, 255})
		if len(vk.frame.uploaded) != 1 || len(vk.frame.textures) != 1 {
			t.Fatal("guide/camera changes recreated retained mesh or content resources")
		}
		if iteration == 0 {
			if err := vk.Resize(80, 72); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestDepthReadOnlyContentSurfaceIsRejectedGPU(t *testing.T) {
	vk := openTextureGPU(t)
	texture, err := render.NewTexture(1, 1, []byte{200, 100, 50, 255})
	if err != nil {
		t.Fatal(err)
	}
	view, model := textureView()
	frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{{Texture: texture, Model: model, Color: [4]float32{1, 1, 1, 1}, DepthReadOnly: true}}}}}
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, nil); err == nil {
		t.Fatal("opaque content accepted a flag that would disable its depth writes")
	}
	if len(vk.frame.textures) != 0 {
		t.Fatal("invalid content depth mode allocated a GPU texture")
	}
}
