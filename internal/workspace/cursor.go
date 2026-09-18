package workspace

import "github.com/codemodify/worldr/internal/experience"

// Cursor follows the actual pointer route, independently of keyboard focus.
// A captured client retains its cursor outside the surface until the final
// button release; overview and placement use the workspace cursor.
func (w *Workspace) Cursor() (experience.ApplicationCursor, bool) {
	view := w.m.applicationState
	if w.applications == nil || w.helpOpen || w.portals.open || view.Overview || view.Placing || w.pointer.kind != captureNone {
		return experience.ApplicationCursor{}, false
	}
	id := w.applicationHoveredID
	if len(w.applicationButtons) > 0 {
		id = w.applicationCapturedID
	}
	if id == 0 || w.applicationNodes[id] == 0 {
		return experience.ApplicationCursor{}, false
	}
	if provider, ok := w.applications.(experience.ApplicationCursorProvider); ok {
		return provider.ApplicationCursor(id)
	}
	return experience.ApplicationCursor{}, false
}
