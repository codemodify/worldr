package nativeapps

import (
	"bytes"
	"image"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/terminal"
)

func testTerminalSnapshot(cols, rows int) terminal.Snapshot {
	s := terminal.Snapshot{Cols: cols, Rows: rows, Cells: make([]terminal.Cell, cols*rows), Revision: 1, Title: "test shell"}
	for i := range s.Cells {
		s.Cells[i] = terminal.Cell{Width: 1, Foreground: terminal.Color{R: 220, G: 230, B: 240}, Background: terminal.Color{R: 8, G: 15, B: 22}}
	}
	return s
}

func TestTerminalRowsDamageAndRetainUnchangedContent(t *testing.T) {
	r, err := newTerminalRenderer(324, 246)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	s := testTerminalSnapshot(30, 8)
	if err := r.paint(s, terminalDecoration{}); err != nil {
		t.Fatal(err)
	}
	initial := r.texture.Revision()
	if err := r.paint(s, terminalDecoration{}); err != nil || r.texture.Revision() != initial {
		t.Fatal("unchanged terminal generated a texture upload", err)
	}
	changed := s
	changed.Cells = append([]terminal.Cell(nil), s.Cells...)
	changed.Cells[3*30+4].Chars[0] = 'A'
	changed.Revision++
	if err := r.paint(changed, terminalDecoration{}); err != nil {
		t.Fatal(err)
	}
	upload, ok := r.texture.Snapshot(initial)
	if !ok || upload.Rect != image.Rect(contentLeft, contentTop+3*cellHeight, contentLeft+30*cellWidth, contentTop+4*cellHeight) {
		t.Fatalf("single row update damaged %v", upload.Rect)
	}
	if s.Cells[3*30+4].Chars[0] != 0 {
		t.Fatal("renderer modified the retained backend snapshot")
	}
	colored := false
	for y := contentTop + 3*cellHeight; y < contentTop+4*cellHeight; y++ {
		for x := contentLeft + 4*cellWidth; x < contentLeft+5*cellWidth; x++ {
			if r.image.RGBAAt(x, y).R > 100 {
				colored = true
			}
		}
	}
	if !colored {
		t.Fatal("real cell glyph was not rasterized")
	}
}

func TestTerminalColorsAttributesCursorAndWideCellClipping(t *testing.T) {
	r, err := newTerminalRenderer(244, 162)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	s := testTerminalSnapshot(22, 4)
	s.Cells[0].Background = terminal.Color{R: 170, G: 20, B: 40}
	s.Cells[1].Foreground = terminal.Color{R: 10, G: 160, B: 30}
	s.Cells[1].Reverse = true
	s.Cells[2].Chars = [6]rune{'e', '\u0301'}
	s.Cells[2].Bold = true
	s.Cells[2].Italic = true
	s.Cells[2].Underline = true
	s.Cells[3].Chars[0] = 'W'
	s.Cells[3].Width = 2
	s.Cells[4].Width = 0
	s.Cursor = terminal.Cursor{Row: 1, Col: 2, Visible: true, Shape: 3}
	decoration := terminalDecoration{Focused: true, CursorOn: true, Selection: selection{Anchor: 5, End: 6, Active: true}}
	if err := r.paint(s, decoration); err != nil {
		t.Fatal(err)
	}
	if got := r.image.RGBAAt(contentLeft+1, contentTop+1); got.R != 170 || got.G != 20 || got.B != 40 {
		t.Fatalf("terminal background color changed: %v", got)
	}
	if got := r.image.RGBAAt(contentLeft+cellWidth+1, contentTop+1); got.G != 160 {
		t.Fatalf("reverse attribute did not swap colors: %v", got)
	}
	if got := r.image.RGBAAt(contentLeft+5*cellWidth+1, contentTop+1); got != rgb(0x285d71) {
		t.Fatalf("selection background missing: %v", got)
	}
	if got := r.image.RGBAAt(contentLeft+2*cellWidth, contentTop+cellHeight+1); got != rgb(0x83e6f1) {
		t.Fatalf("bar cursor missing: %v", got)
	}
	// Combining and wide-cell rasterization must stay inside the content grid.
	if got := r.image.RGBAAt(contentLeft-1, contentTop+3); got != rgb(0x101c26) {
		t.Fatalf("glyph escaped left content edge: %v", got)
	}
	before := r.texture.Revision()
	decoration.CursorOn = false
	if err := r.paint(s, decoration); err != nil {
		t.Fatal(err)
	}
	upload, _ := r.texture.Snapshot(before)
	if upload.Rect.Min.Y != contentTop+cellHeight || upload.Rect.Max.Y != contentTop+2*cellHeight {
		t.Fatalf("cursor blink redrew unrelated rows: %v", upload.Rect)
	}
	if got := r.image.RGBAAt(contentLeft+2*cellWidth, contentTop+cellHeight+1); got == rgb(0x83e6f1) {
		t.Fatal("cursor blink did not restore cell background")
	}
}

