package fluid

import "math"

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// Contains follows the usual half-open rectangle convention: the left/top
// edges are included and the right/bottom edges are excluded.
func (r Rect) Contains(p Point) bool {
	return p.X >= r.X && p.Y >= r.Y && p.X < r.X+r.Width && p.Y < r.Y+r.Height
}

func roundedDistance(x, y, halfWidth, halfHeight, radius float32) float32 {
	qx, qy := abs(x)-(halfWidth-radius), abs(y)-(halfHeight-radius)
	outX, outY := max(qx, 0), max(qy, 0)
	return float32(math.Hypot(float64(outX), float64(outY))) + min(max(qx, qy), 0) - radius
}

// Distance is the exact rounded-box signed distance for a validated Surface.
// Negative values are inside, zero is its contour, and positive values are
// outside. No allocation, mutation or validation occurs while sampling.
func (s Surface) Distance(p Point) float32 {
	r := s.Bounds
	return roundedDistance(p.X-(r.X+r.Width/2), p.Y-(r.Y+r.Height/2), r.Width/2, r.Height/2, s.Radius)
}
func (s Surface) Contains(p Point) bool { return s.Distance(p) <= 0 }

// SmoothUnion is a compact polynomial smooth minimum. Its influence ends
// when |a-b| >= blend; blend<=0 reduces to a hard minimum. Positive infinity
// is the identity, useful when beginning an empty union.
// CPU/GPU agreement requires retaining this expression and surface order.
func SmoothUnion(a, b, blend float32) float32 {
	if blend <= 0 {
		return min(a, b)
	}
	if math.IsInf(float64(a), 1) {
		return b
	}
	if math.IsInf(float64(b), 1) {
		return a
	}
	h := max(blend-abs(a-b), 0) / blend
	return min(a, b) - blend*h*h*.25
}

// Distance samples the panel union without clipping it to Field.Bounds.
// Fusing surfaces are folded in slice order. Non-fusing surfaces are combined
// by hard minimum afterward so they cannot bridge or round another panel.
// An empty field returns positive infinity. Call Validate once before using
// the field in a renderer or repeated hit tests.
func (f Field) Distance(p Point) float32 {
	fused, hard := float32(math.Inf(1)), float32(math.Inf(1))
	for _, surface := range f.Surfaces {
		d := surface.Distance(p)
		if surface.Fuse {
			fused = SmoothUnion(fused, d, f.Style.Blend)
		} else {
			hard = min(hard, d)
		}
	}
	return min(fused, hard)
}
func (f Field) Contains(p Point) bool { return f.Bounds.Contains(p) && f.Distance(p) <= 0 }

// Separation returns the signed gap between two axis-aligned rounded boxes:
// positive when separated, zero at contact, negative while overlapping.
// Rounded corner radii participate, unlike a plain bounding-box gap.
func Separation(a, b Surface) float32 {
	ax, ay := a.Bounds.X+a.Bounds.Width/2, a.Bounds.Y+a.Bounds.Height/2
	bx, by := b.Bounds.X+b.Bounds.Width/2, b.Bounds.Y+b.Bounds.Height/2
	return roundedDistance(ax-bx, ay-by, (a.Bounds.Width+b.Bounds.Width)/2, (a.Bounds.Height+b.Bounds.Height)/2, a.Radius+b.Radius)
}

// Joined reports pairwise visual adjacency for fusing panels. The midpoint
// of a gap becomes part of the smooth union at gap<=blend/2. Non-fusing
// surfaces always return false, even if their hard-union shapes overlap.
// This query does not create movement groups or move either surface.
func Joined(a, b Surface, blend float32) bool {
	return a.Fuse && b.Fuse && finite(blend) && Separation(a, b) <= max(0, blend)/2
}
