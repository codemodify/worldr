package workspace

import "github.com/codemodify/worldr/internal/experience"

// Application input is routed first. Unmodified wheel input over the remaining
// scene changes the camera distance without moving any object or window.
func (w *Workspace) handleCameraScroll(event experience.Event) bool {
	if event.Kind != experience.PointerScroll || event.Modifiers != 0 || event.ScrollY == 0 || !finite(float64(event.ScrollY)) || w.pointer.kind != captureNone {
		return false
	}
	view := w.m.applicationState
	if w.application.ID != 0 && (view.Reading || view.Overview || view.Placing) {
		return false
	}
	x, y := (event.X-w.ox)/w.scale, (event.Y-w.oy)/w.scale
	if !w.inViewport(x, y) {
		return false
	}
	if _, hit := w.surfaceHit(event.X, event.Y); hit {
		return false
	}
	return w.Dispatch(Action{Kind: ZoomCamera, DeltaZoom: -event.ScrollY * .008}) == nil
}
