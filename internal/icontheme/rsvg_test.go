package icontheme

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRasterRSVGOrFallback(t *testing.T) {
	dir := t.TempDir()
	// Path-heavy breeze-style icon: simple raster may miss curves; rsvg
	// should fill. Either path must produce a non-empty 16×16 buffer.
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16">
  <circle cx="8" cy="8" r="6" fill="#2244ff"/>
</svg>`
	p := filepath.Join(dir, "circ.svg")
	if err := os.WriteFile(p, []byte(svg), 0o644); err != nil {
		t.Fatal(err)
	}
	pix, w, h, st, err := LoadBGRASize(p, 16)
	if err != nil || w != 16 || h != 16 || st != 64 || len(pix) != 16*64 {
		t.Fatalf("load %v %d %d %d", err, w, h, st)
	}
	i := 8*st + 8*4
	if pix[i+0] < 100 && pix[i+2] < 100 {
		t.Fatalf("center not filled BGRA %v rsvg=%v", pix[i:i+4], RsvgAvailable())
	}
	if RsvgAvailable() {
		rpix, rw, rh, _, err := RasterRSVG(p, 16)
		if err != nil || rw != 16 || rh != 16 {
			t.Fatalf("RasterRSVG %v %d %d", err, rw, rh)
		}
		if rpix[i+0] < 100 {
			t.Fatalf("rsvg center %v", rpix[i:i+4])
		}
	}
}
