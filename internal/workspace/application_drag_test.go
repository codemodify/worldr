package workspace

import (
	"github.com/codemodify/worldr/internal/experience"
	"testing"
)

type dataDraggingApps struct {
	*fakeApplications
	active         bool
	source, target uint64
	dragEvents     []experience.Event
}

func (a *dataDraggingApps) ApplicationDragActive(id uint64) bool { return a.active && id == a.source }
func (a *dataDraggingApps) ApplicationDrag(source, target uint64, e experience.Event) bool {
	a.target = target
	a.dragEvents = append(a.dragEvents, e)
	if e.Kind == experience.PointerUp || e.Kind == experience.PointerCancel || e.Kind == experience.KeyboardCancel {
		a.active = false
	}
	return target != 0
}
func TestClientDataDragRepicksDestinationWithoutTransferringKeyboard(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	drag := &dataDraggingApps{fakeApplications: apps, source: apps.surfaces[0].ID}
	w.SetApplications(drag)
	w.Draw(1440, 900)
	x, y := visibleApplication(t, w, apps.surfaces[0])
	pointer(w, experience.PointerDown, x, y)
	if w.applicationCapturedID != drag.source {
		t.Fatal("source did not obtain implicit capture")
	}
	drag.active = true
	x, y = visibleApplication(t, w, apps.surfaces[1])
	pointer(w, experience.PointerMove, x, y)
	if drag.target != apps.surfaces[1].ID || w.applicationFocusedID != drag.source || w.applicationCapturedID != drag.source {
		t.Fatal("data drag did not repick or changed ownership")
	}
	pointer(w, experience.PointerUp, x, y)
	if drag.active || w.applicationCapturedID != 0 || drag.dragEvents[len(drag.dragEvents)-1].Kind != experience.PointerUp {
		t.Fatal("drop did not release source gesture")
	}
	if w.applicationFocusedID != drag.source {
		t.Fatal("drop stole keyboard focus")
	}
}
func TestClientDataDragLeavesAndCancelsWithoutSyntheticDrop(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	drag := &dataDraggingApps{fakeApplications: apps, source: apps.surfaces[0].ID}
	w.SetApplications(drag)
	x, y := visibleApplication(t, w, apps.surfaces[0])
	pointer(w, experience.PointerDown, x, y)
	drag.active = true
	pointer(w, experience.PointerMove, 3, 3)
	if drag.target != 0 || !drag.active {
		t.Fatal("leaving a target canceled source or kept stale destination")
	}
	w.Handle(experience.Event{Kind: experience.KeyboardCancel})
	if drag.active || w.applicationCapturedID != 0 || w.OwnsKeyboard() {
		t.Fatal("focus loss failed to cancel client data drag")
	}
	for _, event := range drag.dragEvents {
		if event.Kind == experience.PointerUp {
			t.Fatal("cancellation generated drop")
		}
	}
}
