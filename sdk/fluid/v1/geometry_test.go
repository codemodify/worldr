package fluid

import (
	"math"
	"testing"
)

func near(t *testing.T, got, want, tolerance float32) {
	t.Helper()
	if !finite(got) || abs(got-want) > tolerance {
		t.Fatalf("got %g, want %g ±%g", got, want, tolerance)
	}
}
func box(x, y, w, h, r float32, fuse bool) Surface {
	return Surface{Bounds: Rect{x, y, w, h}, Radius: r, Tint: [4]float32{.3, .5, .8, .7}, Fuse: fuse}
}

func TestRoundedBoxDistanceAndClippedContainment(t *testing.T) {
	s := box(-20, -10, 100, 60, 10, true)
	for _, test := range []struct {
		p        Point
		distance float32
	}{{Point{30, 20}, -30}, {Point{-20, 20}, 0}, {Point{-25, 20}, 5}, {Point{-10, -10}, 0}, {Point{-20, -10}, float32(math.Sqrt(200)) - 10}} {
		near(t, s.Distance(test.p), test.distance, 1e-5)
	}
	if s.Contains(Point{-20, -10}) || !s.Contains(Point{30, 20}) {
		t.Fatal("rounded hit testing used rectangular corners")
	}
	f := Field{Bounds: Rect{0, 0, 40, 40}, Surfaces: []Surface{s}, Style: DefaultStyle()}
	if f.Distance(Point{-10, 20}) >= 0 || f.Contains(Point{-10, 20}) || !f.Contains(Point{30, 20}) {
		t.Fatal("field clipping changed the underlying distance or admitted outside input")
	}
	if f.Bounds.Contains(Point{40, 20}) || !f.Bounds.Contains(Point{0, 0}) {
		t.Fatal("rectangle containment must be half-open")
	}
	f.Surfaces = nil
	if !math.IsInf(float64(f.Distance(Point{5, 5})), 1) || f.Contains(Point{5, 5}) {
		t.Fatal("empty field contains a surface")
	}
}
func TestSmoothUnionCompactSupportAndChainJoining(t *testing.T) {
	near(t, SmoothUnion(3, 3, 8), 1, 0)
	near(t, SmoothUnion(4, 20, 8), 4, 0)
	near(t, SmoothUnion(3, -1, 0), -1, 0)
	near(t, SmoothUnion(4, 7, 8), SmoothUnion(7, 4, 8), 0)
	a, b, c := box(0, 0, 80, 100, 20, true), box(92, 0, 80, 100, 20, true), box(184, 0, 80, 100, 20, true)
	if !Joined(a, b, 32) || !Joined(b, c, 32) || Joined(a, c, 32) {
		t.Fatal("pairwise adjacency lost the chain")
	}
	f := Field{Bounds: Rect{-30, -30, 360, 180}, Surfaces: []Surface{a, b, c}, Style: DefaultStyle()}
	f.Style.Blend = 32
	for _, p := range []Point{{86, 50}, {178, 50}} {
		if f.Distance(p) >= 0 {
			t.Fatal("smooth union left a gap inside the chain", p)
		}
	}
	f.Style.Blend = 0
	if f.Distance(Point{86, 50}) <= 0 {
		t.Fatal("zero blend still joins separated panels")
	}
	f.Style.Blend = 24
	near(t, f.Distance(Point{86, 50}), 0, 1e-5)
	f.Style.Blend = 32
	f.Surfaces[1].Fuse = false
	if f.Distance(Point{86, 50}) <= 0 || f.Distance(Point{178, 50}) <= 0 || Joined(a, f.Surfaces[1], 32) {
		t.Fatal("non-fusing middle panel bridged its neighbors")
	}
	if f.Distance(Point{120, 50}) >= 0 {
		t.Fatal("non-fusing surface disappeared")
	}
	// Moving a non-fusing surface in the slice never changes the fused fold.
	g := f.Clone()
	g.Surfaces = []Surface{f.Surfaces[1], a, c}
	for x := float32(-20); x < 280; x += 3 {
		for y := float32(-20); y < 120; y += 7 {
			near(t, f.Distance(Point{x, y}), g.Distance(Point{x, y}), 0)
		}
	}
}
func TestRoundedSeparationAndLiveGeometryChanges(t *testing.T) {
	a, b := box(0, 0, 40, 40, 20, true), box(40, 40, 40, 40, 20, true)
	near(t, Separation(a, b), float32(math.Sqrt(3200))-40, 1e-5)
	if Joined(a, b, 28) || !Joined(a, b, 34) {
		t.Fatal("rounded corners were treated as touching bounding boxes")
	}
	near(t, Separation(a, b), Separation(b, a), 0)
	b.Bounds = Rect{30, 0, 40, 40}
	near(t, Separation(a, b), -10, 1e-5)
	b.Bounds = Rect{40, 0, 40, 40}
	near(t, Separation(a, b), 0, 1e-5)
	f := Field{Bounds: Rect{-100, -100, 400, 300}, Style: DefaultStyle(), Surfaces: []Surface{a, b}}
	p := Point{0, 0}
	before := f.Distance(p)
	f.Surfaces[0].Radius = 0
	if before <= 0 || f.Distance(p) != 0 {
		t.Fatal("radius update used stale geometry")
	}
	f.Surfaces[0].Bounds.X = -80
	if f.Distance(p) <= 0 {
		t.Fatal("position update used stale geometry")
	}
	distance := f.Distance(p)
	for i := range f.Surfaces {
		f.Surfaces[i].Bounds.X -= 120
		f.Surfaces[i].Bounds.Y -= 90
	}
	p.X -= 120
	p.Y -= 90
	near(t, f.Distance(p), distance, 1e-5)
}
