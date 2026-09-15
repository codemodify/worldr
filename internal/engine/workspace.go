package engine

import (
	"strconv"
	"time"
)

// Virtual desktops v0 — 2–4 workspaces, default 3.
const (
	WorkspaceMin      = 2
	WorkspaceMax      = 4
	WorkspaceDefault  = 3
	WorkspaceDuration = MapInDuration // 260ms slide
)

// ClampWorkspaces snaps n into [2,4]. Non-positive becomes the default.
func ClampWorkspaces(n int) int {
	if n <= 0 {
		return WorkspaceDefault
	}
	if n < WorkspaceMin {
		return WorkspaceMin
	}
	if n > WorkspaceMax {
		return WorkspaceMax
	}
	return n
}

// WrapWorkspace folds i into [0,n).
func WrapWorkspace(i, n int) int {
	if n <= 0 {
		return 0
	}
	i %= n
	if i < 0 {
		i += n
	}
	return i
}

// SwitchDir is +1 when moving "right" (higher index, or last→first wrap).
func SwitchDir(from, to, n int) int {
	if n <= 0 || from == to {
		return 0
	}
	from = WrapWorkspace(from, n)
	to = WrapWorkspace(to, n)
	right := (to - from + n) % n
	left := (from - to + n) % n
	if right <= left {
		return 1
	}
	return -1
}

// SlideOffsets are pixel X shifts for the outgoing / incoming desktop.
// t is 0..1 (already eased). dir +1 means the incoming desktop comes from the right.
func SlideOffsets(dir, screenW int, t float64) (fromOff, toOff int) {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	if dir == 0 || screenW <= 0 {
		return 0, 0
	}
	px := int(t*float64(screenW) + 0.5)
	return -dir * px, dir * (screenW - px)
}

// WorkspaceDraw is the per-frame pager pose for CompositeDesktop.
type WorkspaceDraw struct {
	Count  int
	Active int
	From   int
	To     int
	Dir    int
	T      float64 // 0 = still on From, 1 = settled on To
}

// OffsetFor returns the X shift for an actor on workspace ws, or false to hide it.
func (d WorkspaceDraw) OffsetFor(ws, screenW int) (ox int, show bool) {
	if d.Count <= 1 {
		return 0, true
	}
	if d.T < 1 && d.From != d.To && d.Dir != 0 {
		fo, to := SlideOffsets(d.Dir, screenW, d.T)
		if ws == d.From {
			return fo, true
		}
		if ws == d.To {
			return to, true
		}
		return 0, false
	}
	return 0, ws == d.Active
}

// SetWorkspaces clamps and stores the pager size.
func (s *Scene) SetWorkspaces(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wsN = ClampWorkspaces(n)
	if s.wsActive >= s.wsN {
		s.wsActive = s.wsN - 1
	}
	if s.wsActive < 0 {
		s.wsActive = 0
	}
}

// WorkspaceCount is how many virtual desktops exist.
func (s *Scene) WorkspaceCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wsN < WorkspaceMin {
		return WorkspaceDefault
	}
	return s.wsN
}

// ActiveWorkspace is the current desktop index.
func (s *Scene) ActiveWorkspace() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wsActive
}

// ActorsOn returns a snapshot of actors on workspace ws.
func (s *Scene) ActorsOn(ws int) []*Actor {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Actor, 0, len(s.actors))
	for _, a := range s.actors {
		if a != nil && a.Workspace == ws {
			out = append(out, a)
		}
	}
	return out
}

// WorkspaceLabel is the 1-based "N/M" pager caption (empty when count < 1).
func WorkspaceLabel(active, count int) string {
	if count < 1 {
		return ""
	}
	if active < 0 {
		active = 0
	}
	if active >= count {
		active = count - 1
	}
	return strconv.Itoa(active+1) + "/" + strconv.Itoa(count)
}

// Occupied is true per desktop when at least one actor lives there.
func (s *Scene) Occupied() []bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.wsN
	if n < WorkspaceMin {
		n = WorkspaceDefault
	}
	out := make([]bool, n)
	for _, a := range s.actors {
		if a != nil && a.Workspace >= 0 && a.Workspace < n {
			out[a.Workspace] = true
		}
	}
	return out
}

