package workspace

import (
	"bytes"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func panelLayerWorkspace(t testing.TB) *Workspace {
	t.Helper()
	w, err := NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	w.environment.CatCollisions = false
	selected, err := skin.Builtin("future-panels")
	if err != nil {
		t.Fatal(err)
	}
	selected.ID = "custom.layer-fixture"
	if err := w.SetSkin(selected); err != nil {
		t.Fatal(err)
	}
	pixels := make([]byte, 960*600*4)
	for i := 0; i < len(pixels); i += 4 {
		pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = 16, 24, 36, 172
		if (i/4/960)%35 < 2 {
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = 133, 169, 153, 220
		}
	}
	texture, err := render.NewTexture(960, 600, pixels)
	if err != nil {
		t.Fatal(err)
	}
	apps := &fakeApplications{}
	for i := 0; i < 5; i++ {
		apps.surfaces = append(apps.surfaces, experience.ApplicationSurface{ID: uint64(40 + i), Key: fmt.Sprintf("layers:%d", i), Title: fmt.Sprintf("Panel %d", i+1), Texture: texture})
	}
	w.SetApplications(apps)
	w.Draw(1440, 900)
	w.m.yaw, w.m.pitch, w.m.zoom, w.m.cameraX = 1.07, .34, -.12, -1.2
	placements := [][5]float32{{-4.3, .8, -1.8, 780, 880}, {0, .6, .5, 1000, 900}, {4.4, 1.1, -2.8, 980, 710}, {3.8, -2.3, -1.8, 850, 570}, {-3, -2.7, -3.8, 900, 560}}
	for i, p := range placements {
		layout := &w.m.applicationState.Layouts[i]
		layout.X, layout.Y, layout.Depth, layout.Width, layout.Height = p[0], p[1], p[2], int(p[3]), int(p[4])
	}
	w.Draw(1440, 900)
	return w
}

func TestPanelLayerBudgetRecognizesOnlyProvenRecipes(t *testing.T) {
	selected := testWindowSkin(t, "future-panels")
	selected.ID = "custom.retinted-panels"
	selected.Palette["chrome-base"] = "#52302080"
	if !knownPanelLayerRecipes(&selected) {
		t.Fatal("custom ID and palette discarded proven geometry")
	}
	selected.Window.Frame.Layers[0].Geometry.Corner = .2
	if knownPanelLayerRecipes(&selected) {
		t.Fatal("modified geometry incorrectly retained proven layer bound")
	}
	w := panelLayerWorkspace(t)
	if layers := w.scene.TransparencyLayers; layers >= 32 {
		t.Fatalf("separated known planar chrome still requests %d layers", layers)
	} else {
		t.Logf("five-window composed scene uses %d layers", layers)
	}
	before := w.scene.TransparencyLayers
	mesh := w.scene.Node(w.applicationFrames[w.applicationSurfaces[0].ID]).Mesh
	w.scene.Add(0, scene.Node{Mesh: mesh, Translucent: true})
	w.fitApplicationShadow()
	if got := w.scene.TransparencyLayers; got != min(32, before+8) {
		t.Fatalf("arbitrary mesh allowance changed: %d -> %d", before, got)
	}
	selected = *w.CurrentSkin()
	selected.Window.Grip.Layers[0].Bounds.W += .01
	if err := w.SetSkin(selected); err != nil {
		t.Fatal(err)
	}
	w.Draw(1440, 900)
	if w.scene.TransparencyLayers != 32 {
		t.Fatalf("custom geometry lost fallback allowance: %d", w.scene.TransparencyLayers)
	}
}

// Only the main scene is changed: the retained scenery has its own independent
// authored budget, and changing that would compare different compositions.
func forceMainPanelLayers(frame render.Frame, layers int) render.Frame {
	frame.Commands = append([]render.Command(nil), frame.Commands...)
	for i := len(frame.Commands) - 1; i >= 0; i-- {
		if frame.Commands[i].Kind == render.SceneCommand {
			frame.Commands[i].View.TransparencyLayers = layers
			break
		}
	}
	return frame
}

func TestPanelLayerBudgetMatchesFullDepthGPU(t *testing.T) {
	if os.Getenv("WORLDR_TEST_GPU") != "1" {
		t.Skip("set WORLDR_TEST_GPU=1 for native layer parity")
	}
	w := panelLayerWorkspace(t)
	vk, err := native.OpenVK(false, 1440, 900)
	if err != nil {
		t.Fatal(err)
	}
	defer vk.Close()
	if err := vk.SetSceneAtlas(w.Atlas()); err != nil {
		t.Fatal(err)
	}
	initial := w.m
	for _, scenario := range []string{"composed", "overlap", "oblique", "read", "resize"} {
		t.Run(scenario, func(t *testing.T) {
			w.m = initial
			width, height := 1440, 900
			switch scenario {
			case "overlap":
				for i := 0; i < 5; i++ {
					p := &w.m.applicationState.Layouts[i]
					p.X, p.Y, p.Depth = float32(i)*.1, float32(i)*.1, float32(i)*-.3
				}
			case "oblique":
				w.m.yaw += .6
				w.m.pitch += .35
			case "read":
				w.m.applicationState.Reading = true
			case "resize":
				w.m.applicationState.Reading = false
				width, height = 900, 1440
				if err := vk.Resize(uint32(width), uint32(height)); err != nil {
					t.Fatal(err)
				}
			}
			frame := w.Draw(width, height)
			actual, reference := make([]byte, width*height*4), make([]byte, width*height*4)
			if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, actual); err != nil {
				t.Fatal(err)
			}
			if err := vk.RenderFrame(forceMainPanelLayers(frame, 32), [4]float32{0, 0, 0, 1}, reference); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(actual, reference) {
				differing, maximum := 0, 0
				for i, a := range actual {
					if a != reference[i] {
						differing++
						delta := int(a) - int(reference[i])
						if delta < 0 {
							delta = -delta
						}
						maximum = max(maximum, delta)
					}
				}
				t.Fatalf("%d layers differs from full depth: %d channels, max error %d", w.scene.TransparencyLayers, differing, maximum)
			}
			t.Logf("%d layers: byte-identical to 32", w.scene.TransparencyLayers)
		})
	}
}

func BenchmarkPanelLayerBudgetGPU(b *testing.B) {
	if os.Getenv("WORLDR_TEST_GPU") != "1" {
		b.Skip("set WORLDR_TEST_GPU=1 for native timing")
	}
	// Setup uses the same deterministic scene as the correctness test.
	w := panelLayerWorkspace(b)
	vk, err := native.OpenVK(false, 1440, 900)
	if err != nil {
		b.Fatal(err)
	}
	defer vk.Close()
	if err := vk.SetSceneAtlas(w.Atlas()); err != nil {
		b.Fatal(err)
	}
	frame := w.Draw(1440, 900)
	pixels := make([]byte, 1440*900*4)
	for _, mode := range []string{"full32", "bounded"} {
		b.Run(mode, func(b *testing.B) {
			current := frame
			if mode == "full32" {
				current = forceMainPanelLayers(frame, 32)
			}
			for i := 0; i < 3; i++ {
				if err := vk.RenderFrame(current, [4]float32{0, 0, 0, 1}, pixels); err != nil {
					b.Fatal(err)
				}
			}
			b.ResetTimer()
			start := time.Now()
			for i := 0; i < b.N; i++ {
				if err := vk.RenderFrame(current, [4]float32{0, 0, 0, 1}, pixels); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(time.Since(start).Nanoseconds())/float64(b.N)/1e6, "ms/frame")
		})
	}
}