func TestTerminalRendererRejectsMismatchedGridAndPreservesTextureIdentity(t *testing.T) {
	r, err := newTerminalRenderer(324, 246)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	s := testTerminalSnapshot(30, 8)
	if err := r.paint(s, terminalDecoration{}); err != nil {
		t.Fatal(err)
	}
	id := r.texture.ID()
	bad := s
	bad.Cells = bad.Cells[:3]
	if err := r.paint(bad, terminalDecoration{}); err == nil {
		t.Fatal("short cell buffer accepted")
	}
	bad = testTerminalSnapshot(31, 8)
	if err := r.paint(bad, terminalDecoration{}); err == nil {
		t.Fatal("cell grid extending into chrome accepted")
	}
	if err := r.resize(424, 288); err != nil {
		t.Fatal(err)
	}
	s = testTerminalSnapshot(40, 10)
	if err := r.paint(s, terminalDecoration{}); err != nil {
		t.Fatal(err)
	}
	if r.texture.ID() != id {
		t.Fatal("terminal resize replaced retained texture identity")
	}
}

func TestTerminalChromeClipsAtMinimumWidthAndRetainsCorrectDamage(t *testing.T) {
	r, err := newTerminalRenderer(minWidth, minHeight)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	cols, rows := terminalGrid(minWidth, minHeight)
	s := testTerminalSnapshot(cols, rows)
	decoration := terminalDecoration{}
	if err := r.paint(s, decoration); err != nil {
		t.Fatal(err)
	}
	content := append([]byte(nil), r.image.Pix[contentTop*r.image.Stride:(minHeight-footerHeight)*r.image.Stride]...)
	for _, change := range []func(){
		func() { s.Title = strings.Repeat("long title λ\n\t", 100) },
		func() { s.MouseTracking = true },
		func() { decoration.Focused = true },
		func() { s.Exited, s.ExitError = true, strings.Repeat("long process error λ\n", 100) },
	} {
		before := r.texture.Revision()
		prior, _ := r.texture.Snapshot(0)
		change()
		if err := r.paint(s, decoration); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(content, r.image.Pix[contentTop*r.image.Stride:(minHeight-footerHeight)*r.image.Stride]) {
			t.Fatal("chrome or a long label changed the terminal cell area")
		}
		update, ok := r.texture.Snapshot(before)
		if !ok {
			t.Fatal("changed terminal chrome was not uploaded")
		}
		for y := update.Rect.Min.Y; y < update.Rect.Max.Y; y++ {
			start := (y*prior.Width + update.Rect.Min.X) * 4
			row := (y - update.Rect.Min.Y) * update.Rect.Dx() * 4
			copy(prior.Pixels[start:start+update.Rect.Dx()*4], update.Pixels[row:row+update.Rect.Dx()*4])
		}
		if !bytes.Equal(prior.Pixels, r.image.Pix) {
			t.Fatal("chrome changed pixels outside its reported texture damage")
		}
	}
}
