//go:build linux && cgo

package native

import (
	"bytes"
	"errors"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func TestRendererMemoryBudgetAndFailedResizePreserveLiveFrameGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, draw := materialPlane(t)
	draw.Unlit = true
	frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{draw}}}}
	baseline := make([]byte, 64*64*4)
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, baseline); err != nil {
		t.Fatal(err)
	}
	stats := vk.MemoryStats()
	if stats.AllocatedBytes == 0 || stats.PeakBytes < stats.AllocatedBytes || stats.BudgetBytes != DefaultMemoryBudget || stats.Images == 0 || stats.Buffers == 0 {
		t.Fatalf("missing actual allocation accounting: %+v", stats)
	}
	if err := vk.SetMemoryBudget(stats.AllocatedBytes - 1); !errors.Is(err, ErrOutOfMemory) {
		t.Fatal("lowering below current use must fail visibly", err)
	}
	if vk.MemoryStats().BudgetBytes != DefaultMemoryBudget {
		t.Fatal("rejected budget changed limit")
	}
	if err := vk.SetMemoryBudget(stats.AllocatedBytes); err != nil {
		t.Fatal(err)
	}
	texture, err := render.NewTexture(16, 16, textureSolid(16, 16, [4]byte{255, 0, 0, 255}))
	if err != nil {
		t.Fatal(err)
	}
	extra := draw
	extra.Geometry = nil
	extra.Texture = texture
	oversized := frame
	oversized.Commands = []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{draw, extra}}}
	if err := vk.RenderFrame(oversized, [4]float32{0, 0, 0, 1}, nil); !errors.Is(err, ErrOutOfMemory) {
		t.Fatal("aggregate budget failed to reject a new allocation", err)
	}
	if after := vk.MemoryStats(); after.AllocatedBytes != stats.AllocatedBytes || after.Images != stats.Images || after.Buffers != stats.Buffers {
		t.Fatalf("rejected upload leaked allocations: before=%+v after=%+v", stats, after)
	}
	if err := vk.Resize(1280, 720); !errors.Is(err, ErrOutOfMemory) {
		t.Fatal("oversized resize ignored budget", err)
	}
	if w, h := vk.Size(); w != 64 || h != 64 {
		t.Fatalf("failed resize changed working extent to %dx%d", w, h)
	}
	if err := vk.Resize(0, 0); !errors.Is(err, ErrNotReady) {
		t.Fatal("zero-sized extent should defer presentation", err)
	}
	linear := frame
	linear.LinearColor = true
	if err := vk.RenderFrame(linear, [4]float32{0, 0, 0, 1}, nil); !errors.Is(err, ErrOutOfMemory) {
		t.Fatal("linear target growth ignored budget", err)
	}
	pixels := make([]byte, len(baseline))
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pixels, baseline) {
		t.Fatal("failed resource growth damaged the live frame")
	}
	if err := vk.SetMemoryBudget(0); err != nil {
		t.Fatal(err)
	}
	if err := vk.Resize(80, 72); err != nil {
		t.Fatal("valid resize after rollback failed", err)
	}
}

func TestRecoverRebuildsDeviceResidencyAndRestoresOwnedAtlasGPU(t *testing.T) {
	vk := openTextureGPU(t)
	atlas := render.Atlas{Width: 1, Height: 1, Pixels: []byte{128}}
	if err := vk.SetSceneAtlas(atlas); err != nil {
		t.Fatal(err)
	}
	atlas.Pixels[0] = 255 // Recovery owns the last successful uploaded pixels.
	view, draw := materialPlane(t)
	draw.Unlit = true
	texture, err := render.NewTexture(2, 2, textureSolid(2, 2, [4]byte{20, 80, 160, 255}))
	if err != nil {
		t.Fatal(err)
	}
	content := draw
	content.Geometry = nil
	content.Texture = texture
	content.Model[14] = .3
	frame := render.Frame{LinearColor: true, Vertices: []render.Vertex{
		{X: 0, Y: 0, U: .5, V: .5, R: 1, G: 1, B: 1, A: 1}, {X: 16, Y: 0, U: .5, V: .5, R: 1, G: 1, B: 1, A: 1}, {X: 0, Y: 16, U: .5, V: .5, R: 1, G: 1, B: 1, A: 1},
	}, Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{draw, content}}, {Kind: render.OverlayCommand, Count: 3}}}
	baseline := make([]byte, 64*64*4)
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, baseline); err != nil {
		t.Fatal(err)
	}
	limit := vk.MemoryStats().AllocatedBytes * 2
	if err := vk.SetMemoryBudget(limit); err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 2; iteration++ {
		if err := vk.Recover(); err != nil {
			t.Fatal(err)
		}
		if vk.frame != nil {
			t.Fatal("device recovery retained stale residency flags")
		}
		if stats := vk.MemoryStats(); stats.BudgetBytes != limit || stats.AllocatedBytes == 0 {
			t.Fatalf("recovery lost budget/atlas ownership: %+v", stats)
		}
		pixels := make([]byte, len(baseline))
		if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(pixels, baseline) {
			t.Fatal("device recreation changed geometry, texture, atlas or linear output")
		}
		if len(vk.frame.uploaded) != 1 || len(vk.frame.textures) != 1 {
			t.Fatal("recovery did not reupload retained resources")
		}
	}
	vk.Close()
	if stats := vk.MemoryStats(); stats != (MemoryStats{}) {
		t.Fatalf("closed device reports live allocation: %+v", stats)
	}
	if err := vk.Recover(); err == nil {
		t.Fatal("closed device recovered")
	}
}

func TestRejectedAtlasReplacementKeepsPreviousCoverageGPU(t *testing.T) {
	vk := openTextureGPU(t)
	if err := vk.SetSceneAtlas(render.Atlas{Width: 1, Height: 1, Pixels: []byte{128}}); err != nil {
		t.Fatal(err)
	}
	frame := render.Frame{Vertices: []render.Vertex{{X: 0, Y: 0, U: .5, V: .5, R: 1, G: 1, B: 1, A: 1}, {X: 64, Y: 0, U: .5, V: .5, R: 1, G: 1, B: 1, A: 1}, {X: 0, Y: 64, U: .5, V: .5, R: 1, G: 1, B: 1, A: 1}}, Commands: []render.Command{{Kind: render.OverlayCommand, Count: 3}}}
	pixels := make([]byte, 64*64*4)
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	if err := vk.SetMemoryBudget(vk.MemoryStats().AllocatedBytes); err != nil {
		t.Fatal(err)
	}
	if err := vk.SetSceneAtlas(render.Atlas{Width: 1, Height: 1, Pixels: []byte{255}}); !errors.Is(err, ErrOutOfMemory) {
		t.Fatal("atlas replacement exceeded budget", err)
	}
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, pixels, 64, 8, 8, [4]byte{128, 128, 128, 255})
}
