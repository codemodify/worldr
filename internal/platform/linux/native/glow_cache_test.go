//go:build linux && cgo

package native

import (
	"bytes"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func assertGlowCacheEquivalent(t *testing.T, vk *VK, frame render.Frame, expectReuse bool) {
	t.Helper()
	beforeRendered, beforeReused := vk.glowCacheStats()
	got := glowFrame(t, vk, frame)
	rendered, reused := vk.glowCacheStats()
	if expectReuse {
		if rendered != beforeRendered || reused != beforeReused+1 {
			t.Fatalf("unchanged glow was recomputed: rendered %d->%d reused %d->%d", beforeRendered, rendered, beforeReused, reused)
		}
	} else if rendered != beforeRendered+1 || reused != beforeReused {
		t.Fatalf("changed glow reused stale pixels: rendered %d->%d reused %d->%d", beforeRendered, rendered, beforeReused, reused)
	}
	vk.invalidateGlowCache()
	want := glowFrame(t, vk, frame)
	if !bytes.Equal(got, want) {
		t.Fatal("retained glow output differs from forced seed/blur rendering")
	}
}

func TestGlowCacheInvalidatesEverySeedDependencyAndKeepsPixelsExactGPU(t *testing.T) {
	for _, change := range []string{"unchanged", "mesh-model", "projection", "viewport", "emission", "alpha", "vertex-alpha", "depth-write", "mesh-order", "surface-model", "surface-pixels", "irrelevant-uniforms"} {
		t.Run(change, func(t *testing.T) {
			vk := openTextureGPU(t)
			view, emitter := glowFixture(t)
			texture, err := render.NewTexture(2, 2, textureSolid(2, 2, [4]byte{30, 60, 90, 255}))
			if err != nil {
				t.Fatal(err)
			}
			_, model := textureView()
			model[0], model[5], model[12], model[14] = .12, .3, .18, .2
			surface := render.Draw{Texture: texture, Model: model, Color: [4]float32{1, 1, 1, 1}}
			frame := render.Frame{Commands: []render.Command{glowScene(view, emitter, surface)}}
			glowFrame(t, vk, frame)
			assertGlowCacheEquivalent(t, vk, frame, true)
			draw := &frame.Commands[0].Draws[0]
			expectReuse := false
			switch change {
			case "unchanged":
				expectReuse = true
			case "mesh-model":
				draw.Model[12] += .08
			case "projection":
				frame.Commands[0].View.Projection[0] *= .8
			case "viewport":
				frame.Commands[0].View.Viewport = [4]float32{2.5, 1.25, 58, 60}
			case "emission":
				draw.Glow = [3]float32{.2, .8, .3}
			case "alpha":
				draw.Color[3] = .45
			case "vertex-alpha":
				vertices := draw.Geometry.Vertices()
				for i := range vertices {
					vertices[i].A = .4
				}
				geometry, err := render.NewGeometry(vertices, draw.Geometry.Indices())
				if err != nil {
					t.Fatal(err)
				}
				draw.Geometry = geometry
			case "depth-write":
				draw.DepthReadOnly = true
			case "mesh-order":
				frame.Commands[0].Draws[0], frame.Commands[0].Draws[1] = frame.Commands[0].Draws[1], frame.Commands[0].Draws[0]
			case "surface-model":
				frame.Commands[0].Draws[1].Model[12] = 0
			case "surface-pixels":
				if err := texture.Replace(2, 2, textureSolid(2, 2, [4]byte{190, 150, 70, 255})); err != nil {
					t.Fatal(err)
				}
				frame.Commands[0].Draws[1].UV = [4]float32{.1, .1, .8, .8}
				expectReuse = true
			case "irrelevant-uniforms":
				frame.Commands[0].View.EffectPhase = .5
				frame.Commands[0].View.Light = [3]float32{1, 0, 0}
				draw.Color[0], draw.Color[1], draw.Color[2] = .8, .3, .2
				draw.Material.Specular = .6
				draw.WireWidth = 1
				draw.WireColor = [4]float32{1, 1, 1, 1}
				expectReuse = true
			}
			assertGlowCacheEquivalent(t, vk, frame, expectReuse)
			assertGlowCacheEquivalent(t, vk, frame, true)
		})
	}
}

func TestGlowCacheTargetRecreationGeometryReleaseAndCameraSlotsGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, emitter := glowFixture(t)
	frame := render.Frame{Commands: []render.Command{glowScene(view, emitter)}}
	initial := glowFrame(t, vk, frame)
	if err := vk.Recover(); err != nil {
		t.Fatal(err)
	}
	if got := glowFrame(t, vk, frame); !bytes.Equal(initial, got) {
		t.Fatal("device recovery changed retained glow output")
	}
	if rendered, reused := vk.glowCacheStats(); rendered != 1 || reused != 0 {
		t.Fatal("device recovery retained the previous target cache")
	}
	if err := vk.ReleaseGeometry(emitter.Geometry.ID()); err != nil {
		t.Fatal(err)
	}
	assertGlowCacheEquivalent(t, vk, frame, false)
	for _, linear := range []bool{true, false} {
		frame.LinearColor = linear
		got := glowFrame(t, vk, frame)
		if rendered, reused := vk.glowCacheStats(); rendered != 1 || reused != 0 {
			t.Fatal("target recreation retained old glow cache state")
		}
		vk.invalidateGlowCache()
		if !bytes.Equal(got, glowFrame(t, vk, frame)) {
			t.Fatal("color-mode target recreation changed cached glow")
		}
		assertGlowCacheEquivalent(t, vk, frame, true)
	}
	if err := vk.Resize(81, 49); err != nil {
		t.Fatal(err)
	}
	frame.Commands[0].View.Viewport = [4]float32{-2.5, 1.25, 78.5, 45}
	glowFrame(t, vk, frame)
	if rendered, reused := vk.glowCacheStats(); rendered != 1 || reused != 0 {
		t.Fatal("resizing did not recreate the glow cache")
	}
	assertGlowCacheEquivalent(t, vk, frame, true)
	second := glowScene(view, emitter)
	second.View.Viewport = [4]float32{40, 20, 40, 29}
	second.Draws[0].Glow = [3]float32{.1, .3, .9}
	frame.Commands = append(frame.Commands, second)
	got := glowFrame(t, vk, frame)
	vk.invalidateGlowCache()
	if !bytes.Equal(got, glowFrame(t, vk, frame)) {
		t.Fatal("a new camera slot reused another camera's halo")
	}
	beforeRendered, beforeReused := vk.glowCacheStats()
	got = glowFrame(t, vk, frame)
	if rendered, reused := vk.glowCacheStats(); rendered != beforeRendered || reused != beforeReused+2 {
		t.Fatal("independent unchanged camera slots were not both retained")
	}
	frame.Commands[0], frame.Commands[1] = frame.Commands[1], frame.Commands[0]
	got = glowFrame(t, vk, frame)
	vk.invalidateGlowCache()
	if !bytes.Equal(got, glowFrame(t, vk, frame)) {
		t.Fatal("camera reorder composited stale glow")
	}
}

