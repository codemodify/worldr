package workspace

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

func superC(pressed, repeat bool, modifiers experience.Modifiers) experience.Event {
	return experience.Event{
		Kind: experience.KeyInput, Key: experience.KeyC, Keycode: 46,
		Pressed: pressed, Repeat: repeat, Modifiers: modifiers,
	}
}

func TestSuperCCancelsActiveClientInputBeforeCloseAndDrainsStroke(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	closer := &closingApplications{fakeApplications: apps}
	w.SetApplications(closer)
	w.Draw(1440, 900)
	target := apps.surfaces[0]
	x, y := visibleApplication(t, w, target)
	pointer(w, experience.PointerDown, x, y)
	if w.applicationFocusedID != target.ID || w.applicationCapturedID != target.ID || !w.OwnsKeyboard() {
		t.Fatal("fixture did not give the active client keyboard and pointer capture")
	}

	before, history, events := w.Document(), w.historyPosition, len(apps.events)
	closer.check = func(id uint64) {
		if id != target.ID || w.applicationFocusedID != 0 || w.applicationCapturedID != 0 ||
			w.applicationHoveredID != 0 || len(w.applicationButtons) != 0 || w.OwnsKeyboard() || w.pointer.kind != captureNone {
			t.Fatalf("close reached provider before active input was cancelled: id=%d", id)
		}
		if len(apps.focus) == 0 || apps.focus[len(apps.focus)-1] != 0 {
			t.Fatal("close reached provider before keyboard focus was revoked")
		}
	}
	if !w.Handle(superC(true, false, experience.ModSuper)) {
		t.Fatal("Super+C press was not consumed")
	}
	if len(closer.closed) != 1 || closer.closed[0] != target.ID {
		t.Fatalf("Super+C did not request closing the active app: %v", closer.closed)
	}
	if w.Document() != before || w.historyPosition != history || len(w.applicationSurfaces) != 1 || w.scene.Node(w.applicationNodes[target.ID]) == nil {
		t.Fatal("close request changed document history or withdrew the provider-owned surface")
	}
	keyboardCancel, pointerCancel := 0, 0
	for _, sent := range apps.events[events:] {
		if sent.id != target.ID {
			t.Fatalf("input cancellation reached another client: %+v", sent)
		}
		switch sent.event.Kind {
		case experience.KeyboardCancel:
			keyboardCancel++
		case experience.PointerCancel:
			pointerCancel++
		default:
			t.Fatalf("reserved shortcut leaked into the client: %+v", sent)
		}
	}
	if keyboardCancel != 1 || pointerCancel != 1 {
		t.Fatalf("held client input was not cancelled exactly once: keyboard=%d pointer=%d", keyboardCancel, pointerCancel)
	}
	if !w.Handle(superC(true, true, 0)) || !w.Handle(superC(true, false, 0)) || len(closer.closed) != 1 {
		t.Fatal("repeat or duplicate down issued another close request")
	}
	if !w.Handle(superC(false, false, 0)) || len(apps.events) != events+2 {
		t.Fatal("C release after modifier change escaped the reserved stroke")
	}

	apps.surfaces = nil
	w.Update(0)
	if len(w.applicationSurfaces) != 0 || w.scene.Node(w.applicationNodes[target.ID]) != nil {
		t.Fatal("provider withdrawal did not retire the closed app")
	}
}

func TestSuperCUsesActiveWindowAndPreservesSiblingHover(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	closer := &closingApplications{fakeApplications: apps}
	w.SetApplications(closer)
	w.Draw(1440, 900)
	active, sibling := apps.surfaces[0], apps.surfaces[1]
	ax, ay := visibleApplication(t, w, active)
	pointer(w, experience.PointerDown, ax, ay)
	pointer(w, experience.PointerUp, ax, ay)
	sx, sy := visibleApplication(t, w, sibling)
	pointer(w, experience.PointerMove, sx, sy)
	if w.application.ID != active.ID || w.applicationFocusedID != active.ID || w.applicationHoveredID != sibling.ID {
		t.Fatal("fixture did not separate active/focused app from pointer hover")
	}

	if !w.Handle(superC(true, false, experience.ModSuper)) || len(closer.closed) != 1 || closer.closed[0] != active.ID {
		t.Fatalf("Super+C did not close the active app: %v", closer.closed)
	}
	if w.applicationFocusedID != 0 || w.OwnsKeyboard() || w.applicationHoveredID != sibling.ID || !w.applicationHover {
		t.Fatal("closing the active app cleared another window's pointer hover")
	}
	for _, sent := range apps.events {
		if sent.event.Kind == experience.KeyboardCancel && sent.id != active.ID {
			t.Fatal("keyboard cancellation reached the wrong app")
		}
	}
}

