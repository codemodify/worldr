package main

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"

	nativeui "github.com/codemodify/worldr/sdk/nativeui/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func (d *desktop) setPainters(selected skin.Skin) error {
	compact := selected.Clone()
	compact.Metrics.Padding = 3
	compact.Metrics.Gap = 3
	compact.Metrics.ControlHeight = 18
	compact.Metrics.Icon = 12
	for name, r := range compact.Controls {
		r.ContentInsets = skin.Insets{Top: 1, Right: 3, Bottom: 1, Left: 3}
		compact.Controls[name] = r
	}
	if selected.ID == "merrick" {
		for k, v := range map[string]skin.Color{"background": "#182a3b", "surface": "#243b53", "raised": "#465f7d", "hover": "#587697", "pressed": "#142638", "accent": "#b6cbe2", "accent-alt": "#8dadd0", "border": "#607d9f", "text": "#f1f3f8", "muted": "#afbdcf", "selection": "#537397", "disabled": "#5b6674"} {
			compact.Palette[k] = v
		}
	}
	next := map[int]*nativeui.Painter{}
	for _, size := range []int{9, 10, 11, 12, 14, 18} {
		copy := compact.Clone()
		copy.Typography.Size = float64(size)
		copy.Typography.LineHeight = float64(size + 3)
		theme, err := nativeui.ThemeFromSkin(copy)
		if err != nil {
			for _, p := range next {
				_ = p.Close()
			}
			return err
		}
		p, err := nativeui.NewPainter(theme)
		if err != nil {
			for _, p := range next {
				_ = p.Close()
			}
			return err
		}
		next[size] = p
	}
	for _, p := range d.painters {
		_ = p.Close()
	}
	d.painters = next
	return nil
}

type deskColors struct{ paper, panel, navy, blue, edge, ink, muted, onDark color.RGBA }

func rgb(v uint32) color.RGBA {
	return color.RGBA{R: byte(v >> 16), G: byte(v >> 8), B: byte(v), A: 255}
}
func (d *desktop) colors() deskColors {
	if d.selected.ID == "merrick" {
		return deskColors{rgb(0xc7cbd5), rgb(0xd9dce4), rgb(0x182a3b), rgb(0x465f7d), rgb(0x8b9db5), rgb(0x182332), rgb(0x657184), rgb(0xf0f2f8)}
	}
	c := func(token string) color.RGBA {
		v := color.RGBAModel.Convert(d.selected.Color(token)).(color.RGBA)
		v.A = 255
		return v
	}
	return deskColors{c("surface"), c("raised"), c("background"), c("border"), c("accent"), c("text"), c("muted"), c("text")}
}

type canvas struct {
	d       *desktop
	v       *surfaceView
	image   *image.RGBA
	p       deskColors
	sx, sy  float64
	originX float64
	err     error
}

