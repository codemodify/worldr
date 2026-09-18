package scene

import (
	"math"

	"github.com/codemodify/worldr/internal/render"
	"golang.org/x/image/font"
)

type Color struct{ R, G, B, A float32 }

func ColorHex(rgb uint32, alpha float32) Color {
	return Color{float32((rgb>>16)&255) / 255, float32((rgb>>8)&255) / 255, float32(rgb&255) / 255, alpha}
}
func (c Color) Multiply(other Color) Color {
	return Color{c.R * other.R, c.G * other.G, c.B * other.B, c.A * other.A}
}

// Hierarchical tints remain authored sRGB values at the render boundary, but
// their multiplication must happen in linear light when the frame opts in.
func (c Color) multiplyLinear(other Color) Color {
	channel := func(a, b float32) float32 {
		if a == 1 {
			return b
		}
		if b == 1 {
			return a
		}
		decode := func(v float64) float64 {
			v = max(0, v)
			if v <= .04045 {
				return v / 12.92
			}
			return math.Pow((v+.055)/1.055, 2.4)
		}
		v := decode(float64(a)) * decode(float64(b))
		if v <= .0031308 {
			return float32(v * 12.92)
		}
		return float32(1.055*math.Pow(v, 1/2.4) - .055)
	}
	return Color{channel(c.R, other.R), channel(c.G, other.G), channel(c.B, other.B), c.A * other.A}
}

func (c Color) WithAlpha(alpha float32) Color { c.A = alpha; return c }

// Canvas records ordered 2D batches and native 3D scene passes. Geometry stays
// resident in the GPU backend; a frame contains only instances and UI vertices.
type Canvas struct {
	vertices           []render.Vertex
	commands           []render.Command
	draws              []render.Draw
	pendingUI          int
	atlas              render.Atlas
	glyphs             map[rune]glyph
	kern               map[uint64]float32
	face               font.Face
	ascent, lineHeight float32
	whiteU, whiteV     float32
	width, height      int
	linearColor        bool
	output             render.OutputTransform
}

func NewCanvas() (*Canvas, error) {
	c := &Canvas{vertices: make([]render.Vertex, 0, 16384)}
	if err := c.buildAtlas(); err != nil {
		return nil, err
	}
	c.Reset(1, 1)
	return c, nil
}
func (c *Canvas) Atlas() render.Atlas { return c.atlas }
func (c *Canvas) Reset(width, height int) {
	c.width, c.height = width, height
	c.vertices = c.vertices[:0]
	c.commands = c.commands[:0]
	c.draws = c.draws[:0]
	c.pendingUI = 0
}

// SetLinearColor selects the frame-wide color contract. Reset retains it.
func (c *Canvas) SetLinearColor(enabled bool) { c.linearColor = enabled }

// SetOutputTransform selects the display-referred SDR finish. Reset retains it
// alongside the frame-wide linear-color choice.
func (c *Canvas) SetOutputTransform(output render.OutputTransform) { c.output = output }

func (c *Canvas) Size() (int, int) { return c.width, c.height }

// Frame borrows the canvas buffers until the next Reset. Calling it repeatedly
// does not duplicate commands. Consumers must copy/upload before Reset.
func (c *Canvas) Frame() render.Frame {
	c.flushUI()
	return render.Frame{LinearColor: c.linearColor, Output: c.output, Vertices: c.vertices, Commands: c.commands}
}
func (c *Canvas) flushUI() {
	if count := len(c.vertices) - c.pendingUI; count > 0 {
		c.commands = append(c.commands, render.Command{Kind: render.OverlayCommand, First: c.pendingUI, Count: count})
		c.pendingUI = len(c.vertices)
	}
}
func (c *Canvas) vertex(x, y, depth, u, v float32, col Color) render.Vertex {
	return render.Vertex{X: x, Y: y, Z: depth, U: u, V: v, R: col.R, G: col.G, B: col.B, A: col.A}
}
func (c *Canvas) triangle(a, b, d render.Vertex) { c.vertices = append(c.vertices, a, b, d) }
func (c *Canvas) quad(x0, y0, x1, y1, depth, u0, v0, u1, v1 float32, col Color) {
	a, b, d, e := c.vertex(x0, y0, depth, u0, v0, col), c.vertex(x1, y0, depth, u1, v0, col), c.vertex(x1, y1, depth, u1, v1, col), c.vertex(x0, y1, depth, u0, v1, col)
	c.triangle(a, b, d)
	c.triangle(a, d, e)
}
func (c *Canvas) Rect(x, y, width, height float32, col Color) {
	if width <= 0 || height <= 0 || col.A <= 0 {
		return
	}
	c.quad(x, y, x+width, y+height, 0, c.whiteU, c.whiteV, c.whiteU, c.whiteV, col)
}

