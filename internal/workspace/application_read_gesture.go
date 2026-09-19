package workspace

import "github.com/codemodify/worldr/internal/experience"

const (
	applicationReadDoubleClickMilliseconds = uint32(400)
	applicationReadDoubleClickDistance     = float32(10)
)

// applicationReadClick remembers only a completed, unmoved first click. Input
// timestamps are the compositor's wrapping uint32 millisecond clock.
type applicationReadClick struct {
	key      string
	id       uint64
	time     uint32
	x, y     float32
	prepared bool
}

func (w *Workspace) resetApplicationReadClick() {
	w.applicationReadClick = applicationReadClick{}
}

func (w *Workspace) applicationReadClickDistance() float32 {
	distance := applicationReadDoubleClickDistance * w.scale
	if distance <= 0 {
		return applicationReadDoubleClickDistance
	}
	return distance
}

// Moving outside the click neighborhood or scrolling between presses breaks
// the chain even if the pointer later returns before the timeout.
func (w *Workspace) observeApplicationReadInterruption(event experience.Event) {
	first := w.applicationReadClick
	if !first.prepared {
		return
	}
	switch event.Kind {
	case experience.PointerScroll:
		w.resetApplicationReadClick()
	case experience.PointerMove:
		dx, dy := event.X-first.x, event.Y-first.y
		distance := w.applicationReadClickDistance()
		if dx*dx+dy*dy > distance*distance {
			w.resetApplicationReadClick()
		}
	}
}

// takeApplicationReadDoubleClick consumes the old chain on every eligible
// press. A mismatch therefore becomes the first click of a new chain only
// after it completes; it can never combine with an older target or position.
func (w *Workspace) takeApplicationReadDoubleClick(surface experience.ApplicationSurface, event experience.Event) bool {
	first := w.applicationReadClick
	w.resetApplicationReadClick()
	if !first.prepared || first.id != surface.ID || first.key != surface.Key {
		return false
	}
	if event.Time-first.time > applicationReadDoubleClickMilliseconds {
		return false
	}
	distance := w.applicationReadClickDistance()
	dx, dy := event.X-first.x, event.Y-first.y
	return dx*dx+dy*dy <= distance*distance
}

// A reserved click does not preview placement for normal pointer tremor. Once
// it crosses the drag threshold, it becomes the existing movement gesture.
func (w *Workspace) prepareApplicationReadDrag(event experience.Event) bool {
	p := &w.pointer
	if !p.readClick || p.dragged {
		return true
	}
	dx, dy := event.X-p.pressX, event.Y-p.pressY
	threshold := float32(4) * p.scale
	if threshold <= 0 {
		threshold = 4
	}
	if dx*dx+dy*dy < threshold*threshold {
		return false
	}
	p.dragged = true
	w.resetApplicationReadClick()
	return true
}

func (w *Workspace) applicationReadSurface(id uint64, key string) (experience.ApplicationSurface, bool) {
	for _, surface := range w.applicationSurfaces {
		if surface.ID == id && surface.Key == key && w.inCurrentSpace(surface) {
			return surface, true
		}
	}
	return experience.ApplicationSurface{}, false
}

// toggleApplicationReadingFor is the shared direct-window Read operation used
// by the Super+double-click gesture and the square window control. It selects
// the pointed-to window first, keeps unrelated window momentum alive, and owns
// the interaction without handing keyboard focus to the application.
func (w *Workspace) toggleApplicationReadingFor(surface experience.ApplicationSurface) {
	w.resetApplicationReadClick()
	w.stopWindowThrowForKey(surface.Key)
	w.clearApplicationFocus()
	w.applicationRestoreKey = ""
	if w.m.applicationState.Active != surface.Key {
		if err := w.Dispatch(Action{Kind: SelectApplication, ApplicationKey: surface.Key}); err != nil {
			return
		}
	}
	_ = w.Dispatch(Action{Kind: ToggleApplicationReading})
}

func (w *Workspace) finishApplicationReadClick(p pointerCapture) {
	if !p.readClick || p.dragged {
		w.resetApplicationReadClick()
		return
	}
	surface, live := w.applicationReadSurface(p.applicationID, p.readKey)
	if !live {
		w.resetApplicationReadClick()
		return
	}
	if !p.readDouble {
		// Zero is used by untimed synthetic input. It must not leave a click
		// armed forever; a real second timestamp of zero can still match a
		// nonzero first timestamp across the uint32 wrap.
		if p.readTime == 0 {
			w.resetApplicationReadClick()
			return
		}
		w.applicationReadClick = applicationReadClick{
			key: surface.Key, id: surface.ID, time: p.readTime,
			x: p.readX, y: p.readY, prepared: true,
		}
		return
	}
	w.resetApplicationReadClick()
	w.toggleApplicationReadingFor(surface)
}

