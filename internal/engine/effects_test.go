package engine

import (
	"testing"
	"time"
)

func TestParseTier(t *testing.T) {
	high, err := ParseTier("auto")
	if err != nil || high != TierHigh {
		t.Fatalf("auto: %v %v", high, err)
	}
	if off, err := ParseTier("off"); err != nil || off != TierOff {
		t.Fatalf("off")
	}
	if low, err := ParseTier("LOW"); err != nil || low != TierLow {
		t.Fatalf("low")
	}
	if _, err := ParseTier("wgpu"); err == nil {
		t.Fatal("expected reject")
	}
}

func TestEaseCurves(t *testing.T) {
	if EaseOutCubic(0) != 0 || EaseOutCubic(1) != 1 {
		t.Fatal("out cubic ends")
	}
	if EaseInCubic(0) != 0 || EaseInCubic(1) != 1 {
		t.Fatal("in cubic ends")
	}
	mid := EaseOutCubic(0.5)
	if mid <= 0.5 {
		t.Fatalf("ease-out should be front-loaded, got %v", mid)
	}
	inMid := EaseInCubic(0.5)
	if inMid >= 0.5 {
		t.Fatalf("ease-in should be back-loaded, got %v", inMid)
	}
	if EaseOutQuad(0) != 0 || EaseOutQuad(1) != 1 {
		t.Fatal("quad ends")
	}
	if EaseInOutCubic(0) != 0 || EaseInOutCubic(1) != 1 {
		t.Fatal("in-out ends")
	}
	io := EaseInOutCubic(0.5)
	if io < 0.49 || io > 0.51 {
		t.Fatalf("in-out mid %v", io)
	}
}

func TestVisualMapInHigh(t *testing.T) {
	now := time.Unix(1000, 0)
	a := &Actor{Born: now, Width: 100, Height: 80}
	v0 := a.VisualAt(now, TierHigh)
	if v0.Gone || v0.Alpha != 0 || v0.Scale != MapFromScale {
		t.Fatalf("t0 %+v", v0)
	}
	vMid := a.VisualAt(now.Add(MapInDuration/2), TierHigh)
	if vMid.Alpha <= 0.5 || vMid.Scale <= MapFromScale || vMid.Scale >= 1 {
		t.Fatalf("mid %+v", vMid)
	}
	v1 := a.VisualAt(now.Add(MapInDuration), TierHigh)
	if !v1.Identity() {
		t.Fatalf("done %+v", v1)
	}
}

func TestVisualMapOutThenGone(t *testing.T) {
	now := time.Unix(2000, 0)
	a := &Actor{UnmapAt: now}
	v0 := a.VisualAt(now, TierHigh)
	if v0.Gone || v0.Alpha != 1 || v0.Scale != 1 {
		t.Fatalf("unmap t0 %+v", v0)
	}
	vMid := a.VisualAt(now.Add(MapOutDuration/2), TierHigh)
	if vMid.Gone || vMid.Alpha >= 1 || vMid.Scale >= 1 {
		t.Fatalf("unmap mid %+v", vMid)
	}
	if !a.VisualAt(now.Add(MapOutDuration), TierHigh).Gone {
		t.Fatal("expected gone")
	}
}

func TestVisualLowIsFadeOnly(t *testing.T) {
	now := time.Unix(3000, 0)
	a := &Actor{Born: now}
	v := a.VisualAt(now.Add(MapInDuration/2), TierLow)
	if v.Scale != 1 || v.Alpha <= 0 || v.Shadow != 0 || v.Lift != 0 {
		t.Fatalf("low %+v", v)
	}
}

func TestVisualOffIsIdentity(t *testing.T) {
	a := &Actor{Born: time.Unix(1, 0)}
	if !a.VisualAt(time.Unix(1, 0), TierOff).Identity() {
		t.Fatal("off")
	}
}

func TestVisualFocusPulseHigh(t *testing.T) {
	now := time.Unix(4000, 0)
	a := &Actor{Focused: true, FocusPulse: now, Width: 10, Height: 10}
	v := a.VisualAt(now, TierHigh)
	if v.Lift < 3 || v.Shadow < 0.3 || v.Glow < 0.5 || v.Phase != PhaseFocus {
		t.Fatalf("pulse t0 %+v", v)
	}
	if !a.VisualAt(now.Add(FocusDuration), TierHigh).Identity() {
		t.Fatal("pulse done")
	}
}

