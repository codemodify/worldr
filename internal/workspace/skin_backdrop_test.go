package workspace

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/render"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func TestSkinBackdropCoversDisplayRetainsTextureAndRestoresAmbient(t *testing.T) {
	w, err := NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	selected, err := skin.Builtin("merrick")
	if err != nil {
		t.Fatal(err)
	}
	selected.ID = "custom-silver"
	if err := w.SetSkin(selected); err != nil {
		t.Fatal(err)
	}
	environment := w.environment
	phase := w.backgroundPhase
	w.updateBackground(3 * time.Second)
	if w.backgroundPhase != phase {
		t.Fatal("hidden ambient simulation still advances")
	}
	var id uint64
	for _, size := range [][2]int{{2048, 896}, {900, 1440}, {2880, 1800}} {
		frame := w.Draw(size[0], size[1])
		found := false
		for _, cmd := range frame.Commands {
			if cmd.Kind != render.ImageCommand || cmd.Image.Texture != w.skinBackdrop {
				continue
			}
			found = true
			bounds := cmd.Image.Bounds
			if bounds[0] > .01 || bounds[1] > .01 || bounds[0]+bounds[2] < float32(size[0])-.01 || bounds[1]+bounds[3] < float32(size[1])-.01 {
				t.Fatal("wallpaper does not cover the display")
			}
			if id != 0 && cmd.Image.Texture.ID() != id {
				t.Fatal("resize reallocated retained wallpaper")
			}
			id = cmd.Image.Texture.ID()
		}
		if !found || w.environment != environment {
			t.Fatal("wallpaper missing or changed environment preferences")
		}
	}
	selected, _ = skin.Builtin("plasma")
	if err := w.SetSkin(selected); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range w.Draw(1440, 900).Commands {
		if cmd.Kind == render.ImageCommand && cmd.Image.Texture == w.skinBackdrop {
			t.Fatal("previous skin's wallpaper remains visible")
		}
	}
	if w.environment != environment {
		t.Fatal("skin switching lost environment preferences")
	}
}

func TestQuietSkinBackdropUsesCustomPaletteCoversPhysicalDisplayAndHidesAmbient(t *testing.T) {
	for _, id := range []string{"advanced", "hologram"} {
		t.Run(id, func(t *testing.T) {
			w, err := NewDesktop()
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			selected, err := skin.Builtin(id)
			if err != nil {
				t.Fatal(err)
			}
			selected.ID = "custom.quiet-" + id
			selected.Palette["desktop-background"] = "#123456"
			selected.Palette["desktop-glow"] = "#ABCDEF"
			if err := w.SetSkin(selected); err != nil {
				t.Fatal(err)
			}
			environment, phase, catPhase, net := w.environment, w.backgroundPhase, w.ambientCat.phase, *w.energyNet
			w.updateBackground(2 * time.Second)
			w.impactEnergyWall(0, 0, 1)
			w.queueEnergyWallImpact(0, 0, 1, 0)
			if !w.skinBackdropVisible() || w.backgroundPhase != phase || w.ambientCat.phase != catPhase || *w.energyNet != net || len(w.energyWallImpacts) != 0 {
				t.Fatal("quiet backdrop advanced hidden ambient actors or accepted visual impacts")
			}
			for _, size := range [][2]int{{2048, 900}, {900, 1440}, {2880, 1800}} {
				w.layout(size[0], size[1])
				w.canvas.Reset(size[0], size[1])
				w.drawBackground()
				frame := w.canvas.Frame()
				for _, cmd := range frame.Commands {
					if cmd.Kind != render.OverlayCommand {
						t.Fatal("quiet backdrop drew retained ambient scene/image content")
					}
				}
				if len(frame.Vertices) < 6 {
					t.Fatal("quiet backdrop did not cover the display")
				}
				minX, minY, maxX, maxY := float32(size[0]), float32(size[1]), float32(0), float32(0)
				for _, v := range frame.Vertices[:6] {
					if abs(v.R-float32(0x12)/255) > .001 || abs(v.G-float32(0x34)/255) > .001 || abs(v.B-float32(0x56)/255) > .001 || v.A != 1 {
						t.Fatal("quiet backdrop ignored custom background palette")
					}
					minX, minY, maxX, maxY = min(minX, v.X), min(minY, v.Y), max(maxX, v.X), max(maxY, v.Y)
				}
				if minX != 0 || minY != 0 || maxX != float32(size[0]) || maxY != float32(size[1]) {
					t.Fatal("quiet backdrop is limited to the centered design area")
				}
				foundGlow := false
				for _, v := range frame.Vertices[6:] {
					if abs(v.R-float32(0xAB)/255) < .001 && abs(v.G-float32(0xCD)/255) < .001 && abs(v.B-float32(0xEF)/255) < .001 && abs(v.A-.24) < .001 {
						foundGlow = true
					}
				}
				if !foundGlow {
					t.Fatal("quiet backdrop ignored custom glow palette")
				}
			}
			if w.environment != environment {
				t.Fatal("quiet backdrop changed environment preferences")
			}
			selected, _ = skin.Builtin("plasma")
			if err := w.SetSkin(selected); err != nil {
				t.Fatal(err)
			}
			w.updateBackground(time.Millisecond)
			if w.skinBackdropVisible() || w.backgroundPhase == phase || w.environment != environment {
				t.Fatal("switching away did not resume the unchanged ambient environment")
			}
		})
	}
}