func (w *Workspace) beginApplicationReadClick(surface experience.ApplicationSurface, event experience.Event, double bool) {
	if w.m.applicationState.Active != surface.Key {
		if err := w.Dispatch(Action{Kind: SelectApplication, ApplicationKey: surface.Key}); err != nil {
			return
		}
	}
	w.stopWindowThrowForKey(surface.Key)
	w.clearApplicationFocus()
	w.applicationRestoreKey = ""
	w.pointer = pointerCapture{
		kind: captureApplicationReadClick, start: w.Document(), surface: w.applicationNodes[surface.ID],
		pressX: event.X, pressY: event.Y, lastX: event.X, lastY: event.Y, scale: w.scale, dragViewport: w.viewport,
		applicationID: surface.ID, readClick: true, readDouble: double, readKey: surface.Key,
		readTime: event.Time, readX: event.X, readY: event.Y,
	}
	w.ownWindowDragButton(applicationButton(event))
}

// A passive surface could already be dragged directly from Read or Overview.
// Crossing the normal drag threshold promotes the reserved click back into
// that established gesture, so adding double-click does not take movement away.
func (w *Workspace) promoteApplicationReadClickToDrag(event experience.Event) bool {
	p := w.pointer
	surface, ok := w.applicationReadSurface(p.applicationID, p.readKey)
	if !ok || !surface.DragContent {
		return false
	}
	w.resetApplicationReadClick()
	w.pointer = pointerCapture{}
	press := experience.Event{
		Kind: experience.PointerDown, Button: experience.ButtonPrimary, ButtonCode: 272,
		X: p.pressX, Y: p.pressY, Time: p.readTime, Modifiers: experience.ModSuper,
	}
	w.startWindowDrag(surface, press)
	if w.pointer.kind != captureApplicationPlacement || !w.prepareContentWindowDrag(event) {
		return true
	}
	w.moveApplicationPlacement(event.X, event.Y)
	w.sampleWindowDrag(event, false)
	return true
}

// Super+primary is a workspace-owned chord. A click pair toggles Read; motion
// keeps the existing window drag. Both presses are intercepted before client,
// Overview and Place routing, and scene picking preserves perspective/occlusion.
func (w *Workspace) handleApplicationReadGesture(event experience.Event) bool {
	p := &w.pointer
	if p.kind == captureApplicationReadClick {
		if (event.Kind == experience.PointerMove || event.Kind == experience.PointerUp) && w.viewport != p.dragViewport {
			w.resetApplicationReadClick()
			w.windowDragButtons = nil
			w.cancelPointer()
			return true
		}
		button := applicationButton(event)
		switch event.Kind {
		case experience.PointerCancel, experience.KeyboardCancel:
			w.resetApplicationReadClick()
			w.windowDragButtons = nil
			w.cancelPointer()
			return event.Kind == experience.PointerCancel
		case experience.PointerDown:
			p.dragged = true
			w.resetApplicationReadClick()
			w.ownWindowDragButton(button)
			return true
		case experience.PointerMove:
			dx, dy := event.X-p.pressX, event.Y-p.pressY
			p.lastX, p.lastY = event.X, event.Y
			threshold := float32(4) * p.scale
			if threshold <= 0 {
				threshold = 4
			}
			if dx*dx+dy*dy >= threshold*threshold {
				p.dragged = true
				if w.promoteApplicationReadClickToDrag(event) {
					return true
				}
			}
			return true
		case experience.PointerScroll:
			p.dragged = true
			w.resetApplicationReadClick()
			return true
		case experience.PointerUp:
			w.releaseWindowDragButton(event)
			if button == 272 {
				completed := *p
				w.pointer = pointerCapture{}
				w.finishApplicationReadClick(completed)
			}
			return true
		case experience.KeyInput:
			if event.Pressed && !event.Repeat && event.Modifiers == 0 && (event.Key == experience.KeyEscape || event.Keycode == 1) {
				w.resetApplicationReadClick()
				w.cancelPointer()
				w.helpKeys[helpStroke(event)] = true
				return true
			}
		}
		return false
	}

	if event.Kind != experience.PointerDown {
		return false
	}
	if applicationButton(event) != 272 || event.Modifiers != experience.ModSuper ||
		w.pointer.kind != captureNone || len(w.applicationButtons) != 0 || len(w.windowDragButtons) != 0 {
		w.resetApplicationReadClick()
		return false
	}
	hit, ok := w.applicationHit(event.X, event.Y)
	if !ok {
		w.resetApplicationReadClick()
		return false
	}
	surface := w.applicationForNode(hit.Node)
	if surface.ID == 0 {
		w.resetApplicationReadClick()
		return false
	}
	double := w.takeApplicationReadDoubleClick(surface, event)
	view := w.m.applicationState
	if view.Reading || view.Overview {
		w.beginApplicationReadClick(surface, event, double)
		return true
	}

	w.stopWindowThrowForKey(surface.Key)
	w.ownWindowDragButton(272)
	w.cancelPointer()
	w.clearApplicationFocus()
	w.applicationRestoreKey = ""
	w.startWindowDrag(surface, event)
	if w.pointer.kind == captureApplicationPlacement {
		w.pointer.applicationID = surface.ID
		w.pointer.readClick, w.pointer.readDouble, w.pointer.readKey = true, double, surface.Key
		w.pointer.readTime, w.pointer.readX, w.pointer.readY = event.Time, event.X, event.Y
	}
	return true
}
