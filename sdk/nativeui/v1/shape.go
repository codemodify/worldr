package nativeui

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"
)

// ShapeSpec is a bounded framebuffer-pixel silhouette. Zero-valued dimensions
// use the selected Theme metrics when constructed with Painter.Shape.
type ShapeSpec struct {
	Grammar ShapeGrammar
	Corner  int
	Notch   int
	Stroke  int
}

// Shape returns the current theme's reusable shape specification.
func (p *Painter) Shape() ShapeSpec {
	return ShapeSpec{Grammar: p.theme.Shape, Corner: p.theme.Metrics.Corner, Notch: p.theme.Metrics.Notch, Stroke: p.theme.Metrics.Stroke}
}

func (s ShapeSpec) validate() error {
	if !s.Grammar.valid() {
		return fmt.Errorf("native UI: unknown shape grammar %q", s.Grammar)
	}
	if s.Corner < 0 || s.Corner > 64 || s.Notch < 0 || s.Notch > 64 || s.Stroke < 1 || s.Stroke > 16 {
		return fmt.Errorf("native UI: shape dimensions are outside supported bounds")
	}
	return nil
}

func shapePoints(bounds image.Rectangle, spec ShapeSpec) []image.Point {
	if bounds.Empty() {
		return nil
	}
	x0, y0, x1, y1 := bounds.Min.X, bounds.Min.Y, bounds.Max.X, bounds.Max.Y
	corner := min(spec.Corner, max(0, min(bounds.Dx(), bounds.Dy())/3))
	notch := min(spec.Notch, max(0, min(bounds.Dx()/4, bounds.Dy()/3)))
	switch spec.Grammar {
	case Rounded:
		radius := min(max(1, spec.Corner), min(bounds.Dx(), bounds.Dy())/2)
		points := make([]image.Point, 0, 28)
		centers := []image.Point{{x1 - radius, y0 + radius}, {x1 - radius, y1 - radius}, {x0 + radius, y1 - radius}, {x0 + radius, y0 + radius}}
		for i, center := range centers {
			for step := 0; step <= 6; step++ {
				angle := (-math.Pi / 2) + float64(i)*math.Pi/2 + float64(step)*math.Pi/12
				points = append(points, image.Pt(center.X+int(math.Round(float64(radius)*math.Cos(angle))), center.Y+int(math.Round(float64(radius)*math.Sin(angle)))))
			}
		}
		return points
	case Slab:
		return []image.Point{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
	case Bracketed:
		return []image.Point{{x0 + corner, y0}, {x1, y0}, {x1, y1 - corner}, {x1 - corner, y1}, {x0, y1}, {x0, y0 + corner}}
	case Notched:
		mid := y0 + (y1-y0)/2
		return []image.Point{{x0 + corner, y0}, {x1 - corner, y0}, {x1, y0 + corner}, {x1, mid - notch}, {x1 - notch, mid}, {x1, mid + notch}, {x1, y1 - corner}, {x1 - corner, y1}, {x0 + corner, y1}, {x0, y1 - corner}, {x0, y0 + corner}}
	default: // Chamfered
		return []image.Point{{x0 + corner, y0}, {x1 - corner, y0}, {x1, y0 + corner}, {x1, y1 - corner}, {x1 - corner, y1}, {x0 + corner, y1}, {x0, y1 - corner}, {x0, y0 + corner}}
	}
}

// FillShape composites one shape and clips every write to dst.Bounds().
func FillShape(dst draw.Image, bounds image.Rectangle, spec ShapeSpec, fill color.Color) error {
	if dst == nil {
		return fmt.Errorf("native UI: nil drawing destination")
	}
	if err := spec.validate(); err != nil {
		return err
	}
	if bounds.Empty() || bounds.Intersect(dst.Bounds()).Empty() {
		return nil
	}
	points := shapePoints(bounds, spec)
	if len(points) < 3 {
		return nil
	}
	clip := bounds.Intersect(dst.Bounds())
	source := image.NewUniform(fill)
	intersections := make([]float64, 0, len(points))
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		intersections = intersections[:0]
		scanY := float64(y) + .5
		for i, a := range points {
			b := points[(i+1)%len(points)]
			ay, by := float64(a.Y), float64(b.Y)
			if ay == by || scanY < min(ay, by) || scanY >= max(ay, by) {
				continue
			}
			x := float64(a.X) + (scanY-ay)*(float64(b.X-a.X))/(by-ay)
			intersections = append(intersections, x)
		}
		sort.Float64s(intersections)
		for i := 0; i+1 < len(intersections); i += 2 {
			left := int(math.Ceil(intersections[i] - .5))
			right := int(math.Floor(intersections[i+1]-.5)) + 1
			left, right = max(left, clip.Min.X), min(right, clip.Max.X)
			if right > left {
				draw.Draw(dst, image.Rect(left, y, right, y+1), source, image.Point{}, draw.Over)
			}
		}
	}
	return nil
}