func TestSuperCIsEdgeTriggeredAcrossModifiersRepeatsAndFocusLoss(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	closer := &closingApplications{fakeApplications: apps}
	w.SetApplications(closer)
	x, y := visibleApplication(t, w, apps.surfaces[0])
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)

	ordinary := []experience.Event{
		superC(true, false, 0),
		superC(true, true, experience.ModSuper),
		superC(true, false, experience.ModSuper),
		superC(false, false, experience.ModSuper),
		superC(true, false, experience.ModSuper|experience.ModShift),
		superC(false, false, 0),
	}
	for _, event := range ordinary {
		before := len(apps.events)
		if !w.Handle(event) || len(apps.events) != before+1 || apps.events[before].event != event {
			t.Fatalf("ordinary C stroke was intercepted after modifier/order change: %+v", event)
		}
	}
	if len(closer.closed) != 0 {
		t.Fatal("an ordinary or extra-modifier C stroke closed the app")
	}
	for _, event := range []experience.Event{
		superC(true, true, experience.ModSuper),
		superC(false, false, 0),
	} {
		before := len(apps.events)
		if !w.Handle(event) || len(apps.events) != before+1 || apps.events[before].event != event {
			t.Fatalf("orphan repeat acquired workspace ownership: %+v", event)
		}
	}
	if len(closer.closed) != 0 {
		t.Fatal("orphan Super+C repeat issued a close request")
	}

	if !w.Handle(superC(true, false, experience.ModSuper)) || len(closer.closed) != 1 {
		t.Fatal("fresh exact Super+C did not close once")
	}
	w.Handle(experience.Event{Kind: experience.KeyboardCancel})
	if w.applicationCloseShortcut.active {
		t.Fatal("keyboard focus loss left the close shortcut held")
	}
	if !w.Handle(superC(true, false, experience.ModSuper)) || len(closer.closed) != 2 {
		t.Fatal("fresh Super+C after focus loss remained suppressed")
	}
}

func TestSuperCStrokeDrainsAcrossHelpModal(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	closer := &closingApplications{fakeApplications: apps}
	w.SetApplications(closer)
	if !w.Handle(superC(true, false, experience.ModSuper)) || len(closer.closed) != 1 {
		t.Fatal("fresh Super+C did not begin an owned stroke")
	}
	f1 := experience.Event{Kind: experience.KeyInput, Key: experience.KeyF1, Keycode: 59, Pressed: true}
	if !w.Handle(f1) || !w.helpOpen {
		t.Fatal("help did not open between close press and release")
	}
	f1.Pressed = false
	w.Handle(f1)
	if !w.Handle(superC(true, true, 0)) || !w.Handle(superC(false, false, 0)) || len(closer.closed) != 1 {
		t.Fatal("help captured or replayed the tail of an owned Super+C stroke")
	}
	if w.applicationCloseShortcut.active || w.helpKeys[helpKey{code: 46}] {
		t.Fatal("modal transition left C held in a shortcut tracker")
	}
	dismissHelpByKeyboard(t, w)
	if !w.Handle(superC(true, false, experience.ModSuper)) || len(closer.closed) != 2 {
		t.Fatal("fresh Super+C remained suppressed after closing help")
	}
}

func TestSuperCConsumesUnsupportedAndTargetlessStrokesAsNoOps(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	x, y := visibleApplication(t, w, apps.surfaces[0])
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	before, events := w.Document(), len(apps.events)
	for _, event := range []experience.Event{
		superC(true, false, experience.ModSuper),
		superC(true, true, 0),
		superC(false, false, 0),
	} {
		if !w.Handle(event) {
			t.Fatal("unsupported Super+C stroke was not reserved")
		}
	}
	if w.Document() != before || len(apps.events) != events || !w.OwnsKeyboard() {
		t.Fatal("unsupported close changed state, leaked to the client, or stole focus")
	}

	empty, err := NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Close()
	for _, event := range []experience.Event{superC(true, false, experience.ModSuper), superC(false, false, 0)} {
		if !empty.Handle(event) {
			t.Fatal("targetless Super+C stroke was not reserved")
		}
	}
}

func TestSuperCWorksInSpaceReadPlaceAndOverview(t *testing.T) {
	for _, mode := range []string{"space", "read", "place", "overview"} {
		t.Run(mode, func(t *testing.T) {
			w, apps := multipleApplications(t, 1)
			closer := &closingApplications{fakeApplications: apps}
			w.SetApplications(closer)
			switch mode {
			case "read":
				command(t, w, Action{Kind: ToggleApplicationReading})
			case "place":
				command(t, w, Action{Kind: ToggleApplicationPlacement})
			case "overview":
				command(t, w, Action{Kind: ToggleApplicationOverview})
			}
			before := w.Document()
			if !w.Handle(superC(true, false, experience.ModSuper)) || len(closer.closed) != 1 || closer.closed[0] != apps.surfaces[0].ID {
				t.Fatalf("Super+C failed in %s", mode)
			}
			if w.Document() != before {
				t.Fatalf("close request changed the %s workspace view", mode)
			}
		})
	}
}

func TestSuperCStopsOnlyTheActiveWindowThrow(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 2)
	closer := &closingApplications{fakeApplications: apps}
	w.SetApplications(closer)
	first, active := apps.surfaces[0], apps.surfaces[1]
	startWindowThrow(t, w, first)
	startWindowThrow(t, w, active)
	w.Update(40 * time.Millisecond)
	if !throwingSurface(w, first.ID) || !throwingSurface(w, active.ID) || w.application.ID != active.ID {
		t.Fatal("fixture did not prepare two throws with the second app active")
	}
	firstBefore := w.Document().View.Application.Layouts[0]
	activeBefore := w.Document().View.Application.Layouts[1]
	if !w.Handle(superC(true, false, experience.ModSuper)) || len(closer.closed) != 1 || closer.closed[0] != active.ID {
		t.Fatal("Super+C did not close the active moving app")
	}
	if !throwingSurface(w, first.ID) || throwingSurface(w, active.ID) {
		t.Fatal("Super+C stopped an unrelated throw or retained the target throw")
	}
	w.Update(100 * time.Millisecond)
	after := w.Document().View.Application.Layouts
	if after[0] == firstBefore || after[1] != activeBefore {
		t.Fatal("unrelated motion did not continue independently after close")
	}
}
