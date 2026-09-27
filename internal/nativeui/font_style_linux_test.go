//go:build linux && cgo

package nativeui

import (
	"bytes"
	"image"
	"testing"
)

func TestPangoFontDescriptionAppliesBoldAndItalic(t *testing.T) {
	paint := func(description string) []byte {
		t.Helper()
		layout, err := newTextLayout("MMMM ffff", description, 30, -1)
		if err != nil {
			t.Fatal(err)
		}
		defer layout.close()
		mask := image.NewAlpha(image.Rect(0, 0, 256, 64))
		if err := layout.draw(mask, image.Pt(4, 4)); err != nil {
			t.Fatal(err)
		}
		return mask.Pix
	}
	regular, bold, italic, both := paint("Monospace"), paint("Monospace Bold"), paint("Monospace Italic"), paint("Monospace Bold Italic")
	if bytes.Equal(regular, bold) || bytes.Equal(regular, italic) || bytes.Equal(bold, both) || bytes.Equal(italic, both) {
		t.Fatal("font description style was interpreted as part of a literal family name")
	}
	weight := func(pixels []byte) int {
		total := 0
		for _, pixel := range pixels {
			total += int(pixel)
		}
		return total
	}
	if weight(bold) <= weight(regular) {
		t.Fatal("bold font did not increase stroke coverage")
	}
}
