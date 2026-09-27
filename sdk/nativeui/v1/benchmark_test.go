package nativeui

import (
	"image"
	"image/color"
	"testing"

	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func benchmarkPainter(b *testing.B, id string) *Painter {
	b.Helper()
	s, err := skin.Builtin(id)
	if err != nil {
		b.Fatal(err)
	}
	theme, err := ThemeFromSkin(s)
	if err != nil {
		b.Fatal(err)
	}
	p, err := NewPainter(theme)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = p.Close() })
	return p
}

// These benchmarks exclude construction and measure repeated framebuffer draws
// used by a retained application after its skin has already been accepted.
func BenchmarkPainterControls(b *testing.B) {
	for _, id := range []string{"advanced", "hologram", "plasma"} {
		b.Run(id, func(b *testing.B) {
			p := benchmarkPainter(b, id)
			dst := image.NewRGBA(image.Rect(0, 0, 640, 320))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for row := 0; row < 6; row++ {
					y := row*48 + 6
					state := State{Selected: row == 1, Hovered: row == 2, Focused: row == 3}
					if err := p.DrawButton(dst, Control{Bounds: image.Rect(8, y, 196, y+32), Label: "SELECT PROJECT", Icon: IconPlay, State: state}); err != nil {
						b.Fatal(err)
					}
					if err := p.DrawField(dst, Control{Bounds: image.Rect(212, y, 430, y+32), Text: "studio@example.org", State: state}); err != nil {
						b.Fatal(err)
					}
					if err := p.DrawSlider(dst, Control{Bounds: image.Rect(446, y, 632, y+32), Min: 0, Max: 100, Value: 62, State: state}); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
func BenchmarkPainterBackground(b *testing.B) {
	for _, id := range []string{"advanced", "hologram", "plasma"} {
		b.Run(id, func(b *testing.B) {
			p := benchmarkPainter(b, id)
			dst := image.NewRGBA(image.Rect(0, 0, 640, 360))
			bounds := image.Rect(8, 8, 632, 352)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := p.DrawControlBackground(dst, "panel", bounds, State{Focused: true}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
func BenchmarkPainterLabels(b *testing.B) {
	p := benchmarkPainter(b, "advanced")
	dst := image.NewRGBA(image.Rect(0, 0, 640, 480))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for row := 0; row < 20; row++ {
			if err := p.DrawLabel(dst, image.Rect(12, 12+row*22, 624, 34+row*22), "WORLDR / ADVANCED STUDIOS — native interface / 2026", LabelStyle{Color: color.RGBA{R: 211, G: 221, B: 239, A: 230}}); err != nil {
				b.Fatal(err)
			}
		}
	}
}
