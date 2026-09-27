//go:build linux && cgo

package glass

import (
	"bytes"
	"os"
	"testing"

	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
	"github.com/codemodify/worldr/internal/terminal"
	"golang.org/x/image/font/gofont/gomono"
)

func renderFixture(t testing.TB, width, height int) *App {
	t.Helper()
	canvas, err := scene.NewCanvasWithFont(gomono.TTF)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { canvas.Close() })
	prefs := DefaultPreferences()
	a := &App{canvas: canvas, prefs: prefs, scale: 1, unit: 1, opacity: prefs.Opacity, glow: prefs.Glow, entrance: 1, tabReveal: 1, accent: palette(0)}
	a.layout(width, height)
	return a
}

func glassGPU(t *testing.T, a *App, width, height int) *native.VK {
	t.Helper()
	vk, err := native.OpenVK(false, uint32(width), uint32(height))
	if err != nil {
		if os.Getenv("WORLDR_TEST_GPU") == "1" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	t.Cleanup(vk.Close)
	if err := vk.SetSceneAtlas(a.Atlas()); err != nil {
		t.Fatal(err)
	}
	return vk
}

func populateASCII(a *App) {
	cols, rows := int(a.body.w/a.cellW), int(a.body.h/a.cellH)
	cells := make([]terminal.Cell, cols*rows)
	for i := range cells {
		cells[i] = terminal.Cell{Chars: [6]rune{rune('!' + i%90)}, Width: 1, Foreground: terminal.Color{R: 220, G: 235, B: 238}, Background: terminal.Color{R: 8, G: 16, B: 23}}
	}
	a.tabs = []*terminalPane{{id: 1, snapshot: terminal.Snapshot{Cols: cols, Rows: rows, Cells: cells}}}
}

func TestRoundedWindowFillHasTransparentCornersAndContinuousAlphaGPU(t *testing.T) {
	a := renderFixture(t, 640, 480)
	vk := glassGPU(t, a, 192, 128)
	a.canvas.Reset(192, 128)
	a.canvas.SetLinearColor(true)
	a.roundFill(rect{24, 20, 140, 88}, 20, scene.ColorHex(0x76deec, .5))
	a.roundStroke(rect{24, 20, 140, 88}, 20, 1, scene.ColorHex(0x76deec, .5))
	pixels := make([]byte, 192*128*4)
	if err := vk.RenderFrame(a.canvas.Frame(), [4]float32{}, pixels); err != nil {
		t.Fatal(err)
	}
	alpha := func(x, y int) byte { return pixels[(y*192+x)*4+3] }
	for _, p := range [][2]int{{2, 2}, {24, 20}, {163, 20}, {24, 107}, {163, 107}} {
		if alpha(p[0], p[1]) != 0 {
			t.Fatalf("rounded exterior became opaque at %v", p)
		}
	}
	// Fan triangulation must not double blend seams inside a translucent pane.
	for y := 42; y < 86; y++ {
		for x := 46; x < 140; x++ {
			if a := alpha(x, y); a < 127 || a > 128 {
				t.Fatalf("fill coverage seam at %d,%d: alpha=%d", x, y, a)
			}
		}
	}
	if alpha(90, 20) <= 128 {
		t.Fatal("top border is disconnected from the glass fill")
	}
}

func TestStaticTerminalFrameKeepsGPUResourcesAndPixelsStable(t *testing.T) {
	a := renderFixture(t, 640, 480)
	populateASCII(a)
	vk := glassGPU(t, a, 640, 480)
	pixels := make([]byte, 640*480*4)
	frame := a.Draw(640, 480)
	for _, command := range frame.Commands {
		if command.Kind == render.ImageCommand {
			t.Fatal("ASCII text was rasterized into per-cell textures")
		}
	}
	if err := vk.RenderFrame(frame, [4]float32{}, pixels); err != nil {
		t.Fatal(err)
	}
	before := vk.MemoryStats()
	baseline := bytes.Clone(pixels)
	for i := 0; i < 4; i++ {
		if err := vk.RenderFrame(a.Draw(640, 480), [4]float32{}, pixels); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(baseline, pixels) {
			t.Fatal("static terminal changed pixels across frame reuse")
		}
		after := vk.MemoryStats()
		if before.AllocatedBytes != after.AllocatedBytes || before.Images != after.Images || before.Buffers != after.Buffers {
			t.Fatalf("static frame recreated GPU resources: before=%+v after=%+v", before, after)
		}
	}
	if pixels[3] != 0 {
		t.Fatal("window exterior lost real transparency")
	}
}

func TestWideRoundedGlowStaysCenteredThroughCornersGPU(t *testing.T) {
	a := renderFixture(t, 640, 480)
	vk := glassGPU(t, a, 192, 128)
	a.canvas.Reset(192, 128)
	a.canvas.SetLinearColor(true)
	a.roundStroke(rect{24, 20, 140, 88}, 20, 8, scene.ColorHex(0x76deec, .5))
	pixels := make([]byte, 192*128*4)
	if err := vk.RenderFrame(a.canvas.Frame(), [4]float32{}, pixels); err != nil {
		t.Fatal(err)
	}
	// Both points sit outside the path, inside its centered outer half. The
	// corner must carry the same glow as the straight run, rather than inset it.
	for _, p := range [][2]int{{22, 60}, {22, 34}, {90, 18}} {
		if alpha := pixels[(p[1]*192+p[0])*4+3]; alpha < 100 || alpha > 129 {
			t.Fatalf("rounded glow changed thickness at %v: alpha=%d", p, alpha)
		}
	}
}

func BenchmarkTerminalFrameASCII(b *testing.B) {
	a := renderFixture(b, 1280, 820)
	populateASCII(a)
	a.Draw(1280, 820)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Draw(1280, 820)
	}
}

