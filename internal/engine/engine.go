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
	BufW, BufH    int // pixel buffer; 0 = same as Width/Height (scale 1 / no viewport)
	Stride        int
	Pixels        []byte // BGRA8 / XRGB8888, length >= Stride*Height
	Title         string
	AppID         string
	Focused       bool
	NoChrome      bool      // popup / subsurface — no SSD title or frame
	Owner         *Actor    // parent toplevel for transients
	X11Win        uint32    // X11 window id when this actor is rootless Xwayland
	GPUSlot       int       // 1-based retained dmabuf; 0 = CPU pixels only
	Born          time.Time // map-in start (zero = already settled)
	UnmapAt       time.Time // map-out start (zero = mapped)
	FocusPulse    time.Time // last focus-gain
	Workspace     int       // virtual desktop (0-based)
}

// PixelSize is the attached buffer size (falls back to the logical window).
func (a *Actor) PixelSize() (w, h int) {
	if a == nil {
		return 0, 0
	}
	if a.BufW > 0 && a.BufH > 0 {
		return a.BufW, a.BufH
	}
	return a.Width, a.Height
}

// ScaledBuffer reports a viewport or buffer-scale mismatch (need a scaled blit).
func (a *Actor) ScaledBuffer() bool {
	if a == nil {
		return false
	}
	bw, bh := a.PixelSize()
	return bw != a.Width || bh != a.Height
}

// Scene holds window actors. The frame loop must reuse storage.
type Scene struct {
	mu       sync.Mutex
	actors   []*Actor
	tier     Tier
	wsN      int
	wsActive int
	wsFrom   int
	wsDir    int
	wsSince  time.Time
}

// NewScene returns an empty scene with the default pager (3 desktops).
func NewScene() *Scene { return &Scene{wsN: WorkspaceDefault} }

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
	if a != nil {
		a.Workspace = s.wsActive
		if s.tier != TierOff && a.Born.IsZero() {
			a.Born = time.Now()
		}
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

// HitTop returns the top-most actor whose frame contains (px,py).
// NoChrome actors use the buffer rect only (no title/border).
func (s *Scene) HitTop(px, py, titleH, border int) *Actor {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hitTopLocked(px, py, titleH, border)
}

func (s *Scene) hitTopLocked(px, py, titleH, border int) *Actor {
	for i := len(s.actors) - 1; i >= 0; i-- {
		a := s.actors[i]
		if a == nil || !a.UnmapAt.IsZero() || a.Workspace != s.wsActive {
			continue
		}
		l, t, r, b := actorHitBox(a, titleH, border)
		if px >= l && px < r && py >= t && py < b {
			return a
		}
	}
	return nil
}

func actorHitBox(a *Actor, titleH, border int) (l, t, r, b int) {
	if a.NoChrome {
		return a.X, a.Y, a.X + a.Width, a.Y + a.Height
	}
	return a.X - border, a.Y - titleH, a.X + a.Width + border, a.Y + a.Height + border
}

// FocusAt marks the top-most actor containing (px,py) focused.
// A NoChrome hit focuses its Owner toplevel (menus do not steal window focus).
func (s *Scene) FocusAt(px, py int, titleH, border int) *Actor {
	s.mu.Lock()
	defer s.mu.Unlock()
	var prev *Actor
	for _, a := range s.actors {
		if a.Focused {
			prev = a
			break
		}
	}
	hit := s.hitTopLocked(px, py, titleH, border)
	if hit != nil && hit.NoChrome && hit.Owner != nil {
		hit = hit.Owner
	}
	for _, a := range s.actors {
		a.Focused = a == hit
	}
	if hit != nil && hit != prev {
		hit.FocusPulse = time.Now()
	}
	return hit
}

// Raise moves a to the top of the stacking order (drawn last).
func (s *Scene) Raise(a *Actor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a == nil {
		return
	}
	dst := s.actors[:0]
	for _, x := range s.actors {
		if x != a {
			dst = append(dst, x)
		}
	}
	s.actors = append(dst, a)
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
	for _, x := range s.actors {
		if x != nil && x.Workspace == s.wsActive && !x.NoChrome {
			n++
		}
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