func (c *canvas) r(x, y, w, h int) image.Rectangle {
	return image.Rect(int(math.Round((float64(x)-c.originX)*c.sx)), int(math.Round(float64(y)*c.sy)), int(math.Round((float64(x+w)-c.originX)*c.sx)), int(math.Round(float64(y+h)*c.sy))).Intersect(c.image.Bounds())
}
func (c *canvas) rect(x, y, w, h int, ink color.Color) {
	r := c.r(x, y, w, h)
	if !r.Empty() {
		draw.Draw(c.image, r, image.NewUniform(ink), image.Point{}, draw.Over)
	}
}
func (c *canvas) border(x, y, w, h int, ink color.Color) {
	c.rect(x, y, w, 1, ink)
	c.rect(x, y+h-1, w, 1, ink)
	c.rect(x, y, 1, h, ink)
	c.rect(x+w-1, y, 1, h, ink)
}
func (c *canvas) text(x, y, w, h, size int, value string, ink color.RGBA) {
	if c.err != nil {
		return
	}
	p := c.d.painters[size]
	if p == nil {
		p = c.d.painters[10]
	}
	c.err = p.DrawLabel(c.image, c.r(x, y, w, h), value, nativeui.LabelStyle{Color: ink})
}
func (c *canvas) center(x, y, w, h, size int, value string, ink color.RGBA) {
	if c.err != nil {
		return
	}
	p := c.d.painters[size]
	if p == nil {
		p = c.d.painters[10]
	}
	c.err = p.DrawLabel(c.image, c.r(x, y, w, h), value, nativeui.LabelStyle{Color: ink, Align: nativeui.AlignCenter})
}
func (c *canvas) vertical(x, y, w, h int, value string, ink color.RGBA) {
	b := c.r(x, y, w, h)
	if b.Empty() {
		return
	}
	label := image.NewRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
	if err := c.d.painters[11].DrawLabel(label, label.Bounds(), value, nativeui.LabelStyle{Color: ink}); err != nil {
		c.err = err
		return
	}
	rotated := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for yy := 0; yy < b.Dy(); yy++ {
		for xx := 0; xx < b.Dx(); xx++ {
			rotated.SetRGBA(xx, yy, label.RGBAAt(yy, b.Dx()-xx-1))
		}
	}
	draw.Draw(c.image, b, rotated, image.Point{}, draw.Over)
}
func (c *canvas) line(x0, y0, x1, y1 int, ink color.RGBA) {
	x0, y0 = int((float64(x0)-c.originX)*c.sx), int(float64(y0)*c.sy)
	x1, y1 = int((float64(x1)-c.originX)*c.sx), int(float64(y1)*c.sy)
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := -1, -1
	if x0 < x1 {
		sx = 1
	}
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		if image.Pt(x0, y0).In(c.image.Bounds()) {
			c.image.SetRGBA(x0, y0, ink)
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
func (c *canvas) circle(cx, cy, radius int, ink color.RGBA, fill bool) {
	for y := -radius; y <= radius; y++ {
		for x := -radius; x <= radius; x++ {
			dist := x*x + y*y
			if dist > radius*radius || !fill && dist < (radius-1)*(radius-1) {
				continue
			}
			c.rect(cx+x, cy+y, 1, 1, ink)
		}
	}
}
func (c *canvas) bevel(x, y, w, h int, fill color.RGBA) {
	c.rect(x, y, w, h, fill)
	c.line(x, y, x+w-1, y, c.p.edge)
	c.line(x, y, x, y+h-1, c.p.edge)
	c.line(x+1, y+h-1, x+w-1, y+h-1, c.p.navy)
	c.line(x+w-1, y+1, x+w-1, y+h-1, c.p.navy)
}
func (c *canvas) control(id string, kind nativeui.Kind, x, y, w, h int, label string, state nativeui.State, value float64) nativeui.Control {
	control := nativeui.Control{ID: id, Kind: kind, Bounds: c.r(x, y, w, h), Label: label, State: state, Value: value, Min: 0, Max: 100, Step: 1}
	if !control.Bounds.Empty() {
		c.v.controls = append(c.v.controls, control)
	}
	return c.v.controller.Decorate(control)
}
func (c *canvas) button(id string, x, y, w, h int, label string, selected bool) {
	control := c.control(id, nativeui.KindButton, x, y, w, h, label, nativeui.State{Selected: selected}, 0)
	if control.Bounds.Empty() {
		return
	}
	if err := c.d.painters[10].DrawButton(c.image, control); err != nil {
		c.err = err
	}
}
func (c *canvas) check(id string, x, y, w, h int, label string, value bool) {
	control := c.control(id, nativeui.KindCheckbox, x, y, w, h, label, nativeui.State{Selected: value}, 0)
	if control.Bounds.Empty() {
		return
	}
	control.Label = ""
	if err := c.d.painters[10].DrawCheckbox(c.image, control); err != nil {
		c.err = err
	}
	c.text(x+h+3, y, w-h-3, h, 10, label, c.p.ink)
}
func (c *canvas) slider(id string, x, y, w, h int, value float64) {
	control := c.control(id, nativeui.KindSlider, x, y, w, h, "", nativeui.State{}, value)
	if err := c.d.painters[10].DrawSlider(c.image, control); err != nil {
		c.err = err
	}
}
func (c *canvas) footer(labels ...string) {
	h, w := c.v.spec.height, c.v.spec.width
	x := int(c.originX)
	c.rect(0, h-22, w, 22, c.p.navy)
	c.bevel(x, h-22, min(w-x-35, 138), 18, c.p.blue)
	for i, label := range labels {
		c.text(x+6+i*42, h-22, 48, 17, 9, label, c.p.onDark)
	}
	for i := 0; i < 5; i++ {
		c.rect(x+14+i*16, h-5, 3, 4, c.p.edge)
	}
	c.line(w-45, h-22, w-25, h-22, c.p.edge)
	c.line(w-25, h-22, w-40, h-1, c.p.edge)
}
func (c *canvas) paragraph(x, y, w, size int, value string, maxLines int) int {
	words := strings.Fields(value)
	line := ""
	lines := 0
	limit := max(12, w/(size/2+1))
	flush := func() { c.text(x, y, w, size+4, size, line, c.p.ink); y += size + 4; lines++; line = "" }
	for _, word := range words {
		if len(line)+len(word)+1 > limit && line != "" {
			flush()
			if lines >= maxLines {
				return y
			}
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	if line != "" && lines < maxLines {
		flush()
	}
	return y
}
func (d *desktop) paint(v *surfaceView) error {
	if !v.live {
		return nil
	}
	img := v.pixels
	if img == nil || img.Bounds().Size() != image.Pt(v.width, v.height) {
		img = image.NewRGBA(image.Rect(0, 0, v.width, v.height))
	}
	v.controls = v.controls[:0]
	c := &canvas{d: d, v: v, image: img, p: d.colors(), sx: float64(v.width) / float64(v.spec.width), sy: float64(v.height) / float64(v.spec.height)}
	// The host owns each window's outer title tab. Reclaim the former 20px
	// client gutter while retaining one transform for rendering and hit targets.
	if v.spec.id == 1 || v.spec.id == 2 || v.spec.id == 6 || v.spec.id == 7 {
		c.originX = 20
		c.sx = float64(v.width) / float64(v.spec.width-20)
	}
	draw.Draw(img, img.Bounds(), image.NewUniform(c.p.paper), image.Point{}, draw.Src)
	switch v.spec.id {
	case 1:
		c.calendar()
	case 2:
		c.records()
	case 3, 4, 5:
		c.document()
	case 6:
		c.programs()
	case 7:
		c.messages()
	}
	if c.err != nil {
		return c.err
	}
	if err := v.controller.SetControlsWithin(v.controls, img.Bounds()); err != nil {
		return err
	}
	v.pixels = img
	v.dirty = true
	return nil
}
