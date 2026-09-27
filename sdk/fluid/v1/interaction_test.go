package fluid

import (
	"math"
	"testing"
)

func TestSpringStableAcrossDampingAndFrameIntervals(t *testing.T) {
	for _, damping := range []float32{0, .35, 1, 2, 4} {
		t.Run(fmtFloat(damping), func(t *testing.T) {
			a := Spring{Position: Point{-80, 130}, Velocity: Point{23, -15}}
			b := a
			target := Point{110, -30}
			a.Step(target, .5, 3, damping)
			for range 60 {
				b.Step(target, 1.0/120, 3, damping)
			}
			near(t, a.Position.X, b.Position.X, .001)
			near(t, a.Position.Y, b.Position.Y, .001)
			near(t, a.Velocity.X, b.Velocity.X, .01)
			near(t, a.Velocity.Y, b.Velocity.Y, .01)
		})
	}
	critical := Spring{}
	previous := float32(0)
	for range 240 {
		critical.Step(Point{100, -50}, 1.0/120, 4, 1)
		if critical.Position.X < previous-1e-5 || critical.Position.X > 100.00001 {
			t.Fatal("critical spring overshot or reversed")
		}
		previous = critical.Position.X
	}
	near(t, critical.Position.X, 100, .001)
	near(t, critical.Position.Y, -50, .001)
	near(t, critical.Velocity.X, 0, .01)
	under := Spring{}
	overshot := false
	for range 240 {
		under.Step(Point{100, 0}, 1.0/120, 4, .25)
		overshot = overshot || under.Position.X > 100
	}
	if !overshot {
		t.Fatal("underdamped spring lost its intended response")
	}
}
func fmtFloat(value float32) string {
	if value == 0 {
		return "undamped"
	}
	if value == 1 {
		return "critical"
	}
	if value < 1 {
		return "under"
	}
	if value == 2 {
		return "over"
	}
	return "heavy"
}

func TestSpringInvalidInputsAndReducedMotionReset(t *testing.T) {
	initial := Spring{Position: Point{10, -20}, Velocity: Point{3, -4}}
	for _, args := range [][3]float32{{0, 4, 1}, {-1, 4, 1}, {11, 4, 1}, {float32(math.NaN()), 4, 1}, {.1, 0, 1}, {.1, 121, 1}, {.1, 4, -1}, {.1, 4, 5}, {.1, float32(math.Inf(1)), 1}} {
		s := initial
		s.Step(Point{50, 60}, args[0], args[1], args[2])
		if s != initial {
			t.Fatal("invalid spring parameters changed state", args)
		}
	}
	s := initial
	s.Step(Point{float32(math.NaN()), 0}, .1, 4, 1)
	if s != initial {
		t.Fatal("nonfinite target changed spring")
	}
	s.Reset(Point{-10, 70})
	if s.Position != (Point{-10, 70}) || s.Velocity != (Point{}) {
		t.Fatal("reset retained momentum")
	}
	var absent *Spring
	absent.Step(Point{}, .1, 4, 1)
	absent.Reset(Point{})
}
func TestGridMagneticSnapAndBounds(t *testing.T) {
	if got := SnapGrid(Point{25, -25}, 10); got != (Point{30, -30}) {
		t.Fatal("grid rounding", got)
	}
	if got := SnapGrid(Point{-2, 13}, 0); got != (Point{-2, 13}) {
		t.Fatal("zero grid spacing moved the point")
	}
	peer := Rect{100, 100, 100, 100}
	got, matched := SnapMagnetic(Rect{204, 108, 60, 80}, []Rect{peer}, 12)
	if !matched || got.X != 200 || got.Y != 110 || got.Width != 60 || got.Height != 80 {
		t.Fatal("edge dock/center alignment failed", got, matched)
	}
	far := Rect{204, 400, 60, 80}
	got, matched = SnapMagnetic(far, []Rect{peer}, 12)
	if matched || got != far {
		t.Fatal("distant panels aligned across unrelated rows")
	}
	negative := Rect{-104, -97, 50, 40}
	got, matched = SnapMagnetic(negative, []Rect{{-50, -100, 70, 40}}, 8)
	if !matched || got.X != -100 || got.Y != -100 {
		t.Fatal("negative-coordinate snapping failed", got)
	}
	// Ties are stable in caller order, without changing peer geometry.
	peers := []Rect{{-15, 0, 10, 20}, {15, 0, 10, 20}}
	original := append([]Rect(nil), peers...)
	got, matched = SnapMagnetic(Rect{0, 0, 10, 20}, peers, 5)
	if !matched || got.X != -5 || peers[0] != original[0] || peers[1] != original[1] {
		t.Fatal("tie order or peer ownership changed", got)
	}
	for _, bad := range []float32{-1, float32(math.NaN()), MaxBlend + 1} {
		got, matched = SnapMagnetic(far, []Rect{peer}, bad)
		if matched || got != far {
			t.Fatal("invalid magnet distance moved bounds")
		}
	}
	container := Rect{-40, -20, 100, 80}
	clamped := ClampRect(Rect{-80, 70, 50, 30}, container)
	if clamped != (Rect{-40, 30, 50, 30}) {
		t.Fatal("clamp failed", clamped)
	}
	if got := ClampRect(Rect{0, 0, 200, 100}, container); got != container {
		t.Fatal("oversize panel was not fitted", got)
	}
}
