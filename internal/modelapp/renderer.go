package modelapp

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/render"
)

type renderer struct {
	image          *image.RGBA
	texture        *render.Texture
	painter        *nativeui.Painter
	scaleX, scaleY float32
}

func newRenderer(width, height int) (*renderer, error) {
	painter, err := nativeui.NewPainter(nativeui.Cinematic())
	if err != nil {
		return nil, err
	}
	r := &renderer{painter: painter}
	if err := r.resize(width, height); err != nil {
		painter.Close()
		return nil, err
	}
	return r, nil
}
func (r *renderer) close() { r.painter.Close() }
func (r *renderer) resize(w, h int) error {
	if r.image != nil && r.image.Rect.Dx() == w && r.image.Rect.Dy() == h {
		return nil
	}
	r.image = image.NewRGBA(image.Rect(0, 0, w, h))
	r.scaleX, r.scaleY = float32(w)/960, float32(h)/600
	if r.texture == nil {
		texture, err := render.NewTexture(w, h, r.image.Pix)
		r.texture = texture
		return err
	}
	return r.texture.Replace(w, h, r.image.Pix)
}
func rgba(value uint32) color.RGBA {
	return color.RGBA{R: byte(value >> 16), G: byte(value >> 8), B: byte(value), A: 255}
}
func (r *renderer) rect(x, y, w, h int, c uint32) {
	rect := image.Rect(int(float32(x)*r.scaleX), int(float32(y)*r.scaleY), int(float32(x+w)*r.scaleX), int(float32(y+h)*r.scaleY))
	draw.Draw(r.image, rect, image.NewUniform(rgba(c)), image.Point{}, draw.Src)
}
func safeLabel(text string) string {
	return strings.Map(func(c rune) rune {
		if unicode.IsControl(c) || unicode.Is(unicode.Cf, c) {
			return ' '
		}
		return c
	}, text)
}
func (r *renderer) text(x, y int, text string, c uint32) {
	_ = r.painter.DrawLabel(r.image, r.bounds(x, y-17, 960-x, 23), safeLabel(text), rgba(c))
}
func short(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
func (r *renderer) draw(v *viewer) error {
	r.rect(0, 0, 960, 600, 0x081822)
	r.rect(0, 0, 960, 39, 0x102f3c)
	r.rect(0, 39, 960, 2, 0x56c6df)
	r.text(18, 26, "MODEL / "+short(filepath.Base(v.state.ResourcePath()), 64), 0xb3ebf4)
	for _, node := range v.controls.Semantics().Nodes {
		if node.Role == nativeui.RoleButton {
			_ = r.painter.DrawButton(r.image, node, v.controls.FocusedID() == node.ID || node.Selected, false)
		}
	}
	for x := 194; x < 945; x += 42 {
		r.rect(x, 105, 1, 385, 0x0d2532)
	}
	for y := 105; y < 490; y += 42 {
		r.rect(194, y, 750, 1, 0x0d2532)
	}
	r.rect(14, 104, 168, 388, 0x0e2330)
	r.text(24, 126, "COMPONENTS", 0x69ccdf)
	if v.loading {
		r.text(270, 130, "Loading model…", 0x88b7ca)
	}
	if v.model != nil {
		for i := v.componentTop; i < len(v.model.Components) && i < v.componentTop+13; i++ {
			c := v.model.Components[i]
			row := i - v.componentTop
			if i == v.state.View.Selected {
				r.rect(19, 134+row*26, 159, 25, 0x244553)
			}
			r.text(25, 153+row*26, short(c.Name, 17), 0xa6cad7)
		}
		if len(v.model.Components) > 13 {
			r.text(25, 482, fmt.Sprintf("%d–%d / %d  ↕", v.componentTop+1, min(v.componentTop+13, len(v.model.Components)), len(v.model.Components)), 0x779aaa)
		}
		c := v.model.Components[v.state.View.Selected]
		r.text(204, 510, fmt.Sprintf("%s  /  %d triangles   ·   %d total", short(c.Name, 30), c.Triangles, v.model.Triangles), 0xa3d9e6)
		size := v.model.Maximum.Sub(v.model.Minimum)
		r.text(204, 532, fmt.Sprintf("EXTENT  %.5g × %.5g × %.5g %s", size.X, size.Y, size.Z, v.state.View.Units), 0x81adbf)
		if n := len(v.state.View.Measurements); n > 0 {
			r.text(24, 559, fmt.Sprintf("MEASURE %d / %.6g %s", n, v.state.View.Measurements[n-1].Distance, v.state.View.Units), 0x88e5ec)
		}
	}
	if v.editing {
		r.rect(193, 548, 750, 42, 0x204453)
		_ = r.painter.DrawField(r.image, r.bounds(204, 550, 730, 38), v.note, "Enter to save note", v.focused)
	} else {
		r.text(24, 584, short(v.message, 106), 0xe2c58e)
	}
	r.text(202, 103, "DRAG TO ORBIT  ·  SCROLL TO ZOOM  ·  CTRL+S SAVES NOTES", 0x587f92)
	return r.texture.Replace(r.image.Rect.Dx(), r.image.Rect.Dy(), r.image.Pix)
}

func (r *renderer) bounds(x, y, w, h int) image.Rectangle {
	return image.Rect(int(float32(x)*r.scaleX), int(float32(y)*r.scaleY), int(float32(x+w)*r.scaleX), int(float32(y+h)*r.scaleY))
}
