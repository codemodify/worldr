// Package icontheme resolves freedesktop icon names to PNG or SVG files.
//
// Search is png-first under the current theme, then each Inherits=
// theme from index.theme, then hicolor. Missing PNG falls back to SVG
// (librsvg when built with -tags=librsvg, else the simple raster).
// Absolute Icon= paths are used as-is when they exist.
package icontheme

import (
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// DefaultWant is the SSD / panel icon edge.
const DefaultWant = 16

// Search is one lookup (tests pass isolated dirs).
type Search struct {
	Theme string
	Dirs  []string // XDG data roots (…/share), not …/share/icons
	Want  int
}

// DataDirs is $XDG_DATA_HOME then $XDG_DATA_DIRS (default /usr/local/share:/usr/share).
func DataDirs() []string {
	var dirs []string
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		dirs = append(dirs, d)
	} else if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, ".local/share"))
	}
	data := os.Getenv("XDG_DATA_DIRS")
	if data == "" {
		data = "/usr/local/share:/usr/share"
	}
	for _, d := range strings.Split(data, ":") {
		d = strings.TrimSpace(d)
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// ThemeName is $XDG_ICON_THEME, else a desktop-ish default, else hicolor.
func ThemeName() string {
	if v := strings.TrimSpace(os.Getenv("XDG_ICON_THEME")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("ICON_THEME")); v != "" {
		return v
	}
	desk := strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP"))
	switch {
	case strings.Contains(desk, "kde") || strings.Contains(desk, "plasma"):
		return "breeze"
	case strings.Contains(desk, "gnome"):
		return "Adwaita"
	}
	return "hicolor"
}

// Default is the process XDG search (theme + data dirs).
func Default() Search {
	return Search{Theme: ThemeName(), Dirs: DataDirs(), Want: DefaultWant}
}

type decoded struct {
	pix          []byte
	w, h, stride int
	ok           bool
}

var (
	cacheMu sync.Mutex
	cache   = map[string]decoded{}
)

// Resolve returns a PNG (or existing absolute) path for name.
func Resolve(name string, s Search) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	if filepath.IsAbs(name) || strings.ContainsRune(name, '/') {
		if fileOK(name) {
			return name, true
		}
		if filepath.Ext(name) == "" {
			if p := name + ".png"; fileOK(p) {
				return p, true
			}
			if p := name + ".svg"; fileOK(p) {
				return p, true
			}
		}
		return "", false
	}
	name = stripIconExt(name)
	want := s.Want
	if want < 1 {
		want = DefaultWant
	}
	themes := InheritChain(s.Theme, s.Dirs)

	sizes := []int{16, 22, 24, 32, 36, 48, 64, 96, 128, 256}
	contexts := []string{"apps", "places", "devices", "categories", "mimetypes", "status", "actions"}

	best := ""
	bestD := 1 << 30
	for _, th := range themes {
		for _, dir := range s.Dirs {
			base := filepath.Join(dir, "icons", th)
			for _, sz := range sizes {
				for _, ctx := range contexts {
					p := filepath.Join(base, strconv.Itoa(sz)+"x"+strconv.Itoa(sz), ctx, name+".png")
					if !fileOK(p) {
						continue
					}
					d := sz - want
					if d < 0 {
						d = -d
					}
					if d < bestD {
						best, bestD = p, d
						if d == 0 {
							return p, true
						}
					}
				}
			}
		}
	}
	if best != "" {
		return best, true
	}
	if p, ok := resolveSVG(name, s, themes, contexts); ok {
		return p, true
	}
	for _, dir := range s.Dirs {
		p := filepath.Join(dir, "pixmaps", name+".png")
		if fileOK(p) {
			return p, true
		}
		p = filepath.Join(dir, "pixmaps", name+".svg")
		if fileOK(p) {
			return p, true
		}
	}
	return "", false
}

func resolveSVG(name string, s Search, themes, contexts []string) (string, bool) {
	for _, th := range themes {
		for _, dir := range s.Dirs {
			base := filepath.Join(dir, "icons", th)
			for _, ctx := range contexts {
				p := filepath.Join(base, "scalable", ctx, name+".svg")
				if fileOK(p) {
					return p, true
				}
			}
			for _, sz := range []int{16, 22, 24, 32, 48, 64} {
				for _, ctx := range contexts {
					p := filepath.Join(base, strconv.Itoa(sz)+"x"+strconv.Itoa(sz), ctx, name+".svg")
					if fileOK(p) {
						return p, true
					}
				}
			}
		}
	}
	return "", false
}

func stripIconExt(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".png" || ext == ".svg" || ext == ".xpm" {
		return strings.TrimSuffix(name, filepath.Ext(name))
	}
	return name
}

func fileOK(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// LoadBGRA decodes a PNG (or rasters an SVG) into BGRA8.
func LoadBGRA(path string) (pix []byte, w, h, stride int, err error) {
	return LoadBGRASize(path, DefaultWant)
}

// LoadBGRASize decodes path; SVG is rasterized to want×want.
func LoadBGRASize(path string, want int) (pix []byte, w, h, stride int, err error) {
	if strings.EqualFold(filepath.Ext(path), ".svg") {
		if RsvgAvailable() {
			if pix, w, h, st, err := RasterRSVG(path, want); err == nil && w > 0 && h > 0 {
				return pix, w, h, st, nil
			}
		}
		return RasterSVG(path, want)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	b := img.Bounds()
	w, h = b.Dx(), b.Dy()
	stride = w * 4
	out := make([]byte, stride*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := y*stride + x*4
			out[i+0] = byte(bl >> 8)
			out[i+1] = byte(g >> 8)
			out[i+2] = byte(r >> 8)
			out[i+3] = byte(a >> 8)
		}
	}
	return out, w, h, stride, nil
}

// Lookup PNG-decodes Resolve(name). Cached per name+want+theme+dirs.
func Lookup(name string, s Search) (pix []byte, w, h, stride int, ok bool) {
	key := s.Theme + "\x00" + strings.Join(s.Dirs, "\x00") + "\x00" + name + "\x00" + strconv.Itoa(s.Want)
	cacheMu.Lock()
	if c, hit := cache[key]; hit {
		cacheMu.Unlock()
		return c.pix, c.w, c.h, c.stride, c.ok
	}
	cacheMu.Unlock()
	path, found := Resolve(name, s)
	var d decoded
	if found {
		if pix, w, h, st, err := LoadBGRASize(path, s.Want); err == nil && w > 0 && h > 0 {
			d = decoded{pix: pix, w: w, h: h, stride: st, ok: true}
		}
	}
	cacheMu.Lock()
	cache[key] = d
	cacheMu.Unlock()
	return d.pix, d.w, d.h, d.stride, d.ok
}
