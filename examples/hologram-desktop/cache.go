package main

import (
	"image"
	"image/draw"

	nativeui "github.com/codemodify/worldr/sdk/nativeui/v1"
)

// Four non-overlapping composition bands retain only their current pixels.
// Their combined storage is one framebuffer, plus one immutable particle
// background. Popups are painted last and never enter these base-layer caches.
type diskLayer struct {
	key      diskLayerKey
	pixels   *image.RGBA
	controls []nativeui.Control
}

type diskLayerKey struct {
	state                     diskState
	focused, hovered, pressed string
}

func (d *diskDesktop) clearRenderCache() {
	d.background = nil
	d.layers = [4]diskLayer{}
}

// Summed alpha yields the same box halo, including zero padding and integer
// rounding, without resumming a square neighborhood for every glyph pixel.
func glyphHalo(layer *image.RGBA, radius int) *image.Alpha {
	w, h := layer.Bounds().Dx(), layer.Bounds().Dy()
	stride := w + 1
	sums := make([]uint32, stride*(h+1))
	for y := 0; y < h; y++ {
		var row uint32
		for x := 0; x < w; x++ {
			row += uint32(layer.Pix[y*layer.Stride+x*4+3])
			sums[(y+1)*stride+x+1] = sums[y*stride+x+1] + row
		}
	}
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	area := uint32((2*radius + 1) * (2*radius + 1))
	for y := 0; y < h; y++ {
		y0, y1 := max(0, y-radius), min(h, y+radius+1)
		for x := 0; x < w; x++ {
			x0, x1 := max(0, x-radius), min(w, x+radius+1)
			total := sums[y1*stride+x1] - sums[y0*stride+x1] - sums[y1*stride+x0] + sums[y0*stride+x0]
			mask.Pix[y*mask.Stride+x] = uint8(total / area / 2)
		}
	}
	return mask
}

func (c *diskCanvas) layerKey(index int, controls []nativeui.Control) diskLayerKey {
	s := c.d.state
	var key diskLayerKey
	switch index {
	case 0: // Menu and toolbar captions/toggles.
		key.state = diskState{Menu: s.Menu, Details: s.Details, Grid: s.Grid, Help: s.Help, Sample: s.Sample, Visible: s.Visible}
	case 1: // Sorted table with selection and capacity values.
		key.state = diskState{Selected: s.Selected, Sort: s.Sort, Descending: s.Descending, Sample: s.Sample, Visible: s.Visible}
	case 2: // Physical partition map; table sort never moves its geometry.
		key.state = diskState{Selected: s.Selected, Details: s.Details, Grid: s.Grid, Sample: s.Sample, Visible: s.Visible}
	case 3:
		key.state.Visible = s.Visible
	}
	for _, control := range controls {
		state := c.d.controller.Decorate(control).State
		if state.Focused {
			key.focused = control.ID
		}
		if state.Hovered {
			key.hovered = control.ID
		}
		if state.Pressed {
			key.pressed = control.ID
		}
	}
	return key
}

func (c *diskCanvas) layer(index int, top, bottom float64, reuse bool, render func()) {
	if !reuse {
		render()
		return
	}
	layer := &c.d.layers[index]
	bounds := c.bounds(0, top, initialWidth, bottom-top)
	key := c.layerKey(index, layer.controls)
	if layer.pixels != nil && layer.key == key {
		draw.Draw(c.img, bounds, layer.pixels, image.Point{}, draw.Src)
		// Appending copies the controls: a modal can disable its live controls
		// without modifying the enabled base composition retained here.
		c.d.controls = append(c.d.controls, layer.controls...)
		return
	}
	start := len(c.d.controls)
	render()
	if c.err != nil {
		return
	}
	if layer.pixels == nil || layer.pixels.Bounds().Size() != bounds.Size() {
		layer.pixels = image.NewRGBA(image.Rectangle{Max: bounds.Size()})
	}
	draw.Draw(layer.pixels, layer.pixels.Bounds(), c.img, bounds.Min, draw.Src)
	layer.controls = append(layer.controls[:0], c.d.controls[start:]...)
	layer.key = c.layerKey(index, layer.controls)
}
