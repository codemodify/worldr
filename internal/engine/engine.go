// Package engine holds the scene and window-actor model.
//
// Client surfaces become textured window actors. Phase 1–3 composite on a
// CPU framebuffer (BGRA8) and upload/present; a GPU effect graph comes later.
package engine

import (
	"sync"
	"time"
)

// Actor is a window (or other scene object) in the cinematic desktop.
type Actor struct {
	X, Y          int
	Width, Height int
	Stride        int
	Pixels        []byte // BGRA8 / XRGB8888, length >= Stride*Height
	Title         string
	AppID         string
	Focused       bool
	Born          time.Time // map-in start (zero = already settled)
	UnmapAt       time.Time // map-out start (zero = mapped)
	FocusPulse    time.Time // last focus-gain
}

// Scene holds window actors. The frame loop must reuse storage.
type Scene struct {
	mu     sync.Mutex
	actors []*Actor
	tier   Tier
}

// NewScene returns an empty scene.
func NewScene() *Scene { return &Scene{} }

// SetTheater selects the v0 window theater. Off removes actors immediately.
func (s *Scene) SetTheater(t Tier) {
	s.mu.Lock()
	s.tier = t
	s.mu.Unlock()
}

// Theater is the current effects tier.
func (s *Scene) Theater() Tier {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tier
}

// Actors returns a snapshot of current actors (the slice is a copy; actors are live).
func (s *Scene) Actors() []*Actor {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Actor, len(s.actors))
	copy(out, s.actors)
	return out
}

// Add registers an actor and starts map-in when theater is on.
func (s *Scene) Add(a *Actor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tier != TierOff && a != nil && a.Born.IsZero() {
		a.Born = time.Now()
	}
	s.actors = append(s.actors, a)
}

// Remove starts map-out, or drops immediately when theater is off.
func (s *Scene) Remove(a *Actor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tier != TierOff && a != nil && a.UnmapAt.IsZero() {
		a.UnmapAt = time.Now()
		return
	}
	s.dropLocked(a)
}

func (s *Scene) dropLocked(a *Actor) {
	dst := s.actors[:0]
	for _, x := range s.actors {
		if x != a {
			dst = append(dst, x)
		}
	}
	s.actors = dst
}

// Sweep drops actors whose map-out has finished.
func (s *Scene) Sweep(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tier == TierOff {
		return
	}
	dst := s.actors[:0]
	for _, a := range s.actors {
		if a != nil && !a.UnmapAt.IsZero() && now.Sub(a.UnmapAt) >= MapOutDuration {
			continue
		}
		dst = append(dst, a)
	}
	s.actors = dst
}

// HasActors reports whether any window is mapped.
func (s *Scene) HasActors() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.actors) > 0
}

// FocusAt marks the top-most actor containing (px,py) focused.
func (s *Scene) FocusAt(px, py int, titleH, border int) *Actor {
	s.mu.Lock()
	defer s.mu.Unlock()
	var hit, prev *Actor
	for _, a := range s.actors {
		if a.Focused {
			prev = a
			break
		}
	}
	for i := len(s.actors) - 1; i >= 0; i-- {
		a := s.actors[i]
		if !a.UnmapAt.IsZero() {
			continue
		}
		l, t := a.X-border, a.Y-titleH
		r, b := a.X+a.Width+border, a.Y+a.Height+border
		if px >= l && px < r && py >= t && py < b {
			hit = a
			break
		}
	}
	for _, a := range s.actors {
		a.Focused = a == hit
	}
	if hit != nil && hit != prev {
		hit.FocusPulse = time.Now()
	}
	return hit
}

// FocusActor marks a as the focused window (expose pick).
func (s *Scene) FocusActor(a *Actor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var prev *Actor
	for _, x := range s.actors {
		if x.Focused {
			prev = x
		}
		x.Focused = x == a
	}
	if a != nil && a != prev {
		a.FocusPulse = time.Now()
	}
}

// PlaceNew puts a newly mapped window in an empty-ish slot.
func (s *Scene) PlaceNew(a *Actor, screenW, screenH, border, titleH int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for range s.actors {
		n++
	}
	a.X = 48 + (n*32)%max(1, screenW/3)
	a.Y = titleH + 48 + (n*32)%max(1, screenH/3)
	if a.X+a.Width+border > screenW {
		a.X = border + 16
	}
	if a.Y+a.Height+border > screenH {
		a.Y = titleH + 16
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