func TestVisualPhaseMachine(t *testing.T) {
	now := time.Unix(5000, 0)
	a := &Actor{Born: now, Width: 80, Height: 60}
	if a.VisualAt(now, TierHigh).Phase != PhaseMapIn {
		t.Fatal("map-in")
	}
	if a.VisualAt(now.Add(MapInDuration), TierHigh).Phase != PhaseIdle {
		t.Fatal("idle")
	}
	a.FocusPulse = now.Add(MapInDuration)
	a.Focused = true
	if a.VisualAt(a.FocusPulse, TierHigh).Phase != PhaseFocus {
		t.Fatal("focus")
	}
	a.UnmapAt = now.Add(2 * time.Second)
	if a.VisualAt(a.UnmapAt, TierHigh).Phase != PhaseMapOut {
		t.Fatal("map-out")
	}
	if !a.VisualAt(a.UnmapAt.Add(MapOutDuration), TierHigh).Gone {
		t.Fatal("gone")
	}
}

func TestVisualMapInRiseHigh(t *testing.T) {
	now := time.Unix(6000, 0)
	a := &Actor{Born: now, Width: 40, Height: 30}
	v0 := a.VisualAt(now, TierHigh)
	if v0.SlideY != MapRise || v0.Phase != PhaseMapIn {
		t.Fatalf("rise t0 %+v", v0)
	}
	v1 := a.VisualAt(now.Add(MapInDuration), TierHigh)
	if v1.SlideY != 0 || !v1.Identity() {
		t.Fatalf("rise done %+v", v1)
	}
	low := a.VisualAt(now, TierLow)
	if low.SlideY != 0 || low.Scale != 1 {
		t.Fatalf("low has no rise %+v", low)
	}
}

func TestMinimizeDeltaTowardPanel(t *testing.T) {
	a := &Actor{X: 100, Y: 80, Width: 40, Height: 20}
	v := Visual{Phase: PhaseMapOut, Progress: 1}
	dx, dy := MinimizeDelta(a, v, 800, 600, 36)
	if dx == 0 && dy == 0 {
		t.Fatal("should move toward panel")
	}
	// target is (400, 600-18) = (400, 582); center (120, 90)
	if dx < 200 || dy < 400 {
		t.Fatalf("dx=%d dy=%d", dx, dy)
	}
	if dx, dy := MinimizeDelta(a, Visual{Phase: PhaseIdle}, 800, 600, 36); dx != 0 || dy != 0 {
		t.Fatal("idle")
	}
	if dx, dy := MinimizeDelta(nil, v, 800, 600, 36); dx != 0 || dy != 0 {
		t.Fatal("nil")
	}
}

func TestSlideFade(t *testing.T) {
	if SlideFade(0, 800) != 1 {
		t.Fatal("settled")
	}
	mid := SlideFade(400, 800)
	if mid >= 1 || mid < 0.45 {
		t.Fatalf("mid %v", mid)
	}
	if SlideFade(800, 800) >= SlideFade(200, 800) {
		t.Fatal("farther is dimmer")
	}
}

func TestActorsIntoReuses(t *testing.T) {
	s := NewScene()
	s.Add(&Actor{Width: 1, Height: 1})
	buf := make([]*Actor, 0, 8)
	got := s.ActorsInto(buf)
	if cap(got) != 8 || len(got) != 1 {
		t.Fatalf("len=%d cap=%d", len(got), cap(got))
	}
	s.Add(&Actor{Width: 2, Height: 2})
	got2 := s.ActorsInto(got)
	if cap(got2) != 8 || len(got2) != 2 {
		t.Fatalf("reuse len=%d cap=%d", len(got2), cap(got2))
	}
}

func TestOccupiedIntoReuses(t *testing.T) {
	s := NewScene()
	s.Add(&Actor{})
	buf := make([]bool, 0, 4)
	got := s.OccupiedInto(buf)
	if cap(got) != 4 || len(got) != 3 || !got[0] {
		t.Fatalf("%v cap=%d", got, cap(got))
	}
}

func TestSceneRemoveSweepsAfterMapOut(t *testing.T) {
	s := NewScene()
	s.SetTheater(TierHigh)
	a := &Actor{}
	s.Add(a)
	if !s.HasActors() || a.Born.IsZero() {
		t.Fatal("add")
	}
	s.Remove(a)
	if !s.HasActors() || a.UnmapAt.IsZero() {
		t.Fatal("should keep actor during map-out")
	}
	s.Sweep(a.UnmapAt.Add(MapOutDuration))
	if s.HasActors() {
		t.Fatal("sweep should drop")
	}
}

func TestSceneRemoveOffIsImmediate(t *testing.T) {
	s := NewScene()
	a := &Actor{}
	s.Add(a)
	s.Remove(a)
	if s.HasActors() {
		t.Fatal("off is immediate")
	}
}
