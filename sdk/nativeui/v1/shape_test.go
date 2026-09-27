package nativeui

import (
	"crypto/sha256"
	"image"
	"image/color"
	"testing"
)

func TestShapeGrammarsAreDistinctFilledAndStrictlyClipped(t *testing.T) {
	seen := map[[32]byte]ShapeGrammar{}
	for _, grammar := range ShapeGrammars() {
		canvas := image.NewRGBA(image.Rect(0, 0, 80, 50))
		marker := color.RGBA{R: 3, G: 5, B: 7, A: 255}
		for y := 0; y < canvas.Bounds().Dy(); y++ {
			for x := 0; x < canvas.Bounds().Dx(); x++ {
				canvas.SetRGBA(x, y, marker)
			}
		}
		bounds := image.Rect(13, 9, 67, 41)
		spec := ShapeSpec{Grammar: grammar, Corner: 7, Notch: 8, Stroke: 2}
		if err := FillShape(canvas, bounds, spec, color.RGBA{R: 40, G: 170, B: 210, A: 210}); err != nil {
			t.Fatal(err)
		}
		if err := StrokeShape(canvas, bounds, spec, color.RGBA{R: 210, G: 250, B: 255, A: 255}); err != nil {
			t.Fatal(err)
		}
		painted := 0
		for y := 0; y < canvas.Bounds().Dy(); y++ {
			for x := 0; x < canvas.Bounds().Dx(); x++ {
				changed := canvas.RGBAAt(x, y) != marker
				if changed {
					painted++
					if !image.Pt(x, y).In(bounds) {
						t.Fatalf("%q painted outside bounds at %d,%d", grammar, x, y)
					}
				}
			}
		}
		if painted == 0 {
			t.Fatalf("%q painted no pixels", grammar)
		}
		signature := sha256.Sum256(canvas.Pix)
		if previous, duplicate := seen[signature]; duplicate {
			t.Fatalf("%q and %q produced identical silhouettes", previous, grammar)
		}
		seen[signature] = grammar
	}
}

func TestShapesClipAtDestinationAndRejectInvalidSpecs(t *testing.T) {
	canvas := image.NewRGBA(image.Rect(10, 20, 50, 60))
	if err := FillShape(canvas, image.Rect(-200, -200, 30, 40), ShapeSpec{Grammar: Notched, Corner: 5, Notch: 5, Stroke: 1}, color.White); err != nil {
		t.Fatal(err)
	}
	if canvas.RGBAAt(15, 25).A == 0 {
		t.Fatal("partially visible shape did not paint the non-zero-origin destination")
	}
	before := append([]byte(nil), canvas.Pix...)
	if err := StrokeShape(canvas, canvas.Bounds(), ShapeSpec{Grammar: "broken", Stroke: 1}, color.White); err == nil {
		t.Fatal("accepted an invalid shape grammar")
	}
	if string(before) != string(canvas.Pix) {
		t.Fatal("invalid shape changed the destination")
	}
}
