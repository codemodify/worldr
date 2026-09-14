package engine

import (
	"fmt"
	"strings"
	"time"
)

// Theater v0 — hardcoded Compiz-style open/close. Not a plugin graph.
const (
	MapInDuration  = 260 * time.Millisecond
	MapOutDuration = 220 * time.Millisecond
	FocusDuration  = 180 * time.Millisecond
	MapFromScale   = 0.88
)

// Tier selects how much theater work we do per frame.
type Tier int

const (
	TierOff  Tier = iota // instant map/unmap, no pulse
	TierLow              // fade only
	TierHigh             // scale + fade + focus lift/shadow
)

// ParseTier reads --effects=off|low|high|auto.
func ParseTier(s string) (Tier, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto", "high", "on", "true":
		return TierHigh, nil
	case "low":
		return TierLow, nil
	case "off", "none", "false":
		return TierOff, nil
	default:
		return TierOff, fmt.Errorf("unknown --effects=%s (off|low|high)", s)
	}
}

func (t Tier) String() string {
	switch t {
	case TierLow:
		return "low"
	case TierHigh:
		return "high"
	default:
		return "off"
	}
}

// Visual is the per-frame theater pose for an actor.
type Visual struct {
	Scale  float64 // 1 = identity
	Alpha  float64 // 0..1
	Lift   int     // pixels upward
	Shadow float64 // 0..1
	Gone   bool
}

// Identity reports a settled window (fast blit path).
func (v Visual) Identity() bool {
	return !v.Gone && v.Scale == 1 && v.Alpha == 1 && v.Lift == 0 && v.Shadow == 0
}

// VisualAt evaluates the v0 curve at now.
func (a *Actor) VisualAt(now time.Time, tier Tier) Visual {
	if a == nil || tier == TierOff {
		return Visual{Scale: 1, Alpha: 1}
	}
	if !a.UnmapAt.IsZero() {
		dt := now.Sub(a.UnmapAt)
		if dt >= MapOutDuration {
			return Visual{Gone: true}
		}
		p := EaseInCubic(float64(dt) / float64(MapOutDuration))
		v := Visual{Scale: 1, Alpha: 1 - p}
		if tier == TierHigh {
			v.Scale = 1 - p*(1-MapFromScale)
		}
		return v
	}
	if !a.Born.IsZero() {
		dt := now.Sub(a.Born)
		if dt < MapInDuration {
			p := EaseOutCubic(float64(dt) / float64(MapInDuration))
			v := Visual{Scale: 1, Alpha: p}
			if tier == TierHigh {
				v.Scale = MapFromScale + p*(1-MapFromScale)
			}
			return v
		}
	}
	if a.Focused && !a.FocusPulse.IsZero() {
		dt := now.Sub(a.FocusPulse)
		if dt < FocusDuration {
			p := 1 - EaseOutQuad(float64(dt)/float64(FocusDuration))
			v := Visual{Scale: 1, Alpha: 1}
			if tier == TierHigh {
				v.Lift = int(4*p + 0.5)
				v.Shadow = 0.45 * p
			}
			return v
		}
	}
	return Visual{Scale: 1, Alpha: 1}
}

// EaseOutCubic is 0→1, fast start / settle (map-in).
func EaseOutCubic(t float64) float64 {
	t = clamp01(t)
	u := 1 - t
	return 1 - u*u*u
}

// EaseInCubic is 0→1, slow start (map-out).
func EaseInCubic(t float64) float64 {
	t = clamp01(t)
	return t * t * t
}

// EaseOutQuad is 0→1 for the focus pulse decay.
func EaseOutQuad(t float64) float64 {
	t = clamp01(t)
	return 1 - (1-t)*(1-t)
}

func clamp01(t float64) float64 {
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	return t
}
