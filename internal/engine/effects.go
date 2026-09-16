package engine

import (
	"fmt"
	"strings"
	"time"
)

// Theater v1 — Compiz-style open/close/focus/workspace. Not a plugin graph.
const (
	MapInDuration   = 280 * time.Millisecond
	MapOutDuration  = 240 * time.Millisecond
	FocusDuration   = 220 * time.Millisecond
	MapFromScale    = 0.86
	MapRise         = 14 // px rise on map-in (high)
	FocusLiftPx     = 6
	FocusShadow     = 0.55
	FocusGlow       = 0.70
	CubeForeshorten = 0.28
	CubePull        = 0.18
)

// Phase is the theater state machine for one actor.
type Phase int

const (
	PhaseIdle Phase = iota
	PhaseMapIn
	PhaseMapOut
	PhaseFocus
)

func (p Phase) String() string {
	switch p {
	case PhaseMapIn:
		return "map-in"
	case PhaseMapOut:
		return "map-out"
	case PhaseFocus:
		return "focus"
	default:
		return "idle"
	}
}

// Tier selects how much theater work we do per frame.
type Tier int

const (
	TierOff  Tier = iota // instant map/unmap, no pulse
	TierLow              // fade only
	TierHigh             // scale+fade+rise+glow + mesh wobble + burn + cube
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
	Scale    float64 // 1 = identity
	Alpha    float64 // 0..1
	Lift     int     // pixels upward
	Shadow   float64 // 0..1
	Glow     float64 // 0..1 focus ring
	SlideX   int     // extra translation
	SlideY   int
	Progress float64 // 0..1 along Phase
	Phase    Phase
	Gone     bool
	Warped   bool // live wobble mesh — skip the identity blit
}

// Identity reports a settled window (fast blit path).
func (v Visual) Identity() bool {
	return !v.Gone && !v.Warped && v.Phase == PhaseIdle && v.Scale == 1 && v.Alpha == 1 &&
		v.Lift == 0 && v.Shadow == 0 && v.Glow == 0 && v.SlideX == 0 && v.SlideY == 0
}

// VisualAt evaluates the theater curve at now.
func (a *Actor) VisualAt(now time.Time, tier Tier) Visual {
	if a == nil || tier == TierOff {
		return Visual{Scale: 1, Alpha: 1, Phase: PhaseIdle}
	}
	if !a.UnmapAt.IsZero() {
		dt := now.Sub(a.UnmapAt)
		out := MapOutLen(tier)
		if dt >= out {
			return Visual{Gone: true, Phase: PhaseMapOut, Progress: 1}
		}
		p := EaseInCubic(float64(dt) / float64(out))
		v := Visual{Scale: 1, Alpha: 1 - p, Phase: PhaseMapOut, Progress: p}
		if tier == TierHigh {
			// Burn handles dissolve in-place; keep full size/alpha.
			v.Scale, v.Alpha = 1, 1
			v.Progress = float64(dt) / float64(out)
		}
		return v
	}
	if !a.Born.IsZero() {
		dt := now.Sub(a.Born)
		if dt < MapInDuration {
			p := EaseOutCubic(float64(dt) / float64(MapInDuration))
			v := Visual{Scale: 1, Alpha: p, Phase: PhaseMapIn, Progress: p}
			if tier == TierHigh {
				v.Scale = MapFromScale + p*(1-MapFromScale)
				v.SlideY = int((1-p)*float64(MapRise) + 0.5)
			}
			return v
		}
	}
	if a.Focused && !a.FocusPulse.IsZero() {
		dt := now.Sub(a.FocusPulse)
		if dt < FocusDuration {
			p := 1 - EaseOutQuad(float64(dt)/float64(FocusDuration))
			v := Visual{Scale: 1, Alpha: 1, Phase: PhaseFocus, Progress: 1 - p}
			if tier == TierHigh {
				v.Lift = int(float64(FocusLiftPx)*p + 0.5)
				v.Shadow = FocusShadow * p
				v.Glow = FocusGlow * p
				v.Warped = a.MeshLive()
			}
			return v
		}
	}
	v := Visual{Scale: 1, Alpha: 1, Phase: PhaseIdle}
	if tier == TierHigh {
		v.Warped = a.MeshLive()
	}
	return v
}

// CubeFace is the cheap Compiz cube pose for a sliding workspace.
// ox is OffsetFor; scale foreshortens, extraX pulls toward the hinge.
func CubeFace(ox, screenW int) (scale, fade float64, extraX int) {
	if ox == 0 || screenW <= 0 {
		return 1, 1, 0
	}
	u := float64(absInt(ox)) / float64(screenW)
	if u > 1 {
		u = 1
	}
	scale = 1 - CubeForeshorten*u
	fade = SlideFade(ox, screenW)
	extraX = int(-float64(ox)*CubePull + 0.5)
	return
}

// MinimizeDelta is the extra translation on map-out toward the panel
// title slot (cheap Compiz minimize). Zero when not mapping out.
func MinimizeDelta(a *Actor, v Visual, screenW, screenH, panelH int) (dx, dy int) {
	if a == nil || v.Phase != PhaseMapOut || v.Progress <= 0 || screenW <= 0 || screenH <= 0 {
		return 0, 0
	}
	tx := screenW / 2
	ty := screenH - panelH/2
	if panelH < 1 {
		ty = screenH - 18
	}
	cx := a.X + a.Width/2
	cy := a.Y + a.Height/2
	p := v.Progress
	return int(float64(tx-cx)*p + 0.5), int(float64(ty-cy)*p + 0.5)
}

// SlideFade is the workspace-switch dim (outgoing dims, incoming brightens).
func SlideFade(ox, screenW int) float64 {
	if ox == 0 || screenW <= 0 {
		return 1
	}
	a := 1 - 0.32*float64(absInt(ox))/float64(screenW)
	if a < 0.45 {
		return 0.45
	}
	if a > 1 {
		return 1
	}
	return a
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
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

// EaseInOutCubic is 0→1, soft start and settle (workspace slide).
func EaseInOutCubic(t float64) float64 {
	t = clamp01(t)
	if t < 0.5 {
		return 4 * t * t * t
	}
	u := -2*t + 2
	return 1 - u*u*u/2
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