func TestGlowCacheFallsBackForLargeScopesWithoutChangingOutputGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, emitter := glowFixture(t)
	emitter.Model[12] = 5 // Keep this capacity fixture cheap to rasterize.
	draws := make([]render.Draw, 1025)
	for i := range draws {
		draws[i] = emitter
	}
	frame := render.Frame{Commands: []render.Command{glowScene(view, draws...)}}
	first := glowFrame(t, vk, frame)
	second := glowFrame(t, vk, frame)
	if rendered, reused := vk.glowCacheStats(); rendered != 2 || reused != 0 {
		t.Fatal("oversized scene did not take the bounded uncached path")
	}
	if !bytes.Equal(first, second) {
		t.Fatal("bounded cache fallback changed output")
	}
}

// This measures completed GPU work, with readback disabled and unchanged
// quality. The forced case invalidates only the halo cache before each frame.
func BenchmarkRetainedGlowGPU(b *testing.B) {
	for _, force := range []bool{true, false} {
		name := "retained"
		if force {
			name = "forced"
		}
		b.Run(name, func(b *testing.B) {
			vk, err := OpenVK(false, 1584, 1248)
			if err != nil {
				b.Skip(err)
			}
			defer vk.Close()
			geometry, err := render.NewGeometry([]render.MeshVertex{
				{X: -.8, Y: -.8, Z: .5, NZ: 1, R: 1, G: 1, B: 1, A: 1}, {X: .8, Y: -.8, Z: .5, NZ: 1, R: 1, G: 1, B: 1, A: 1},
				{X: .8, Y: .8, Z: .5, NZ: 1, R: 1, G: 1, B: 1, A: 1}, {X: -.8, Y: .8, Z: .5, NZ: 1, R: 1, G: 1, B: 1, A: 1},
			}, []uint32{0, 1, 2, 0, 2, 3})
			if err != nil {
				b.Fatal(err)
			}
			view, model := textureView()
			view.Viewport = [4]float32{0, 0, 1584, 1248}
			draw := render.Draw{Geometry: geometry, Model: model, Color: [4]float32{.1, .15, .2, 1}, Glow: [3]float32{.42, .3, .42}, Unlit: true}
			frame := render.Frame{Commands: []render.Command{glowScene(view, draw)}}
			clear := [4]float32{0, 0, 0, 1}
			if err := vk.RenderFrame(frame, clear, nil); err != nil {
				b.Fatal(err)
			}
			before, _ := vk.glowCacheStats()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if force {
					vk.invalidateGlowCache()
				}
				if err := vk.RenderFrame(frame, clear, nil); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			after, _ := vk.glowCacheStats()
			b.ReportMetric(float64(after-before)/float64(b.N), "seed-pass/op")
		})
	}
}
