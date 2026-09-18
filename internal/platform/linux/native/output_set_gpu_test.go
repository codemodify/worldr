//go:build linux && cgo

package native

import (
	"errors"
	"image"
	"os"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func outputGPU(t *testing.T, w, h uint32) *VK {
	t.Helper()
	vk, err := OpenVK(false, w, h)
	if err != nil {
		if os.Getenv("WORLDR_TEST_GPU") == "1" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	t.Cleanup(vk.Close)
	if err := vk.SetSceneAtlas(render.Atlas{Width: 1, Height: 1, Pixels: []byte{255}}); err != nil {
		t.Fatal(err)
	}
	return vk
}
func TestOutputSetGPUCropsMatchOneDesktopAndRecoverSharedResources(t *testing.T) {
	full := outputGPU(t, 128, 64)
	left := outputGPU(t, 64, 64)
	right := outputGPU(t, 64, 64)
	set, err := NewOutputSet([]Output{{ID: 11, Bounds: image.Rect(0, 0, 64, 64), VK: left}, {ID: 22, Bounds: image.Rect(64, 0, 128, 64), VK: right}}, 256<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	if set.Primary() != left {
		t.Fatal("primary output changed")
	}
	texture, err := render.NewTexture(2, 2, []byte{120, 0, 0, 180, 0, 180, 0, 180, 0, 0, 100, 180, 100, 100, 0, 180})
	if err != nil {
		t.Fatal(err)
	}
	geometry, err := render.NewGeometry([]render.MeshVertex{
		{X: -.9, Y: -.8, Z: .6, NZ: 1, R: 1, G: .3, B: .1, A: 1, BX: 1},
		{X: .9, Y: -.4, Z: .6, NZ: 1, R: .1, G: 1, B: .2, A: 1, BY: 1},
		{X: .2, Y: .9, Z: .6, NZ: 1, R: .1, G: .3, B: 1, A: 1, BZ: 1},
	}, []uint32{0, 1, 2})
	if err != nil {
		t.Fatal(err)
	}
	identity := [16]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
	view := render.View{Projection: identity, Viewport: [4]float32{13, 7, 102, 50}, Eye: [3]float32{0, 0, 2}, Light: [3]float32{0, 0, 1}, TransparencyLayers: 2}
	surface := render.Draw{Texture: texture, Model: identity, Color: [4]float32{1, 1, 1, 1}, Translucent: true}
	surface.Model[14] = .2
	vertices := []render.Vertex{{X: 54, Y: 52, U: .5, V: .5, R: .2, G: .3, B: .6, A: 1}, {X: 88, Y: 52, U: .5, V: .5, R: .2, G: .3, B: .6, A: 1}, {X: 54, Y: 61, U: .5, V: .5, R: .2, G: .3, B: .6, A: 1}}
	frame := render.Frame{LinearColor: true, Vertices: vertices, Commands: []render.Command{
		{Kind: render.SceneCommand, View: view, Draws: []render.Draw{{Geometry: geometry, Model: identity, Color: [4]float32{1, 1, 1, 1}, Unlit: true}, surface}},
		{Kind: render.ImageCommand, Image: render.Image{Texture: texture, Bounds: [4]float32{51, 3, 26, 14}}},
		{Kind: render.OverlayCommand, First: 0, Count: 3},
	}}
	clearColor := [4]float32{.03, .04, .07, 1}
	baseline := make([]byte, 128*64*4)
	if err := full.RenderFrame(frame, clearColor, baseline); err != nil {
		t.Fatal(err)
	}
	compare := func() {
		t.Helper()
		if err := set.RenderFrame(frame, clearColor); err != nil {
			t.Fatal(err)
		}
		for i, vk := range []*VK{left, right} {
			pixels := make([]byte, 64*64*4)
			if err := vk.RenderFrame(set.outputs[i].frame, clearColor, pixels); err != nil {
				t.Fatal(err)
			}
			for y := 0; y < 64; y++ {
				for x := 0; x < 64; x++ {
					for channel := 0; channel < 4; channel++ {
						want := baseline[(y*128+x+i*64)*4+channel]
						got := pixels[(y*64+x)*4+channel]
						d := int(got) - int(want)
						if d < -2 || d > 2 {
							t.Fatalf("output%d (%d,%d) channel%d=%d want%d", i, x, y, channel, got, want)
						}
					}
				}
			}
			if len(vk.frame.textures) != 1 || len(vk.frame.uploaded) != 1 {
				t.Fatal("shared sources did not retain independent GPU residency")
			}
		}
	}
	compare()
	before := set.MemoryStats()
	if err := set.SetMemoryBudget(before.AllocatedBytes); err != nil {
		t.Fatal(err)
	}
	extra, err := render.NewTexture(64, 64, make([]byte, 64*64*4))
	if err != nil {
		t.Fatal(err)
	}
	denied := render.Frame{Commands: []render.Command{{Kind: render.ImageCommand, Image: render.Image{Texture: extra, Bounds: [4]float32{0, 0, 128, 64}}}}}
	if err := set.RenderFrame(denied, clearColor); !errors.Is(err, ErrOutOfMemory) {
		t.Fatal("shared actual allocation budget not enforced", err)
	}
	if set.MemoryStats().AllocatedBytes != before.AllocatedBytes {
		t.Fatal("budget rejection leaked device allocations")
	}
	if err := set.SetMemoryBudget(256 << 20); err != nil {
		t.Fatal(err)
	}
	if err := set.Recover(); err != nil {
		t.Fatal(err)
	}
	compare()
	if err := set.ReleaseTexture(texture.ID()); err != nil {
		t.Fatal(err)
	}
	if err := set.ReleaseGeometry(geometry.ID()); err != nil {
		t.Fatal(err)
	}
	if len(left.frame.textures) != 0 || len(right.frame.textures) != 0 || len(left.frame.uploaded) != 0 || len(right.frame.uploaded) != 0 {
		t.Fatal("retirement missed an output")
	}
	compare()
	set.Close()
	if w, _ := left.Size(); w != 0 {
		t.Fatal("set did not close owned first renderer")
	}
	if w, _ := right.Size(); w != 0 {
		t.Fatal("set did not close owned second renderer")
	}
	if _, ok := texture.Snapshot(0); !ok {
		t.Fatal("output close destroyed caller-owned source image")
	}
}
