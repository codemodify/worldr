package researchapp

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"path/filepath"
	"strings"

	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/render"
)

const baseWidth, baseHeight = 1120, 700

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

func (r *renderer) resize(width, height int) error {
	if width < 720 || height < 440 || width > 4096 || height > 4096 {
		return fmt.Errorf("research dashboard must be between 720x440 and 4096x4096")
	}
	if r.image != nil && r.image.Rect.Dx() == width && r.image.Rect.Dy() == height {
		return nil
	}
	r.image = image.NewRGBA(image.Rect(0, 0, width, height))
	r.scaleX, r.scaleY = float32(width)/baseWidth, float32(height)/baseHeight
	if r.texture == nil {
		texture, err := render.NewTexture(width, height, r.image.Pix)
		r.texture = texture
		return err
	}
	return r.texture.Replace(width, height, r.image.Pix)
}

func researchColor(value uint32) color.RGBA {
	return color.RGBA{R: byte(value >> 16), G: byte(value >> 8), B: byte(value), A: 255}
}

func (r *renderer) bounds(x, y, width, height int) image.Rectangle {
	return image.Rect(int(float32(x)*r.scaleX), int(float32(y)*r.scaleY), int(float32(x+width)*r.scaleX), int(float32(y+height)*r.scaleY))
}

func (r *renderer) rect(x, y, width, height int, value uint32) {
	draw.Draw(r.image, r.bounds(x, y, width, height), image.NewUniform(researchColor(value)), image.Point{}, draw.Src)
}

func (r *renderer) text(x, y, width int, text string, value uint32) {
	_ = r.painter.DrawLabel(r.image, r.bounds(x, y-18, width, 24), text, researchColor(value))
}

func chartValue(d *Dataset, column, row int) (float64, bool) {
	if d == nil || column < 0 || column >= len(d.Values) || row < 0 || row >= len(d.Rows) {
		return 0, false
	}
	value := d.Values[column][row]
	return value, !math.IsNaN(value) && !math.IsInf(value, 0)
}

func normalize(value, minimum, maximum float64) float64 {
	if maximum <= minimum {
		return .5
	}
	return max(0, min(1, (value-minimum)/(maximum-minimum)))
}

func (r *renderer) chart(v *viewer, bounds image.Rectangle) {
	d := v.dataset
	if d == nil || len(d.Rows) == 0 || v.state.View.X >= len(d.Columns) || v.state.View.Y >= len(d.Columns) {
		return
	}
	for i := 0; i <= 8; i++ {
		x := bounds.Min.X + i*bounds.Dx()/8
		draw.Draw(r.image, image.Rect(x, bounds.Min.Y, x+1, bounds.Max.Y), image.NewUniform(researchColor(0x173340)), image.Point{}, draw.Src)
	}
	for i := 0; i <= 5; i++ {
		y := bounds.Min.Y + i*bounds.Dy()/5
		draw.Draw(r.image, image.Rect(bounds.Min.X, y, bounds.Max.X, y+1), image.NewUniform(researchColor(0x173340)), image.Point{}, draw.Src)
	}
	last, hasLast := image.Point{}, false
	step := max(1, len(d.Rows)/1500)
	for row := 0; row < len(d.Rows); row += step {
		xValue, xOK := chartValue(d, v.state.View.X, row)
		yValue, yOK := chartValue(d, v.state.View.Y, row)
		if !xOK || !yOK {
			hasLast = false
			continue
		}
		x := bounds.Min.X + int(normalize(xValue, d.Minimum[v.state.View.X], d.Maximum[v.state.View.X])*float64(bounds.Dx()-1))
		y := bounds.Max.Y - 1 - int(normalize(yValue, d.Minimum[v.state.View.Y], d.Maximum[v.state.View.Y])*float64(bounds.Dy()-1))
		point := image.Pt(x, y)
		if v.state.View.Mode == "line" && hasLast {
			drawLine(r.image, last, point, researchColor(0x55bed1))
		}
		if v.state.View.Mode == "scatter" || row == v.state.View.Selected {
			radius, value := 2, uint32(0x72dce8)
			if row == v.state.View.Selected {
				radius, value = 5, 0xffcc7a
			}
			draw.Draw(r.image, image.Rect(x-radius, y-radius, x+radius+1, y+radius+1).Intersect(bounds), image.NewUniform(researchColor(value)), image.Point{}, draw.Src)
		}
		last, hasLast = point, true
	}
	if row := v.state.View.Selected; row >= 0 && row < len(d.Rows) {
		if xValue, ok := chartValue(d, v.state.View.X, row); ok {
			if yValue, ok := chartValue(d, v.state.View.Y, row); ok {
				x := bounds.Min.X + int(normalize(xValue, d.Minimum[v.state.View.X], d.Maximum[v.state.View.X])*float64(bounds.Dx()-1))
				y := bounds.Max.Y - 1 - int(normalize(yValue, d.Minimum[v.state.View.Y], d.Maximum[v.state.View.Y])*float64(bounds.Dy()-1))
				draw.Draw(r.image, image.Rect(x-5, y-5, x+6, y+6).Intersect(bounds), image.NewUniform(researchColor(0xffcc7a)), image.Point{}, draw.Src)
			}
		}
	}
}

