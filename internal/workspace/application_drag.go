package workspace

import "github.com/codemodify/worldr/internal/experience"

func (w *Workspace) handleApplicationDrag(event experience.Event) bool {
	source := w.applicationCapturedID
	router, ok := w.applications.(experience.ApplicationDragRouter)
	if !ok || source == 0 || !router.ApplicationDragActive(source) {
		return false
	}
	switch event.Kind {
	case experience.PointerCancel, experience.KeyboardCancel:
		router.ApplicationDrag(source, 0, event)
		w.applicationButtons = nil
		w.applicationCapturedID = 0
		w.applicationCaptureObject = 0
		w.applicationHoveredID = 0
		w.applicationHover = false
		return false // Let normal keyboard/capture cancellation also clear focus.
	case experience.PointerMove, experience.PointerUp:
		mapped, target, hit := w.mapApplication(event, false)
		targetID := uint64(0)
		if hit {
			targetID = target.ID
		}
		mapped.ButtonCode = applicationButton(event)
		accepted := router.ApplicationDrag(source, targetID, mapped)
		if !accepted {
			targetID = 0
		}
		w.applicationHoveredID, w.applicationHover = targetID, targetID != 0
		if event.Kind == experience.PointerUp {
			delete(w.applicationButtons, mapped.ButtonCode)
			if len(w.applicationButtons) == 0 {
				w.applicationCapturedID = 0
				w.applicationCaptureObject = 0
			}
		}
		return true
	case experience.PointerDown, experience.PointerScroll:
		return true
	}
	return false
}
