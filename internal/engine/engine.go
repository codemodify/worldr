// Package engine holds the scene and window-actor model.
//
// Client surfaces become textured window actors. Phase 1–3 composite on a
// CPU framebuffer (BGRA8) and upload/present; a GPU effect graph comes later.
package engine

import "sync"

// Actor is a window (or other scene object) in the cinematic desktop.
type Actor struct {
	X, Y          int
	Width, Height int
	Stride        int
	Pixels        []byte // BGRA8 / XRGB8888, length >= Stride*Height
	Title         string
	AppID         string
	Focused       bool
}

// Scene holds window actors. The frame loop must reuse storage.
type Scene struct {
	mu     sync.Mutex
	actors []*Actor
}

// NewScene returns an empty scene.
func NewScene() *Scene { return &Scene{} }

// Actors returns a snapshot of current actors (the slice is a copy; actors are live).
func (s *Scene) Actors() []*Actor {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Actor, len(s.actors))
	copy(out, s.actors)
	return out
}

// Add registers an actor.
func (s *Scene) Add(a *Actor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actors = append(s.actors, a)
}

// Remove drops an actor.
func (s *Scene) Remove(a *Actor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dst := s.actors[:0]
	for _, x := range s.actors {
		if x != a {
			dst = append(dst, x)
		}
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
	var hit *Actor
	for i := len(s.actors) - 1; i >= 0; i-- {
		a := s.actors[i]
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
	return hit
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
