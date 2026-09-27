package nativeapps

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/terminal"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func TestTerminalSkinRepaintsToolbarAndPreservesPTYCells(t *testing.T) {
	p, _ := testProvider(t)
	p.Resize(1, 960, 600)
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), p.renderer.image.Pix...)
	cells := p.renderer.image.SubImage(image.Rect(contentLeft, contentTop, contentLeft+p.snapshot.Cols*cellWidth, contentTop+p.snapshot.Rows*cellHeight)).(*image.RGBA)
	priorCells := image.NewRGBA(cells.Bounds())
	for y := cells.Rect.Min.Y; y < cells.Rect.Max.Y; y++ {
		copy(priorCells.Pix[(y-priorCells.Rect.Min.Y)*priorCells.Stride:][:priorCells.Rect.Dx()*4], cells.Pix[(y-cells.Rect.Min.Y)*cells.Stride:][:cells.Rect.Dx()*4])
	}
	selected, err := skin.Builtin("plasma")
	if err != nil {
		t.Fatal(err)
	}
	p.SetSkin(selected)
	if p.renderer.initialized {
		t.Fatal("skin did not invalidate cached chrome")
	}
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, p.renderer.image.Pix) {
		t.Fatal("terminal controls did not repaint")
	}
	for y := cells.Rect.Min.Y; y < cells.Rect.Max.Y; y++ {
		if !bytes.Equal(priorCells.Pix[(y-priorCells.Rect.Min.Y)*priorCells.Stride:][:priorCells.Rect.Dx()*4], cells.Pix[(y-cells.Rect.Min.Y)*cells.Stride:][:cells.Rect.Dx()*4]) {
			t.Fatal("skin changed terminal content pixels")
		}
	}
}

func TestFuturePanelsPreserveTerminalCoverageAndOpaqueText(t *testing.T) {
	p, backend := testProvider(t)
	for i := range backend.snapshot.Cells {
		backend.snapshot.Cells[i].Background = terminal.Color{R: 8, G: 16, B: 23}
		backend.snapshot.Cells[i].Foreground = terminal.Color{R: 220, G: 235, B: 238}
	}
	backend.snapshot.Cells[0].Chars[0] = 'M'
	backend.snapshot.Cells[0].Underline = true
	backend.snapshot.Cells[1].Background = terminal.Color{R: 170, G: 20, B: 40}
	backend.snapshot.Cells[2].Foreground = terminal.Color{R: 10, G: 160, B: 30}
	backend.snapshot.Cells[2].Reverse = true
	selected, err := skin.Builtin("future-panels")
	if err != nil {
		t.Fatal(err)
	}
	p.SetSkin(selected)
	if !p.renderer.panelField {
		t.Fatal("panel-field did not opt terminal content into alpha")
	}
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	r := p.renderer
	background := color.RGBAModel.Convert(selected.Color("background")).(color.RGBA)
	for _, point := range []image.Point{{1, contentTop + 4}, {contentLeft + 4*cellWidth + 1, contentTop + 1}, {1, 1}} {
		if got := r.image.RGBAAt(point.X, point.Y); got != background || got.A == 255 {
			t.Fatalf("default fill is not authored translucent background at %v: %v, want %v", point, got, background)
		}
	}
	text := color.RGBAModel.Convert(selected.Color("text")).(color.RGBA)
	if got := r.image.RGBAAt(contentLeft+1, contentTop+cellHeight-3); got != text || got.A != 255 {
		t.Fatalf("terminal text decoration lost opaque palette text: %v, want %v", got, text)
	}
	if got := r.image.RGBAAt(contentLeft+cellWidth+1, contentTop+1); got != rgb(0xaa1428) {
		t.Fatalf("ANSI background lost its color/coverage: %v", got)
	}
	if got := r.image.RGBAAt(contentLeft+2*cellWidth+1, contentTop+1); got != rgb(0x0aa01e) {
		t.Fatalf("inverse background lost its color/coverage: %v", got)
	}
	fg, bg := r.cellColors(backend.snapshot.Cells[3], true)
	if fg != rgb(0xebfcff) || bg != rgb(0x285d71) {
		t.Fatalf("selection lost opaque contrast: %v, %v", fg, bg)
	}
	for i := 0; i < len(r.image.Pix); i += 4 {
		if r.image.Pix[i] > r.image.Pix[i+3] || r.image.Pix[i+1] > r.image.Pix[i+3] || r.image.Pix[i+2] > r.image.Pix[i+3] {
			t.Fatalf("non-premultiplied pixel at byte %d: %v", i, r.image.Pix[i:i+4])
		}
	}
	before := r.texture.Revision()
	if err := p.Poll(); err != nil || r.texture.Revision() != before {
		t.Fatal("unchanged translucent terminal generated an upload", err)
	}
	other, err := skin.Builtin("plasma")
	if err != nil {
		t.Fatal(err)
	}
	p.SetSkin(other)
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	if r.panelField || r.image.RGBAAt(contentLeft+4*cellWidth+1, contentTop+1) != rgb(0x081017) || r.image.RGBAAt(1, contentTop+4) != rgb(0x101c26) {
		t.Fatal("leaving panel-field failed to restore original opaque rendering")
	}
}

func TestNewTerminalsInheritSkinAndLegacyPreferenceResetsIt(t *testing.T) {
	f := newManagerFixture(t, Options{})
	selected, err := skin.Builtin("plasma")
	if err != nil {
		t.Fatal(err)
	}
	f.manager.SetSkin(selected)
	launchNative(t, f.manager)
	first := f.manager.slots[0].provider.renderer.ui.Theme.Accent
	f.manager.SetControlTheme(experience.ControlTheme{Family: "glass", Shape: "slab"})
	launchNative(t, f.manager)
	current := f.manager.slots[0].provider.renderer.ui.Theme.Accent
	if current == first || current != f.manager.slots[1].provider.renderer.ui.Theme.Accent {
		t.Fatal("legacy selection failed to reset existing and future terminal controls")
	}
}
