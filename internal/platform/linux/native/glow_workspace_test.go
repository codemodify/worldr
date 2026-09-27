//go:build linux && cgo

package native

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/workspace"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

type glowWorkspaceApplications struct{ surface experience.ApplicationSurface }

func (a *glowWorkspaceApplications) Surfaces() []experience.ApplicationSurface {
	return []experience.ApplicationSurface{a.surface}
}
func (*glowWorkspaceApplications) Focus(uint64)                  {}
func (*glowWorkspaceApplications) Send(uint64, experience.Event) {}
func (*glowWorkspaceApplications) Resize(uint64, int, int)       {}

// The actual Hologram desktop layout, camera, frame, glass/refraction, title,
// wallpaper and corner controls run here. Only the external application's
// opaque client pixels are a fixture; they cannot affect the glow seed.
func newGlowWorkspace(tb testing.TB) (*workspace.Workspace, *VK) {
	tb.Helper()
	vk, err := OpenVK(false, 1584, 1248)
	if err != nil {
		if os.Getenv("WORLDR_TEST_GPU") == "1" {
			tb.Fatal(err)
		}
		tb.Skip(err)
	}
	tb.Cleanup(vk.Close)
	w, err := workspace.NewDesktop()
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { w.Close() })
	data, err := os.ReadFile("../../../../examples/hologram-desktop/layout.json")
	if err != nil {
		tb.Fatal(err)
	}
	var envelope struct {
		State json.RawMessage `json:"state"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		tb.Fatal(err)
	}
	if err := w.LoadState(envelope.State); err != nil {
		tb.Fatal(err)
	}
	selected, err := skin.Builtin("hologram")
	if err != nil {
		tb.Fatal(err)
	}
	if err := w.SetSkin(selected); err != nil {
		tb.Fatal(err)
	}
	texture, err := render.NewTexture(1440, 1000, textureSolid(1440, 1000, [4]byte{1, 7, 21, 255}))
	if err != nil {
		tb.Fatal(err)
	}
	w.SetApplications(&glowWorkspaceApplications{experience.ApplicationSurface{
		ID: 1, Key: "sdk:dev.worldr.hologram-desktop/volumes", AppID: "dev.worldr.hologram-desktop",
		Title: "Disk Management", Texture: texture, MinWidth: 900, MinHeight: 650,
	}})
	return w, vk
}

func TestGlowCacheHologramWorkspaceUpdatesKeepExactPixelsGPU(t *testing.T) {
	w, vk := newGlowWorkspace(t)
	for i := range 12 {
		w.Update(time.Second / 60)
		frame := w.Draw(1584, 1248)
		for _, id := range w.RetiredGeometryIDs() {
			if err := vk.ReleaseGeometry(id); err != nil {
				t.Fatal(err)
			}
		}
		beforeRendered, beforeReused := vk.glowCacheStats()
		got := glowFrame(t, vk, frame)
		rendered, reused := vk.glowCacheStats()
		if i > 0 && (rendered != beforeRendered || reused != beforeReused+1) {
			t.Fatalf("workspace update %d lost retained halo: rendered %d->%d reused %d->%d", i, beforeRendered, rendered, beforeReused, reused)
		}
		vk.invalidateGlowCache()
		if !bytes.Equal(got, glowFrame(t, vk, frame)) {
			t.Fatalf("workspace update %d differs with retained halo", i)
		}
	}
}

// Unlike BenchmarkRetainedGlowGPU's isolated emissive plane, this includes the
// full desktop frame and the original eight-layer transparency pipeline.
func BenchmarkRetainedGlowWorkspaceGPU(b *testing.B) {
	for _, force := range []bool{true, false} {
		name := "retained"
		if force {
			name = "forced"
		}
		b.Run(name, func(b *testing.B) {
			w, vk := newGlowWorkspace(b)
			clear := [4]float32{0, 0, 0, 1}
			for range 3 {
				w.Update(time.Second / 60)
				if err := vk.RenderFrame(w.Draw(1584, 1248), clear, nil); err != nil {
					b.Fatal(err)
				}
			}
			before, _ := vk.glowCacheStats()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				w.Update(time.Second / 60)
				frame := w.Draw(1584, 1248)
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
