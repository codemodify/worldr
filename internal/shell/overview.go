package shell

import (
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/input"
)

// Overview is the expose/grid state machine.
type Overview struct {
	Want   bool
	Since  time.Time
	Select int
}

// Toggle opens or closes expose.
func (o *Overview) Toggle(now time.Time) {
	o.Want = !o.Want
	o.Since = now
	if !o.Want {
		o.Select = 0
	}
}

// Open forces expose on (demo / click no-op if already).
func (o *Overview) Open(now time.Time) {
	if o.Want {
		return
	}
	o.Want = true
	o.Since = now
}

// Close forces expose off.
func (o *Overview) Close(now time.Time) {
	if !o.Want {
		return
	}
	o.Want = false
	o.Since = now
}

// Progress is 0 (desktop) … 1 (full grid). Eases both ways.
func (o *Overview) Progress(now time.Time) float64 {
	if o.Since.IsZero() {
		if o.Want {
			return 1
		}
		return 0
	}
	p := float64(now.Sub(o.Since)) / float64(engine.OverviewDuration)
	p = clamp01f(p)
	e := engine.EaseInOutCubic(p)
	if o.Want {
		return e
	}
	return 1 - e
}

// Live is true while we should steal clicks/keys for expose.
func (o *Overview) Live(now time.Time) bool {
	return o.Want || o.Progress(now) > 0.04
}

func clamp01f(t float64) float64 {
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	return t
}

func handleOverviewKeys(ov *Overview, ptr *input.Pointer, scene *engine.Scene, now time.Time, _w, _h int, metaHeld *bool, stealNav bool, ctrlHeld, altHeld *bool) map[uint32]bool {
	consumed := map[uint32]bool{}
	if ov == nil || ptr == nil {
		return consumed
	}
	actors := desktopActors(scene)
	n := len(actors)
	for _, k := range ptr.Keys {
		if isMeta(k.Code) {
			*metaHeld = k.Pressed
			consumed[k.Code] = true
			continue
		}
		if !k.Pressed {
			continue
		}
		if isOverviewToggle(k.Code, *metaHeld) {
			ov.Toggle(now)
			if ov.Want {
				ov.Select = focusedIndex(actors)
			}
			consumed[k.Code] = true
			ptr.Quit = false
			continue
		}
		if stealNav {
			continue
		}
		if ctrlHeld != nil && altHeld != nil && *ctrlHeld && *altHeld &&
			(isEvdev(k.Code, keyLeft) || isEvdev(k.Code, keyRight)) {
			continue
		}
		if ov.Want && isEvdev(k.Code, keyEsc) {
			ov.Close(now)
			consumed[k.Code] = true
			ptr.Quit = false
			continue
		}
		if ov.Want && (isEvdev(k.Code, keyRight) || (isEvdev(k.Code, keyTab) && !*metaHeld)) {
			ov.moveSelect(1, n)
			if ov.Select >= 0 && ov.Select < n {
				scene.FocusActor(actors[ov.Select])
			}
			consumed[k.Code] = true
			continue
		}
		if ov.Want && isEvdev(k.Code, keyLeft) {
			ov.moveSelect(-1, n)
			if ov.Select >= 0 && ov.Select < n {
				scene.FocusActor(actors[ov.Select])
			}
			consumed[k.Code] = true
			continue
		}
		if ov.Want && isEvdev(k.Code, keyUp) {
			ov.moveSelect(-engine.GridCols(n), n)
			if ov.Select >= 0 && ov.Select < n {
				scene.FocusActor(actors[ov.Select])
			}
			consumed[k.Code] = true
			continue
		}
		if ov.Want && isEvdev(k.Code, keyDown) {
			ov.moveSelect(engine.GridCols(n), n)
			if ov.Select >= 0 && ov.Select < n {
				scene.FocusActor(actors[ov.Select])
			}
			consumed[k.Code] = true
			continue
		}
		if ov.Want && isEvdev(k.Code, keyEnter) {
			if ov.Select >= 0 && ov.Select < n {
				scene.FocusActor(actors[ov.Select])
			}
			ov.Close(now)
			consumed[k.Code] = true
			continue
		}
	}
	return consumed
}

func focusedIndex(actors []*engine.Actor) int {
	for i, a := range actors {
		if a != nil && a.Focused {
			return i
		}
	}
	return 0
}

func (o *Overview) moveSelect(delta, n int) {
	if n <= 0 {
		o.Select = 0
		return
	}
	o.Select = (o.Select + delta) % n
	if o.Select < 0 {
		o.Select += n
	}
}

// pickOverview focuses the tile under (x,y) and leaves expose.
func pickOverview(ov *Overview, scene *engine.Scene, x, y, w, h int, now time.Time) bool {
	if ov == nil || scene == nil || !ov.Want {
		return false
	}
	actors := desktopActors(scene)
	cells := engine.LayoutGrid(len(actors), w, h)
	i := engine.HitGrid(cells, x, y)
	if i < 0 || i >= len(actors) {
		return false
	}
	ov.Select = i
	scene.FocusActor(actors[i])
	ov.Close(now)
	return true
}