func (c *Canvas) Line(x1, y1, x2, y2, width float32, col Color) {
	if width <= 0 || col.A <= 0 {
		return
	}
	// A one-pixel coverage fringe preserves the weight of subpixel strokes.
	dx, dy := x2-x1, y2-y1
	length := float32(math.Hypot(float64(dx), float64(dy)))
	if length < 1e-5 {
		return
	}
	nx, ny := -dy/length, dx/length
	outer, inner := (width+1)/2, abs(width-1)/2
	coverage := clamp(width, 0, 1)
	offsets := [4]float32{-outer, -inner, inner, outer}
	alphas := [4]float32{0, coverage, coverage, 0}
	var start, end [4]render.Vertex
	for i, offset := range offsets {
		x, y := nx*offset, ny*offset
		color := col
		color.A *= alphas[i]
		start[i] = c.vertex(x1+x, y1+y, 0, c.whiteU, c.whiteV, color)
		end[i] = c.vertex(x2+x, y2+y, 0, c.whiteU, c.whiteV, color)
	}
	for i := 0; i < 3; i++ {
		if offsets[i] == offsets[i+1] {
			continue
		}
		c.triangle(start[i], start[i+1], end[i+1])
		c.triangle(start[i], end[i+1], end[i])
	}
}

// Circle draws a stroke. A non-positive width draws a filled disc.
func (c *Canvas) Circle(cx, cy, r, width float32, col Color) {
	c.Arc(cx, cy, r, 0, 2*math.Pi, width, col)
}

// RadialGradient fills a disc with one triangle fan, interpolating straight
// RGBA from inner at the center to outer at the perimeter. For a fading halo,
// keep both RGB colors equal and set outer.A to zero. Like other canvas shapes,
// geometry can extend outside the canvas and is clipped by the GPU viewport.
// Nonpositive radii, non-finite inputs and overflowing extents are ignored.
func (c *Canvas) RadialGradient(centerX, centerY, radius float32, inner, outer Color) {
	if radius <= 0 || (inner.A <= 0 && outer.A <= 0) {
		return
	}
	for _, value := range [...]float32{centerX, centerY, radius, inner.R, inner.G, inner.B, inner.A, outer.R, outer.G, outer.B, outer.A} {
		if !finite(value) {
			return
		}
	}
	for _, coordinate := range [...]float32{centerX, centerY} {
		if math.Abs(float64(coordinate))+float64(radius) > math.MaxFloat32 {
			return
		}
	}
	steps := int(math.Min(512, math.Max(12, math.Ceil(2*math.Pi*math.Sqrt(float64(radius))))))
	center := c.vertex(centerX, centerY, 0, c.whiteU, c.whiteV, inner)
	first := c.vertex(centerX+radius, centerY, 0, c.whiteU, c.whiteV, outer)
	previous := first
	for i := 1; i <= steps; i++ {
		next := first // Close the seam exactly, including its interpolated color.
		if i < steps {
			angle := 2 * math.Pi * float64(i) / float64(steps)
			next = c.vertex(centerX+radius*float32(math.Cos(angle)), centerY+radius*float32(math.Sin(angle)), 0, c.whiteU, c.whiteV, outer)
		}
		c.triangle(center, previous, next)
		previous = next
	}
}

// Arc angles are radians, clockwise in framebuffer coordinates. A non-positive
// width fills the sector; positive width draws an annular stroke.
func (c *Canvas) Arc(cx, cy, r, start, end, width float32, col Color) {
	if r <= 0 || col.A <= 0 || end == start {
		return
	}
	span := end - start
	steps := int(math.Ceil(math.Abs(float64(span)) * math.Sqrt(float64(r))))
	if steps < 4 {
		steps = 4
	}
	if steps > 512 {
		steps = 512
	}
	d := float32(0)
	var radii, alphas [4]float32
	layers := 4
	if width <= 0 || width >= r {
		radii = [4]float32{0, clamp(r-0.5, 0, r), r + 0.5}
		alphas = [4]float32{1, 1, 0}
		layers = 3
	} else {
		center, outer, inner := r-width/2, (width+1)/2, abs(width-1)/2
		radii = [4]float32{clamp(center-outer, 0, r), clamp(center-inner, 0, r), center + inner, center + outer}
		coverage := clamp(width, 0, 1)
		alphas = [4]float32{0, coverage, coverage, 0}
	}
	for i := 0; i < steps; i++ {
		a, b := float64(start+span*float32(i)/float32(steps)), float64(start+span*float32(i+1)/float32(steps))
		ca, sa, cb, sb := float32(math.Cos(a)), float32(math.Sin(a)), float32(math.Cos(b)), float32(math.Sin(b))
		for band := 0; band < layers-1; band++ {
			inner, outer := radii[band], radii[band+1]
			if inner == outer {
				continue
			}
			innerColor, outerColor := col, col
			innerColor.A *= alphas[band]
			outerColor.A *= alphas[band+1]
			v0, v1, v2, v3 := c.vertex(cx+ca*inner, cy+sa*inner, d, c.whiteU, c.whiteV, innerColor), c.vertex(cx+ca*outer, cy+sa*outer, d, c.whiteU, c.whiteV, outerColor), c.vertex(cx+cb*outer, cy+sb*outer, d, c.whiteU, c.whiteV, outerColor), c.vertex(cx+cb*inner, cy+sb*inner, d, c.whiteU, c.whiteV, innerColor)
			c.triangle(v0, v1, v2)
			if inner > 0 {
				c.triangle(v0, v2, v3)
			}
		}
	}
}
