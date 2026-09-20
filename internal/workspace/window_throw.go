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
	time  uint32 // Input timestamps in milliseconds, including uint32 wrap.
	x, y  float32
	depth float32
}

// Motion is transient. Its complete path shares the drag's single undo entry;
// redo and saved documents restore a position, never replay the animation.
type windowThrow struct {
	next                          *windowThrow
	origin                        ApplicationLayouts
	selected                      uint32
	editID                        uint64
	liveIDs                       [MaxApplicationLayouts]uint64
	vx, vy, vz, elapsed, duration float64
	wallCollisionTime             float64
	wallCollisionSpeed            float64
	wallImpactSent                bool
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
	w.appendWindowDragSample(windowDragSample{time: event.Time, x: placement.X, y: placement.Y, depth: placement.Depth}, release)
}

func (w *Workspace) appendWindowDragSample(sample windowDragSample, release bool) {
	p := &w.pointer
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

// A wheel packet reports displacement at one timestamp rather than continuous
// pointer motion. Keep a recent sample when one exists; after a long hold,
// synthesize a short bounded interval for that discrete impulse. One deliberate
// scroll followed by release can then carry depth momentum without depending on
// an unrelated pointer move before it.
func (w *Workspace) sampleWindowDepthStep(event experience.Event, before, after ApplicationPlacement) {
	p := &w.pointer
	timedWrap := false
	if event.Time == 0 && p.dragSampleCount > 0 {
		span := event.Time - p.dragSamples[p.dragSampleCount-1].time
		timedWrap = span > 0 && span <= 120
	}
	if event.Time == 0 && !timedWrap {
		// Timestamp-free synthetic input may reposition a window, but must not
		// manufacture velocity from an invented clock interval.
		p.dragSampleCount = 0
		w.appendWindowDragSample(windowDragSample{x: after.X, y: after.Y, depth: after.Depth}, false)
		return
	}
	useRecent := false
	if p.dragSampleCount > 0 {
		span := event.Time - p.dragSamples[p.dragSampleCount-1].time
		useRecent = span >= 8 && span <= 120
	}
	if !useRecent {
		p.dragSampleCount = 0
		w.appendWindowDragSample(windowDragSample{
			time: event.Time - 16, x: before.X, y: before.Y, depth: before.Depth,
		}, false)
	}
	w.appendWindowDragSample(windowDragSample{
		time: event.Time, x: after.X, y: after.Y, depth: after.Depth,
	}, false)
}

func (w *Workspace) releaseWindowDrag() {
	p := &w.pointer
	if !p.dragged || p.dragSampleCount < 2 {
		w.commitPointer()
		return
	}
	last := p.dragSamples[p.dragSampleCount-1]
	var vx, vy, vz float64
	for _, first := range p.dragSamples[:p.dragSampleCount] {
		span := last.time - first.time
		if span >= 8 && span <= 120 {
			vx, vy = float64(last.x-first.x)*1000/float64(span), float64(last.y-first.y)*1000/float64(span)
			vz = float64(last.depth-first.depth) * 1000 / float64(span)
			break
		}
	}
	speed := math.Sqrt(vx*vx + vy*vy + vz*vz)
	if !finite(speed) || speed < windowThrowMinSpeed {
		w.commitPointer()
		return
	}
	if speed > windowThrowMaxSpeed {
		vx, vy, vz = vx*windowThrowMaxSpeed/speed, vy*windowThrowMaxSpeed/speed, vz*windowThrowMaxSpeed/speed
		speed = windowThrowMaxSpeed
	}
	v := w.m.applicationState
	motion := &windowThrow{origin: v.Layouts, selected: v.movementSelection(), vx: vx, vy: vy, vz: vz,
		duration: math.Min(3, math.Log(speed/windowThrowStopSpeed)/windowThrowFriction)}
	motion.wallCollisionTime = -1
	if w.desktop {
		motion.wallCollisionTime, motion.wallCollisionSpeed = energyWallCollision(motion.origin, motion.selected, vz)
	}
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
	for motion := w.windowThrow; motion != nil; {
		next := motion.next
		w.updateOneWindowThrow(motion, dt)
		motion = next
	}
}

func (w *Workspace) updateOneWindowThrow(motion *windowThrow, dt time.Duration) {
	previousElapsed := motion.elapsed
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
	effectiveElapsed := motion.elapsed
	if fraction < 1 {
		decay := -math.Expm1(-windowThrowFriction * motion.elapsed)
		effectiveElapsed = -math.Log1p(-fraction*decay) / windowThrowFriction
	}
	if depthElapsed, hit := windowThrowDepthLimitTime(motion, effectiveElapsed); hit {
		effectiveElapsed = depthElapsed
	}
	stoppedAtBoundary := effectiveElapsed < motion.elapsed
	distance = -math.Expm1(-windowThrowFriction*effectiveElapsed) / windowThrowFriction
	dx, dy = motion.vx*distance, motion.vy*distance
	dz := windowThrowDepthDisplacement(motion, effectiveElapsed)
	next := w.Document()
	for i, origin := range motion.origin {
		if motion.selected&(1<<i) != 0 {
			next.View.Application.Layouts[i].X = float32(float64(origin.X) + dx)
			next.View.Application.Layouts[i].Y = float32(float64(origin.Y) + dy)
			next.View.Application.Layouts[i].Depth = float32(float64(origin.Depth) + dz)
		}
	}
	if err := next.Validate(); err != nil {
		w.finishOneWindowThrow(motion)
		return
	}
	w.install(next, false)
	if !motion.wallImpactSent && motion.wallCollisionTime >= previousElapsed && motion.wallCollisionTime <= effectiveElapsed {
		motion.wallImpactSent = true
		impactDistance := -math.Expm1(-windowThrowFriction*motion.wallCollisionTime) / windowThrowFriction
		var x, y float64
		count := 0
		rear := float32(math.MaxFloat32)
		for i, origin := range motion.origin {
			if motion.selected&(1<<i) != 0 {
				rear = min(rear, origin.Depth)
			}
		}
		for i, origin := range motion.origin {
			if motion.selected&(1<<i) == 0 || abs(origin.Depth-rear) > .0001 {
				continue
			}
			x += float64(origin.X) + motion.vx*impactDistance
			y += float64(origin.Y) + motion.vy*impactDistance
			count++
		}
		if count > 0 {
			offset := time.Duration(max(0.0, motion.wallCollisionTime-previousElapsed) * float64(time.Second))
			w.queueEnergyWallImpact(float32(x/float64(count)), float32(y/float64(count)), float32(motion.wallCollisionSpeed/windowThrowMaxSpeed), offset)
		}
	}
	w.refreshWindowThrowEdit(motion)
	if stoppedAtBoundary || motion.elapsed >= motion.duration {
		w.finishOneWindowThrow(motion)
	}
}

// energyWallCollision returns the exact impact time and normal speed for an
// exponentially damped depth throw. A negative time means the release cannot
// reach the rear wall, even at its infinite-time limit.
func energyWallCollision(origin ApplicationLayouts, selected uint32, vz float64) (float64, float64) {
	if vz >= 0 || selected == 0 {
		return -1, 0
	}
	rear := float64(math.MaxFloat32)
	for i, placement := range origin {
		if selected&(1<<i) != 0 {
			rear = math.Min(rear, float64(placement.Depth))
		}
	}
	limit := float64(energyWallDepth + energyWindowClearance)
	required := limit - rear
	if required >= 0 {
		// A legacy placement already behind the wall is allowed to move toward
		// the camera; motion farther into the wall contacts immediately.
		return 0, -vz
	}
	if vz/windowThrowFriction >= required {
		return -1, 0
	}
	remaining := 1 - windowThrowFriction*required/vz
	if remaining <= 0 || remaining >= 1 {
		return -1, 0
	}
	t := -math.Log(remaining) / windowThrowFriction
	speed := -vz * math.Exp(-windowThrowFriction*t)
	return t, speed
}

func windowThrowDepthDisplacement(motion *windowThrow, elapsed float64) float64 {
	if motion.vz == 0 {
		return 0
	}
	if motion.wallCollisionTime < 0 || elapsed <= motion.wallCollisionTime {
		return motion.vz * -math.Expm1(-windowThrowFriction*elapsed) / windowThrowFriction
	}
	rear := float64(math.MaxFloat32)
	for i, placement := range motion.origin {
		if motion.selected&(1<<i) != 0 {
			rear = math.Min(rear, float64(placement.Depth))
		}
	}
	hitDistance := float64(energyWallDepth+energyWindowClearance) - rear
	reboundVelocity := motion.wallCollisionSpeed * float64(energyWallRestitution)
	reboundTime := elapsed - motion.wallCollisionTime
	return hitDistance + reboundVelocity*-math.Expm1(-windowThrowFriction*reboundTime)/windowThrowFriction
}

// windowThrowDepthLimitTime finds the first instant at which a rigid moving
// group reaches either document depth bound. Desktop rebounds use the front
// bound; standalone rearward throws still honor the shared -40 limit. Every
// path uses the motion integrator's closed-form exponential, so coarse and fine
// frames stop at the same point.
func windowThrowDepthLimitTime(motion *windowThrow, maxElapsed float64) (float64, bool) {
	front, rear := float64(-math.MaxFloat32), float64(math.MaxFloat32)
	for i, placement := range motion.origin {
		if motion.selected&(1<<i) != 0 {
			front = math.Max(front, float64(placement.Depth))
			rear = math.Min(rear, float64(placement.Depth))
		}
	}
	if front == float64(-math.MaxFloat32) {
		return 0, false
	}
	var limitTime float64
	if motion.wallCollisionTime < 0 {
		if motion.vz == 0 {
			return 0, false
		}
		target := 40 - front
		if motion.vz < 0 {
			target = -40 - rear
		}
		if motion.vz > 0 && target <= 0 || motion.vz < 0 && target >= 0 {
			return 0, true
		}
		remaining := 1 - windowThrowFriction*target/motion.vz
		if remaining <= 0 || remaining >= 1 {
			return 0, false
		}
		limitTime = -math.Log(remaining) / windowThrowFriction
	} else {
		target := 40 - front
		hitDistance := float64(energyWallDepth+energyWindowClearance) - rear
		reboundVelocity := motion.wallCollisionSpeed * float64(energyWallRestitution)
		required := target - hitDistance
		if reboundVelocity <= 0 || required <= 0 {
			return motion.wallCollisionTime, motion.wallCollisionTime <= maxElapsed
		}
		remaining := 1 - windowThrowFriction*required/reboundVelocity
		if remaining <= 0 || remaining >= 1 {
			return 0, false
		}
		limitTime = motion.wallCollisionTime - math.Log(remaining)/windowThrowFriction
	}
	return limitTime, limitTime <= maxElapsed
}
