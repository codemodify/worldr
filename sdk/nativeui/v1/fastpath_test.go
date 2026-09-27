package nativeui

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"testing"

	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

// genericDestination deliberately hides concrete framebuffer types, retaining
// the original image/draw and color-interface rendering path as a pixel oracle.
type genericDestination struct{ draw.Image }

func TestRGBARecipeAndTextFastPathsMatchGenericPixels(t *testing.T) {
	for _, id := range []string{"merrick", "advanced", "hologram", "plasma"} {
		t.Run(id, func(t *testing.T) {
			s, err := skin.Builtin(id)
			if err != nil {
				t.Fatal(err)
			}
			theme, err := ThemeFromSkin(s)
			if err != nil {
				t.Fatal(err)
			}
			p, err := NewPainter(theme)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			for _, state := range []State{{}, {Hovered: true}, {Pressed: true}, {Selected: true, Focused: true}, {Disabled: true}, {Invalid: true}} {
				fast := image.NewRGBA(image.Rect(-13, -9, 277, 176))
				slow := image.NewRGBA(fast.Bounds())
				// Exercise nonopaque destination channels and a nonzero stride/origin.
				for y := fast.Rect.Min.Y; y < fast.Rect.Max.Y; y++ {
					for x := fast.Rect.Min.X; x < fast.Rect.Max.X; x++ {
						a := uint8((x - y + 300) % 256)
						v := color.NRGBA{R: uint8(x + 29), G: uint8(y + 17), B: 137, A: a}
						fast.Set(x, y, v)
						slow.Set(x, y, v)
					}
				}
				paint := func(dst draw.Image) {
					dst = clipToBounds(clipToBounds(dst, image.Rect(-8, -5, 270, 168)), image.Rect(0, 0, 258, 160))
					for _, err := range []error{
						p.DrawControlBackground(dst, "panel", image.Rect(-7, -3, 266, 154), state),
						p.DrawButton(dst, Control{Bounds: image.Rect(7, 5, 205, 40), Label: "PROJECT / café", Icon: IconPlay, State: state}),
						p.DrawField(dst, Control{Bounds: image.Rect(7, 46, 231, 79), Text: "studio@example.org", State: state}),
						p.DrawSlider(dst, Control{Bounds: image.Rect(6, 85, 234, 116), Min: 0, Max: 100, Value: 63, State: state}),
						p.DrawLabel(dst, image.Rect(-20, 122, 265, 157), "Clipped / translucent text", LabelStyle{Color: RGBA(0xe5c18793), Align: AlignEnd}),
					} {
						if err != nil {
							t.Fatal(err)
						}
					}
				}
				paint(fast)
				paint(genericDestination{slow})
				if !bytes.Equal(fast.Pix, slow.Pix) {
					for i := range fast.Pix {
						if fast.Pix[i] != slow.Pix[i] {
							t.Fatalf("state %+v differs at byte %d: fast %d, generic %d", state, i, fast.Pix[i], slow.Pix[i])
						}
					}
				}
			}
		})
	}
}

func TestRGBAFastBlendMatchesGenericAcrossAlphaAndSubimages(t *testing.T) {
	fast := image.NewRGBA(image.Rect(-2, -2, 19, 17))
	slow := image.NewRGBA(fast.Rect)
	for alpha := 0; alpha < 256; alpha++ {
		for channel := 0; channel < 256; channel += 17 {
			base := color.NRGBA{R: uint8(channel), G: 91, B: 203, A: uint8(255 - alpha)}
			fast.Set(3, 4, base)
			slow.Set(3, 4, base)
			src := color.NRGBA{R: 173, G: uint8(channel), B: 41, A: uint8(alpha)}
			blendPixel(fast.SubImage(image.Rect(1, 2, 8, 9)).(*image.RGBA), 3, 4, src)
			blendPixel(genericDestination{slow}, 3, 4, src)
			if fast.RGBAAt(3, 4) != slow.RGBAAt(3, 4) {
				t.Fatalf("alpha %d channel %d changed rounding", alpha, channel)
			}
		}
	}
}

func TestOptimizedPainterThemeReplacementAndClose(t *testing.T) {
	first, _ := skin.Builtin("advanced")
	next, _ := skin.Builtin("hologram")
	theme, err := ThemeFromSkin(first)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPainter(theme)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	canvas := image.NewRGBA(image.Rect(0, 0, 230, 55))
	drawButton := func() {
		if err := p.DrawButton(canvas, Control{Bounds: canvas.Bounds(), Label: "SKIN REPLACEMENT", State: State{Focused: true}}); err != nil {
			t.Fatal(err)
		}
	}
	drawButton()
	before := append([]byte(nil), canvas.Pix...)
	nextTheme, err := ThemeFromSkin(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetTheme(nextTheme); err != nil {
		t.Fatal(err)
	}
	clear(canvas.Pix)
	drawButton()
	if bytes.Equal(before, canvas.Pix) {
		t.Fatal("new theme reused old visual state")
	}
	reference, err := NewPainter(nextTheme)
	if err != nil {
		t.Fatal(err)
	}
	defer reference.Close()
	want := image.NewRGBA(canvas.Bounds())
	if err := reference.DrawButton(want, Control{Bounds: want.Bounds(), Label: "SKIN REPLACEMENT", State: State{Focused: true}}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want.Pix, canvas.Pix) {
		t.Fatal("theme replacement differs from fresh painter")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.DrawLabel(canvas, canvas.Bounds(), "closed", LabelStyle{}); err == nil {
		t.Fatal("closed painter remained active")
	}
	if err := p.SetTheme(nextTheme); err != nil {
		t.Fatal(err)
	}
	clear(canvas.Pix)
	drawButton()
	if !bytes.Equal(want.Pix, canvas.Pix) {
		t.Fatal("reopened painter retained stale resources")
	}
}
