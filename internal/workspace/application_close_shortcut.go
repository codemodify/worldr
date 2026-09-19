package workspace

import "github.com/codemodify/worldr/internal/experience"

// applicationCloseShortcut owns an entire physical C stroke after a reserved
// Super+C press. An ordinary C stroke is also remembered until release so a
// later modifier change or key repeat cannot turn it into a workspace command.
type applicationCloseShortcut struct {
	active bool
	owned  bool
	code   uint32
	key    experience.Key
}

func applicationCloseKey(event experience.Event) bool {
	return event.Key == experience.KeyC || event.Key == experience.KeyUnknown && event.Keycode == 46
}

func (s applicationCloseShortcut) matches(event experience.Event) bool {
	if !s.active {
		return false
	}
	if s.code != 0 {
		return event.Keycode == s.code
	}
	return event.Key == s.key
}

// drainApplicationCloseShortcut runs before modal and client routing. A stroke
// that began as Super+C stays owned even if Super is released first or a modal
// opens before C comes up. Ordinary C is only observed here and keeps routing.
func (w *Workspace) drainApplicationCloseShortcut(event experience.Event) bool {
	if event.Kind == experience.KeyboardCancel {
		stroke := w.applicationCloseShortcut
		if stroke.active {
			key := helpKey{key: stroke.key}
			if stroke.code != 0 {
				key = helpKey{code: stroke.code}
			}
			delete(w.helpKeys, key)
		}
		w.applicationCloseShortcut = applicationCloseShortcut{}
		return false
	}
	if event.Kind != experience.KeyInput || !w.applicationCloseShortcut.matches(event) {
		return false
	}
	owned := w.applicationCloseShortcut.owned
	if !event.Pressed {
		w.applicationCloseShortcut = applicationCloseShortcut{}
		// Help observes every initial key press even while closed. If it opened
		// during this reserved stroke, do not leave its copy stuck as held.
		delete(w.helpKeys, helpStroke(event))
	}
	return owned
}

// clearApplicationInputFor cancels only input streams owned by the requested
// surface. Pointer hover or keyboard focus belonging to another live window is
// preserved. Client data drags use their routing lifecycle for cancellation.
func (w *Workspace) clearApplicationInputFor(id uint64) {
	if id == 0 || w.applications == nil {
		return
	}
	focused := w.applicationFocusedID == id
	captured := w.applicationCapturedID == id
	hovered := w.applicationHoveredID == id

	if focused {
		w.applications.Send(id, experience.Event{Kind: experience.KeyboardCancel})
		w.applications.Focus(0)
		w.applicationFocusedID = 0
		w.applicationKeyboard = false
	}

	dragCancelled := false
	if captured {
		if router, ok := w.applications.(experience.ApplicationDragRouter); ok && router.ApplicationDragActive(id) {
			router.ApplicationDrag(id, 0, experience.Event{Kind: experience.PointerCancel})
			dragCancelled = true
		} else if !hovered {
			w.applications.Send(id, experience.Event{Kind: experience.PointerCancel})
		}
		w.applicationCapturedID = 0
		w.applicationButtons = nil
		w.applicationCaptureObject = 0
	}
	if hovered {
		if !dragCancelled {
			w.applications.Send(id, experience.Event{Kind: experience.PointerCancel})
		}
		w.applicationHoveredID = 0
		w.applicationHover = false
	}
}

func (w *Workspace) requestApplicationClose(surface experience.ApplicationSurface, closer experience.ApplicationCloser) {
	// A workspace gesture and a client grab must be settled before the provider
	// can synchronously withdraw the surface. The provider remains authoritative:
	// the workspace keeps drawing the app until it disappears from Surfaces.
	w.resetApplicationReadClick()
	w.cancelPointer()
	w.windowDragButtons = nil
	w.stopWindowThrowForKey(surface.Key)
	w.clearApplicationInputFor(surface.ID)
	closer.CloseApplication(surface.ID)
}

// Super+C is reserved by the workspace even while a client owns the keyboard.
// The current active live surface is snapshotted on the fresh press. Repeats,
// duplicate downs and a release after Super changes are consumed without
// issuing another close request. With no target or no closer, it is a no-op.
func (w *Workspace) handleApplicationCloseShortcut(event experience.Event) bool {
	if event.Kind == experience.KeyboardCancel {
		w.applicationCloseShortcut = applicationCloseShortcut{}
		return false
	}
	if event.Kind != experience.KeyInput {
		return false
	}
	// The early drain deliberately lets client-owned C strokes continue. A
	// duplicate down after Super changes still belongs to that same stroke.
	if w.applicationCloseShortcut.matches(event) {
		return false
	}
	// Ownership can begin only on a fresh physical edge. An orphan repeat may
	// still belong to a client that saw the original press before focus changed.
	if !event.Pressed || event.Repeat || !applicationCloseKey(event) {
		return false
	}

	owned := event.Modifiers == experience.ModSuper
	w.applicationCloseShortcut = applicationCloseShortcut{
		active: true,
		owned:  owned,
		code:   event.Keycode,
		key:    event.Key,
	}
	if !owned {
		return false
	}

	surface := w.application
	closer, supported := w.applications.(experience.ApplicationCloser)
	if surface.ID == 0 || !supported {
		w.resetApplicationReadClick()
		return true
	}
	w.requestApplicationClose(surface, closer)
	return true
}
