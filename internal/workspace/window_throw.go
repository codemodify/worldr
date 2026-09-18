package workspace

import (
	"math"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

const (
	windowThrowFriction  = 2.6
	windowThrowMinSpeed  = .35
	windowThrowMaxSpeed  = 10.0
	windowThrowStopSpeed = .025
)

type windowDragSample struct {
	time uint32 // Input timestamps in milliseconds, including uint32 wrap.
	x, y float32
}

// Motion is transient. Its complete path shares the drag's single undo entry;
// redo and saved documents restore a position, never replay the animation.
type windowThrow struct {
	next                      *windowThrow
	origin                    ApplicationLayouts
	selected                  uint32
	editID                    uint64
	liveIDs                   [MaxApplicationLayouts]uint64
	vx, vy, elapsed, duration float64
}

func (w *Workspace) initializeWindowDrag(event experience.Event) {
	if w.pointer.kind != captureApplicationPlacement {
		return
	}
	w.pointer.windowDrag, w.pointer.dragViewport = true, w.viewport
	w.pointer.pressX, w.pointer.pressY = event.X, event.Y
	w.pointer.scale = w.scale
	w.ownWindowDragButton(applicationButton(event))
	w.sampleWindowDrag(event, false)
}

func (w *Workspace) sampleWindowDrag(event experience.Event, release bool) {
	p := &w.pointer
	dx, dy := (event.X-p.pressX)/p.scale, (event.Y-p.pressY)/p.scale
	p.dragged = p.dragged || dx*dx+dy*dy >= 16
	i := w.m.applicationState.index(w.m.applicationState.Active)
	if i < 0 {
		return
	}
	placement := w.m.applicationState.Layouts[i]
	sample := windowDragSample{time: event.Time, x: placement.X, y: placement.Y}
	n := p.dragSampleCount
	if n > 0 {
		last := p.dragSamples[n-1]
		delta := sample.time - last.time
		if delta == 0 {
			// Untimed synthetic input can drag, but cannot manufacture velocity.
			p.dragSamples[n-1] = sample
			return
		}
		if delta > 250 {
			// Pauses and backwards timestamps cannot carry old momentum.
			p.dragSampleCount = 0
		} else if delta < 4 && !release {
			return
		}
	}
	if p.dragSampleCount == len(p.dragSamples) {
		copy(p.dragSamples[:], p.dragSamples[1:])
		p.dragSampleCount--
	}
	p.dragSamples[p.dragSampleCount] = sample
	p.dragSampleCount++
}

func (w *Workspace) releaseWindowDrag() {
	p := &w.pointer
	if w.m.reducedMotion || !p.dragged || p.dragSampleCount < 2 {
		w.commitPointer()
		return
	}
	last := p.dragSamples[p.dragSampleCount-1]
	var vx, vy float64
	for _, first := range p.dragSamples[:p.dragSampleCount] {
		span := last.time - first.time
		if span >= 8 && span <= 120 {
			vx, vy = float64(last.x-first.x)*1000/float64(span), float64(last.y-first.y)*1000/float64(span)
			break
		}
	}
	speed := math.Hypot(vx, vy)
	if !finite(speed) || speed < windowThrowMinSpeed {
		w.commitPointer()
		return
	}
	if speed > windowThrowMaxSpeed {
		vx, vy = vx*windowThrowMaxSpeed/speed, vy*windowThrowMaxSpeed/speed
		speed = windowThrowMaxSpeed
	}
	v := w.m.applicationState
	motion := &windowThrow{origin: v.Layouts, selected: v.movementSelection(), vx: vx, vy: vy,
		duration: math.Min(3, math.Log(speed/windowThrowStopSpeed)/windowThrowFriction)}
	for _, surface := range w.applicationSurfaces {
		i := v.index(surface.Key)
		if i >= 0 && motion.selected&(1<<i) != 0 {
			motion.liveIDs[i] = surface.ID
		}
	}
	// Reserve the gesture's edit now, in release order. Later selection or
	// independent throws must not change its Undo order or become part of it.
	after := w.Document()
	before := windowDragBefore(*p, after)
	motion.editID = w.record(before, after, fieldApplicationLayout)
	if motion.editID == 0 {
		// A gesture can return to its starting point with nonzero velocity.
		// Its upcoming coast still needs an edit reserved ahead of later input.
		motion.editID = w.appendEdit(edit{before: before, after: after, fields: fieldApplicationLayout})
	}
	motion.next, w.windowThrow = w.windowThrow, motion
	w.pointer = pointerCapture{}
}

func (w *Workspace) finishWindowThrow() {
	for w.windowThrow != nil {
		w.finishOneWindowThrow(w.windowThrow)
	}
}

func (w *Workspace) finishOneWindowThrow(motion *windowThrow) {
	for link := &w.windowThrow; *link != nil; link = &(*link).next {
		if *link != motion {
			continue
		}
		*link = motion.next
		w.refreshWindowThrowEdit(motion)
		return
	}
}

func (w *Workspace) refreshWindowThrowEdit(motion *windowThrow) {
	for i := len(w.history) - 1; i >= 0; i-- {
		entry := &w.history[i]
		if entry.id != motion.editID {
			continue
		}
		for slot, origin := range motion.origin {
			if motion.selected&(1<<slot) == 0 {
				continue
			}
			current := w.m.applicationState.index(origin.Key)
			target := entry.after.View.Application.index(origin.Key)
			if current >= 0 && target >= 0 {
				entry.after.View.Application.Layouts[target] = w.m.applicationState.Layouts[current]
			}
		}
		entry.after.View.Application.aliases()
		return
	}
	// A very old gesture may have left the bounded history while still moving.
}

// Stop only the independently moving group that owns the routed target.
func (w *Workspace) stopWindowThrows(selected uint32) {
	for motion := w.windowThrow; motion != nil; {
		next := motion.next
		if motion.selected&selected != 0 {
			w.finishOneWindowThrow(motion)
		}
		motion = next
	}
}

func (w *Workspace) stopWindowThrowForKey(key string) {
	if index := w.m.applicationState.index(key); index >= 0 {
		w.stopWindowThrows(1 << index)
	}
}

// Other windows can move while a gesture is held or coasting. Its edit/cancel
// must contain only its own members, not snapshots of their unrelated motion.
func windowMoveBefore(start, current Document, selected uint32) Document {
	before := current
	for i, placement := range start.View.Application.Layouts {
		if selected&(1<<i) == 0 {
			continue
		}
		if index := before.View.Application.index(placement.Key); index >= 0 {
			before.View.Application.Layouts[index] = placement
		}
	}
	before.View.Application.Active = start.View.Application.Active
	before.View.Application.Selected = start.View.Application.Selected
	before.View.Application.aliases()
	return before
}

func windowDragBefore(pointer pointerCapture, current Document) Document {
	before := windowMoveBefore(pointer.start, current, pointer.windowSelection)
	if pointer.dragViewChanged {
		before.View.Application.Reading = pointer.start.View.Application.Reading
		before.View.Application.Overview = pointer.start.View.Application.Overview
		before.View.Application.Placing = pointer.start.View.Application.Placing
	}
	return before
}

func (w *Workspace) updateWindowThrow(dt time.Duration) {
	if dt <= 0 {
		return
	}
	if w.m.reducedMotion {
		w.finishWindowThrow()
		return
	}
	for motion := w.windowThrow; motion != nil; {
		next := motion.next
		w.updateOneWindowThrow(motion, dt)
		motion = next
	}
}

func (w *Workspace) updateOneWindowThrow(motion *windowThrow, dt time.Duration) {
	motion.elapsed = math.Min(motion.duration, motion.elapsed+dt.Seconds())
	// Integrate from the release point, not the previous frame. Low and high
	// frame rates therefore reach exactly the same resting position.
	distance := -math.Expm1(-windowThrowFriction*motion.elapsed) / windowThrowFriction
	dx, dy := motion.vx*distance, motion.vy*distance
	fraction := 1.0
	for i, origin := range motion.origin {
		if motion.selected&(1<<i) == 0 {
			continue
		}
		if w.m.applicationState.Layouts[i].Key != origin.Key {
			w.finishOneWindowThrow(motion)
			return
		}
		// Stop the entire group at the first bound, retaining its arrangement.
		for axis, delta := range []float64{dx, dy} {
			position := float64(origin.X)
			if axis == 1 {
				position = float64(origin.Y)
			}
			if delta > 0 {
				fraction = math.Min(fraction, (100-position)/delta)
			}
			if delta < 0 {
				fraction = math.Min(fraction, (-100-position)/delta)
			}
		}
	}
	next := w.Document()
	for i, origin := range motion.origin {
		if motion.selected&(1<<i) != 0 {
			next.View.Application.Layouts[i].X = float32(float64(origin.X) + dx*fraction)
			next.View.Application.Layouts[i].Y = float32(float64(origin.Y) + dy*fraction)
		}
	}
	if err := next.Validate(); err != nil {
		w.finishOneWindowThrow(motion)
		return
	}
	w.install(next, false)
	w.refreshWindowThrowEdit(motion)
	if fraction < 1 || motion.elapsed >= motion.duration {
		w.finishOneWindowThrow(motion)
	}
}
