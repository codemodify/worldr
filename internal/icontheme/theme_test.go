package icontheme

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePNG(t *testing.T, path string, r, g, b, a byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: r, G: g, B: b, A: a})
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestResolveHicolorAndMissing(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "icons/hicolor/48x48/apps/foot.png"), 10, 20, 30, 255)
	s := Search{Theme: "hicolor", Dirs: []string{root}, Want: 16}
	p, ok := Resolve("foot", s)
	if !ok || filepath.Base(p) != "foot.png" {
		t.Fatalf("hicolor foot: %q %v", p, ok)
	}
	if _, ok := Resolve("no-such-icon-xyz", s); ok {
		t.Fatal("missing must fail")
	}
	if _, ok := Resolve("", s); ok {
		t.Fatal("empty")
	}
	if _, ok := Resolve("foot.svg", s); !ok {
		t.Fatal("strip .svg and find png")
	}
}

func TestResolvePrefersCurrentTheme(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "icons/hicolor/48x48/apps/kate.png"), 1, 0, 0, 255)
	writePNG(t, filepath.Join(root, "icons/breeze/24x24/apps/kate.png"), 0, 1, 0, 255)
	s := Search{Theme: "breeze", Dirs: []string{root}, Want: 24}
	p, ok := Resolve("kate", s)
	if !ok || !strings.Contains(p, "breeze") {
		t.Fatalf("want breeze, got %q", p)
	}
}

func TestResolveClosestSize(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "icons/hicolor/16x16/apps/x.png"), 1, 0, 0, 255)
	writePNG(t, filepath.Join(root, "icons/hicolor/48x48/apps/x.png"), 0, 1, 0, 255)
	p, ok := Resolve("x", Search{Theme: "hicolor", Dirs: []string{root}, Want: 24})
	if !ok || !strings.Contains(p, "16x16") {
		t.Fatalf("24 want → closer 16 than 48: %q", p)
	}
}

func TestResolveAbsoluteAndPixmaps(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "custom.png")
	writePNG(t, abs, 9, 8, 7, 255)
	p, ok := Resolve(abs, Search{Dirs: []string{root}})
	if !ok || p != abs {
		t.Fatalf("abs %q %v", p, ok)
	}
	writePNG(t, filepath.Join(root, "pixmaps/term.png"), 2, 2, 2, 255)
	p, ok = Resolve("term", Search{Theme: "hicolor", Dirs: []string{root}, Want: 16})
	if !ok || !strings.Contains(p, "pixmaps") {
		t.Fatalf("pixmaps fallback: %q", p)
	}
}

func TestLoadBGRAAndLookup(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "icons/hicolor/16x16/apps/dot.png"), 255, 0, 0, 255)
	s := Search{Theme: "hicolor", Dirs: []string{root}, Want: 16}
	pix, w, h, st, ok := Lookup("dot", s)
	if !ok || w != 1 || h != 1 || st != 4 || pix[2] != 255 || pix[0] != 0 {
		t.Fatalf("red → BGRA %+v ok=%v", pix, ok)
	}
	// cache hit
	pix2, _, _, _, ok2 := Lookup("dot", s)
	if !ok2 || pix2[2] != 255 {
		t.Fatal("cache")
	}
	if _, _, _, _, ok := Lookup("missing", s); ok {
		t.Fatal("missing lookup")
	}
}

func TestResolveAndRasterSVG(t *testing.T) {
	root := t.TempDir()
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><rect x="0" y="0" width="16" height="16" fill="#ff0000"/></svg>`
	p := filepath.Join(root, "icons/hicolor/scalable/apps/red.svg")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(svg), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Search{Theme: "hicolor", Dirs: []string{root}, Want: 16}
	got, ok := Resolve("red", s)
	if !ok || !strings.HasSuffix(got, "red.svg") {
		t.Fatalf("svg resolve %q %v", got, ok)
	}
	pix, w, h, st, ok := Lookup("red", s)
	if !ok || w != 16 || h != 16 || st != 64 {
		t.Fatalf("raster %v %d %d %d", ok, w, h, st)
	}
	// center pixel is red → BGRA
	i := 8*st + 8*4
	if pix[i+2] < 200 || pix[i+0] > 40 {
		t.Fatalf("center BGRA %v", pix[i:i+4])
	}
}

func TestRasterSVGPath(t *testing.T) {
	raw := []byte(`<svg viewBox="0 0 8 8"><path d="M0 0 L8 0 L8 8 L0 8 Z" fill="#00ff00"/></svg>`)
	pix, w, h, _, err := rasterSVGBytes(raw, 8)
	if err != nil || w != 8 || h != 8 {
		t.Fatalf("%v %d %d", err, w, h)
	}
	if pix[1] < 200 || pix[2] > 40 {
		t.Fatalf("path fill %v", pix[:4])
	}
}

func TestRasterSVGEmptyFails(t *testing.T) {
	if _, _, _, _, err := rasterSVGBytes([]byte(`<svg viewBox="0 0 8 8"></svg>`), 8); err == nil {
		t.Fatal("empty svg")
	}
}

func TestPNGWinsOverSVG(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "icons/hicolor/16x16/apps/both.png"), 0, 0, 255, 255)
	svg := `<svg viewBox="0 0 16 16"><rect width="16" height="16" fill="#00ff00"/></svg>`
	p := filepath.Join(root, "icons/hicolor/scalable/apps/both.svg")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(svg), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := Resolve("both", Search{Theme: "hicolor", Dirs: []string{root}, Want: 16})
	if !ok || !strings.HasSuffix(got, "both.png") {
		t.Fatalf("png first %q", got)
	}
}

func TestThemeName(t *testing.T) {
	t.Setenv("XDG_ICON_THEME", "breeze")
	t.Setenv("XDG_CURRENT_DESKTOP", "GNOME")
	if ThemeName() != "breeze" {
		t.Fatal("env wins")
	}
	t.Setenv("XDG_ICON_THEME", "")
	t.Setenv("ICON_THEME", "")
	t.Setenv("XDG_CURRENT_DESKTOP", "KDE")
	if ThemeName() != "breeze" {
		t.Fatal("plasma default")
	}
	t.Setenv("XDG_CURRENT_DESKTOP", "")
	if ThemeName() != "hicolor" {
		t.Fatal("hicolor default")
	}
}