func drawLine(dst *image.RGBA, a, b image.Point, c color.RGBA) {
	dx, dy := int(math.Abs(float64(b.X-a.X))), -int(math.Abs(float64(b.Y-a.Y)))
	sx, sy := -1, -1
	if a.X < b.X {
		sx = 1
	}
	if a.Y < b.Y {
		sy = 1
	}
	err := dx + dy
	for {
		if a.In(dst.Bounds()) {
			dst.SetRGBA(a.X, a.Y, c)
		}
		if a == b {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			a.X += sx
		}
		if e2 <= dx {
			err += dx
			a.Y += sy
		}
	}
}

func shortResearch(value string, limit int) string {
	value = strings.ReplaceAll(value, "\t", " ")
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return value
}

func (r *renderer) draw(v *viewer) error {
	r.rect(0, 0, baseWidth, baseHeight, 0x07151f)
	r.rect(0, 0, baseWidth, 42, 0x102e3a)
	r.rect(0, 40, baseWidth, 2, 0x5ed4e4)
	title := "RESEARCH / " + shortResearch(filepath.Base(v.state.Source), 72)
	r.text(18, 28, 650, title, 0xbceff4)
	for _, node := range v.controls.Semantics().Nodes {
		if node.Role == nativeui.RoleButton {
			_ = r.painter.DrawButton(r.image, node, v.controls.FocusedID() == node.ID, false)
		}
	}
	chart := r.bounds(300, 118, 796, 386)
	r.rect(300, 118, 796, 386, 0x091c27)
	r.chart(v, chart)
	r.text(312, 108, 760, "LINKED SIGNAL VIEW", 0x6dd2e2)
	r.rect(18, 118, 264, 386, 0x0c222e)
	r.text(30, 108, 230, "OBSERVATIONS", 0x6dd2e2)
	if v.loading {
		r.text(36, 150, 220, "Loading dataset…", 0x9bc3ce)
	}
	if v.dataset != nil {
		d := v.dataset
		start := max(0, min(len(d.Rows)-1, v.state.View.Selected-6))
		for row := start; row < len(d.Rows) && row < start+13; row++ {
			y := 128 + (row-start)*27
			if row == v.state.View.Selected {
				r.rect(24, y, 252, 25, 0x285264)
			}
			value := fmt.Sprintf("%05d", row+1)
			if v.state.View.Y < len(d.Rows[row]) {
				value += "  " + shortResearch(d.Rows[row][v.state.View.Y], 20)
			}
			r.text(32, y+19, 236, value, 0xb4d4dc)
		}
		x, y, z := v.state.View.X, v.state.View.Y, v.state.View.Z
		r.text(30, 532, 1060, fmt.Sprintf("ROWS %d   COLS %d   X %s   Y %s   Z %s", len(d.Rows), len(d.Columns), d.Columns[x], d.Columns[y], d.Columns[z]), 0x96cbd5)
		row := d.Rows[v.state.View.Selected]
		for column := 0; column < len(d.Columns) && column < 4; column++ {
			r.text(30+column*270, 574, 252, strings.ToUpper(shortResearch(d.Columns[column], 18)), 0x5f91a1)
			r.text(30+column*270, 600, 252, shortResearch(row[column], 26), 0xe0c98f)
		}
	}
	message := v.message
	if message == "" {
		message = "Pick a spatial point or table row · ↑/↓ selects · drag rotates · wheel changes depth"
	}
	r.text(30, 670, 1060, shortResearch(message, 130), 0x7fa8b5)
	return r.texture.Replace(r.image.Rect.Dx(), r.image.Rect.Dy(), r.image.Pix)
}