func fallbackFixture(t *testing.T, width, height int) *App {
	t.Helper()
	a := renderFixture(t, width, height)
	populateASCII(a)
	for i := range a.pane().snapshot.Cells {
		a.pane().snapshot.Cells[i].Chars = [6]rune{}
	}
	painter, err := nativeui.NewPainter(nativeui.Cinematic())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(painter.Close)
	a.fallback = painter
	return a
}

func TestFallbackGlyphPreservesANSIColorAndReverse(t *testing.T) {
	a := fallbackFixture(t, 640, 480)
	red, blue := terminal.Color{R: 255}, terminal.Color{B: 255}
	a.pane().snapshot.Cells[0] = terminal.Cell{Chars: [6]rune{'M'}, Width: 1, Italic: true, Foreground: red, Background: blue}
	a.pane().snapshot.Cells[1] = terminal.Cell{Chars: [6]rune{'M'}, Width: 1, Italic: true, Reverse: true, Foreground: red, Background: blue}
	a.tabReveal = 0 // Content keeps its color/opacity while its position settles.
	a.Draw(640, 480)
	if a.err != nil {
		t.Fatal(a.err)
	}
	if len(a.glyphs) != 2 {
		t.Fatalf("ANSI foreground variants shared one texture: %d", len(a.glyphs))
	}
	for key, texture := range a.glyphs {
		update, _ := texture.Snapshot(0)
		ink := false
		for i := 0; i < len(update.Pixels); i += 4 {
			p := update.Pixels[i : i+4]
			if p[3] == 0 {
				continue
			}
			ink = true
			if key.foreground == red && (p[0] != p[3] || p[1] != 0 || p[2] != 0) {
				t.Fatalf("red glyph changed color: %v", p)
			}
			if key.foreground == blue && (p[0] != 0 || p[1] != 0 || p[2] != p[3]) {
				t.Fatalf("reverse glyph changed color: %v", p)
			}
		}
		if !ink {
			t.Fatal("fallback glyph produced no coverage")
		}
	}
}

func TestVisibleFallbackCacheDoesNotThrashAndRetiresDepartedGlyphs(t *testing.T) {
	a := fallbackFixture(t, 1280, 820)
	const count = 1050
	for i := 0; i < count; i++ {
		// Distinct fallback strings exercise the cache beyond its pruning
		// threshold, as a large CJK terminal viewport does.
		a.pane().snapshot.Cells[i].Chars = [6]rune{rune(0x4e00 + i)}
	}
	a.Draw(1280, 820)
	if a.err != nil {
		t.Fatal(a.err)
	}
	if len(a.glyphs) != count {
		t.Fatalf("cached %d glyphs, expected %d", len(a.glyphs), count)
	}
	ids := make(map[glyphKey]uint64, count)
	for key, texture := range a.glyphs {
		ids[key] = texture.ID()
	}
	for i := 0; i < 3; i++ {
		a.Draw(1280, 820)
		for key, texture := range a.glyphs {
			if ids[key] != texture.ID() {
				t.Fatal("visible glyph texture was recreated")
			}
		}
		if len(a.retired) != 0 {
			t.Fatal("visible glyphs were retired")
		}
	}
	for i := 0; i < count; i++ {
		a.pane().snapshot.Cells[i].Chars = [6]rune{}
	}
	for i := 0; i < 3; i++ {
		a.Draw(1280, 820)
	}
	if len(a.glyphs) != 0 || len(a.retired) != count {
		t.Fatalf("departed cache was not reclaimed: retained=%d retired=%d", len(a.glyphs), len(a.retired))
	}
}

func TestDrawerButtonsFadeWithTheirParent(t *testing.T) {
	a := renderFixture(t, 640, 480)
	a.hover = "reset"
	a.canvas.Reset(640, 480)
	a.drawButtonWithAlpha(button{id: "reset", label: "Reset", box: rect{20, 20, 78, 33}}, .2)
	frame := a.canvas.Frame()
	if len(frame.Vertices) == 0 {
		t.Fatal("button has no geometry")
	}
	for _, vertex := range frame.Vertices {
		if vertex.A > .20001 {
			t.Fatalf("button escaped parent fade: alpha=%f", vertex.A)
		}
	}
}

func TestCompactWindowFitsDrawerAndEightTabs(t *testing.T) {
	for _, size := range [][2]int{{640, 420}, {640, 300}, {420, 420}} {
		a := renderFixture(t, size[0], size[1])
		populateASCII(a)
		first := a.tabs[0]
		for len(a.tabs) < 8 {
			a.tabs = append(a.tabs, first)
		}
		a.settings = true
		a.drawer = 1
		a.layout(size[0], size[1])
		var appearance, glow, motion rect
		for _, b := range a.buttons {
			switch b.id {
			case "appearance":
				appearance = b.box
			case "glow":
				glow = b.box
			case "motion":
				motion = b.box
			case "new":
				t.Fatal("ninth-tab affordance shown at eight sessions")
			}
			if b.box.w < 0 || b.box.h < 0 {
				t.Fatalf("negative button dimensions at %v: %+v", size, b)
			}
		}
		if glow.y+glow.h > motion.y {
			t.Fatalf("drawer controls overlap at %v: glow=%+v motion=%+v", size, glow, motion)
		}
		if a.settingsBox.y < a.frame.y || a.settingsBox.y+a.settingsBox.h > a.frame.y+a.frame.h {
			t.Fatalf("drawer escapes frame at %v", size)
		}
		for _, b := range a.buttons {
			if len(b.id) > 4 && b.id[:4] == "tab:" && b.box.x+b.box.w > appearance.x {
				t.Fatalf("tab overlaps appearance at %v: %+v", size, b)
			}
		}
	}
}