// SwitchTo jumps to desktop i (clamped, no wrap). Starts a slide when i changes.
func (s *Scene) SwitchTo(i int, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.wsN
	if n < WorkspaceMin {
		n = WorkspaceDefault
		s.wsN = n
	}
	if i < 0 {
		i = 0
	}
	if i >= n {
		i = n - 1
	}
	if i == s.wsActive {
		return false
	}
	s.wsDir = SwitchDir(s.wsActive, i, n)
	s.wsFrom = s.wsActive
	s.wsActive = i
	s.wsSince = now
	s.focusDesktopLocked()
	return true
}

// StepWorkspace moves delta desktops and wraps.
func (s *Scene) StepWorkspace(delta int, now time.Time) bool {
	s.mu.Lock()
	n := s.wsN
	if n < WorkspaceMin {
		n = WorkspaceDefault
		s.wsN = n
	}
	to := WrapWorkspace(s.wsActive+delta, n)
	s.mu.Unlock()
	return s.SwitchTo(to, now)
}

// MoveActor assigns a (and actors that name it as Owner) to dest.
// dest is wrapped into the pager. Empty dest stays addressable.
func (s *Scene) MoveActor(a *Actor, dest int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.moveActorLocked(a, dest)
}

func (s *Scene) workspaceNLocked() int {
	n := s.wsN
	if n < WorkspaceMin {
		n = WorkspaceDefault
		s.wsN = n
	}
	return n
}

func (s *Scene) moveActorLocked(a *Actor, dest int) bool {
	if a == nil {
		return false
	}
	if a.NoChrome && a.Owner != nil {
		a = a.Owner
	}
	n := s.workspaceNLocked()
	dest = WrapWorkspace(dest, n)
	if a.Workspace == dest {
		return false
	}
	for _, x := range s.actors {
		if x == nil {
			continue
		}
		if x == a || x.Owner == a {
			x.Workspace = dest
		}
	}
	return true
}

// MoveFocused assigns the focused window to dest = wrap(active+delta)
// and follows with a slide. No focused window still switches (empty
// dest stays addressable). Returns false when dest == active.
func (s *Scene) MoveFocused(delta int, now time.Time) bool {
	s.mu.Lock()
	n := s.workspaceNLocked()
	to := WrapWorkspace(s.wsActive+delta, n)
	if to == s.wsActive {
		s.mu.Unlock()
		return false
	}
	var hit *Actor
	for _, a := range s.actors {
		if a == nil || !a.Focused || !a.UnmapAt.IsZero() {
			continue
		}
		hit = a
		break
	}
	if hit != nil {
		s.moveActorLocked(hit, to)
	}
	s.mu.Unlock()
	return s.SwitchTo(to, now)
}

// WorkspacePose is the current slide (T already eased).
func (s *Scene) WorkspacePose(now time.Time) WorkspaceDraw {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.wsN
	if n < WorkspaceMin {
		n = WorkspaceDefault
	}
	t := 1.0
	from, to, dir := s.wsActive, s.wsActive, 0
	if !s.wsSince.IsZero() && s.wsFrom != s.wsActive {
		p := float64(now.Sub(s.wsSince)) / float64(WorkspaceDuration)
		if p < 0 {
			p = 0
		}
		if p > 1 {
			p = 1
		}
		t = EaseOutCubic(p)
		from, to, dir = s.wsFrom, s.wsActive, s.wsDir
		if p >= 1 {
			from, to, dir = s.wsActive, s.wsActive, 0
			t = 1
		}
	}
	return WorkspaceDraw{Count: n, Active: s.wsActive, From: from, To: to, Dir: dir, T: t}
}

func (s *Scene) focusDesktopLocked() {
	var hit *Actor
	for _, a := range s.actors {
		if a == nil || a.Workspace != s.wsActive || !a.UnmapAt.IsZero() {
			continue
		}
		if a.Focused || hit == nil {
			hit = a
		}
	}
	for _, a := range s.actors {
		if a != nil {
			a.Focused = a == hit
		}
	}
	if hit != nil {
		hit.FocusPulse = time.Now()
	}
}