func strokePixel(dst draw.Image, x, y, width int, source image.Image) {
	radius := max(0, width-1) / 2
	bounds := image.Rect(x-radius, y-radius, x+radius+1+(width-1)%2, y+radius+1+(width-1)%2).Intersect(dst.Bounds())
	if !bounds.Empty() {
		draw.Draw(dst, bounds, source, image.Point{}, draw.Over)
	}
}

func strokeSegment(dst draw.Image, from, to image.Point, width int, source image.Image) {
	x0, y0, x1, y1 := from.X, from.Y, to.X, to.Y
	dx, dy := absInt(x1-x0), -absInt(y1-y0)
	sx, sy := -1, -1
	if x0 < x1 {
		sx = 1
	}
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		strokePixel(dst, x0, y0, width, source)
		if x0 == x1 && y0 == y1 {
			return
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

// StrokeShape composites a clipped outline. Bracketed outlines deliberately
// leave the middle of each edge open while retaining a complete filled body.
func StrokeShape(dst draw.Image, bounds image.Rectangle, spec ShapeSpec, stroke color.Color) error {
	if dst == nil {
		return fmt.Errorf("native UI: nil drawing destination")
	}
	if err := spec.validate(); err != nil {
		return err
	}
	strokeBounds := bounds
	if strokeBounds.Dx() > 1 {
		strokeBounds.Max.X--
	}
	if strokeBounds.Dy() > 1 {
		strokeBounds.Max.Y--
	}
	points := shapePoints(strokeBounds, spec)
	if len(points) < 2 || bounds.Intersect(dst.Bounds()).Empty() {
		return nil
	}
	target := clippedImage{Image: dst, clip: bounds.Intersect(dst.Bounds())}
	source := image.NewUniform(stroke)
	if spec.Grammar == Bracketed {
		// Four two-segment corner brackets preserve the open-rail grammar at
		// every control size without exposing unpainted pixels in its body.
		reachX := max(3, bounds.Dx()/4)
		reachY := max(3, bounds.Dy()/3)
		x0, y0, x1, y1 := bounds.Min.X, bounds.Min.Y, bounds.Max.X-1, bounds.Max.Y-1
		segments := [][2]image.Point{
			{{x0, y0 + min(spec.Corner, reachY)}, {x0, y0 + reachY}}, {{x0, y0 + min(spec.Corner, reachY)}, {x0 + min(spec.Corner, reachX), y0}}, {{x0 + min(spec.Corner, reachX), y0}, {x0 + reachX, y0}},
			{{x1 - reachX, y0}, {x1, y0}}, {{x1, y0}, {x1, y0 + reachY}},
			{{x0, y1 - reachY}, {x0, y1}}, {{x0, y1}, {x0 + reachX, y1}},
			{{x1 - reachX, y1}, {x1 - min(spec.Corner, reachX), y1}}, {{x1 - min(spec.Corner, reachX), y1}, {x1, y1 - min(spec.Corner, reachY)}}, {{x1, y1 - min(spec.Corner, reachY)}, {x1, y1 - reachY}},
		}
		for _, segment := range segments {
			strokeSegment(target, segment[0], segment[1], spec.Stroke, source)
		}
		return nil
	}
	for i, point := range points {
		strokeSegment(target, point, points[(i+1)%len(points)], spec.Stroke, source)
	}
	return nil
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
