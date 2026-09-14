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
	if v.Lift < 3 || v.Shadow < 0.3 {
		t.Fatalf("pulse t0 %+v", v)
	}
	if !a.VisualAt(now.Add(FocusDuration), TierHigh).Identity() {
		t.Fatal("pulse done")
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
