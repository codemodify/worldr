package workspace

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

func throwingSurface(w *Workspace, id uint64) bool {
	for motion := w.windowThrow; motion != nil; motion = motion.next {
		for _, moving := range motion.liveIDs {
			if moving == id {
				return true
			}
		}
	}
	return false
}

func TestWindowThrowContinuesWhileAnotherTerminalReceivesKeysAndEscape(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 2)
	a, b := apps.surfaces[0], apps.surfaces[1]
	startWindowThrow(t, w, a)
	w.Update(40 * time.Millisecond)
	x, y := visibleApplication(t, w, b)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if !throwingSurface(w, a.ID) || w.applicationFocusedID != b.ID || !w.OwnsKeyboard() {
		t.Fatal("clicking another terminal stopped the moving window or failed to focus the clicked app")
	}
	before := w.Document().View.Application.Layouts[0]
	count := len(apps.events)
	for _, event := range []experience.Event{
		{Kind: experience.KeyInput, Key: experience.KeyE, Keycode: 18, Pressed: true},
		{Kind: experience.KeyInput, Key: experience.KeyE, Keycode: 18},
		{Kind: experience.KeyInput, Key: experience.KeyEscape, Keycode: 1, Pressed: true},
		{Kind: experience.KeyInput, Key: experience.KeyEscape, Keycode: 1},
	} {
		if !w.Handle(event) || len(apps.events) != count+1 || apps.events[count].id != b.ID || apps.events[count].event != event {
			t.Fatal("an unrelated moving window intercepted focused terminal input")
		}
		count++
	}
	w.Update(100 * time.Millisecond)
	if !throwingSurface(w, a.ID) || w.Document().View.Application.Layouts[0] == before || w.applicationFocusedID != b.ID {
		t.Fatal("typing or terminal Escape stopped the independent throw or lost focus")
	}
}

func TestWindowThrowMovingContentAndGripClicksStopOnlyThatWindow(t *testing.T) {
	for _, target := range []string{"content", "grip"} {
		t.Run(target, func(t *testing.T) {
			w, apps := windowThrowWorkspace(t, 2)
			a, b := apps.surfaces[0], apps.surfaces[1]
			startWindowThrow(t, w, a)
			startWindowThrow(t, w, b)
			w.Update(60 * time.Millisecond)
			if !throwingSurface(w, a.ID) || !throwingSurface(w, b.ID) {
				t.Fatal("fixture failed to keep both independent throws active")
			}
			before := w.Document().View.Application.Layouts
			var x, y float32
			if target == "grip" {
				x, y = windowGripPoint(t, w, a)
			} else {
				x, y = visibleApplication(t, w, a)
			}
			w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y, Time: 2000})
			w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x, Y: y, Time: 2010})
			w.Update(100 * time.Millisecond)
			after := w.Document().View.Application.Layouts
			if throwingSurface(w, a.ID) || !throwingSurface(w, b.ID) || after[0] != before[0] || after[1] == before[1] {
				t.Fatal("clicking the moving target stopped an unrelated throw or failed to stop its own")
			}
			if target == "content" && w.applicationFocusedID != a.ID {
				t.Fatal("stopping a window by clicking content prevented normal application focus")
			}
		})
	}
}

func TestWindowThrowContinuesThroughCapturedSecondaryClickOverMovingWindow(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 2)
	a, b := apps.surfaces[0], apps.surfaces[1]
	startWindowThrow(t, w, a)
	w.Update(40 * time.Millisecond)
	bx, by := visibleApplication(t, w, b)
	pointer(w, experience.PointerDown, bx, by)
	ax, ay := visibleApplication(t, w, a)
	before := w.Document().View.Application.Layouts[0]
	w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonSecondary, X: ax, Y: ay})
	if !throwingSurface(w, a.ID) || w.applicationCapturedID != b.ID || len(w.applicationButtons) != 2 {
		t.Fatal("secondary button over a moving window ignored the other client's existing capture")
	}
	last := apps.events[len(apps.events)-1]
	if last.id != b.ID || last.event.Kind != experience.PointerDown || last.event.ButtonCode != 273 {
		t.Fatal("captured secondary button was sent to the visually underlying moving window")
	}
	w.Update(60 * time.Millisecond)
	if w.Document().View.Application.Layouts[0] == before || !throwingSurface(w, a.ID) {
		t.Fatal("captured pointer activity interrupted the independent coast")
	}
	w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonSecondary, X: ax, Y: ay})
	pointer(w, experience.PointerUp, bx, by)
	if len(w.applicationButtons) != 0 || w.applicationFocusedID != b.ID {
		t.Fatal("captured multibutton release lost ownership or left a stuck button")
	}
}

func TestWindowThrowAnotherHeldDragCanCancelWithoutRewindingTheCoast(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 2)
	a, b := apps.surfaces[0], apps.surfaces[1]
	startWindowThrow(t, w, a)
	w.Update(40 * time.Millisecond)
	before := w.Document().View.Application.Layouts
	x, y := windowGripPoint(t, w, b)
	w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y, Time: 2000})
	w.Handle(experience.Event{Kind: experience.PointerMove, X: x + 40, Y: y + 20, Time: 2040})
	w.Update(120 * time.Millisecond)
	advanced := w.Document().View.Application.Layouts[0]
	if !throwingSurface(w, a.ID) || advanced == before[0] || !w.pointer.windowDrag {
		t.Fatal("holding a separate drag paused the first window")
	}
	w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyEscape, Keycode: 1, Pressed: true})
	if w.pointer.kind != captureNone || !throwingSurface(w, a.ID) || w.Document().View.Application.Layouts[0] != advanced || w.Document().View.Application.Layouts[1] != before[1] {
		t.Fatal("Escape did not cancel only the held drag while preserving the independent throw")
	}
	pointer(w, experience.PointerUp, x+40, y+20)
	w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyEscape, Keycode: 1})
	w.Update(100 * time.Millisecond)
	if w.Document().View.Application.Layouts[0] == advanced || !throwingSurface(w, a.ID) {
		t.Fatal("cancelled drag's remaining input stopped the independent throw")
	}
}

func TestWindowThrowTwoWindowsKeepIndependentMotionAndUndo(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 2)
	a, b := apps.surfaces[0], apps.surfaces[1]
	before := w.Document().View.Application.Layouts
	startWindowThrow(t, w, a)
	w.Update(100 * time.Millisecond)
	x, y := windowGripPoint(t, w, b)
	w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y, Time: 2000})
	w.Handle(experience.Event{Kind: experience.PointerMove, X: x + 15, Y: y + 5, Time: 2020})
	w.Update(50 * time.Millisecond)
	w.Handle(experience.Event{Kind: experience.PointerMove, X: x + 35, Y: y + 12, Time: 2040})
	w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x + 45, Y: y + 15, Time: 2050})
	if !throwingSurface(w, a.ID) || !throwingSurface(w, b.ID) {
		t.Fatal("throwing a second window stopped or replaced the first motion")
	}
	during := w.Document().View.Application.Layouts
	w.Update(100 * time.Millisecond)
	if w.Document().View.Application.Layouts[0] == during[0] || w.Document().View.Application.Layouts[1] == during[1] {
		t.Fatal("independent throws did not both continue advancing")
	}
	for i := 0; w.windowThrow != nil && i < 40; i++ {
		w.Update(100 * time.Millisecond)
	}
	if w.windowThrow != nil {
		t.Fatal("independent throws did not settle naturally")
	}
	after := w.Document().View.Application.Layouts
	command(t, w, Action{Kind: Undo})
	oneUndo := w.Document().View.Application.Layouts
	if oneUndo[0] != after[0] || oneUndo[1] != before[1] {
		t.Fatal("first undo did not restore the later throw while preserving the earlier window's final position")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document().View.Application.Layouts != before {
		t.Fatal("two undo operations failed to restore both independent starting positions")
	}
	command(t, w, Action{Kind: Redo})
	command(t, w, Action{Kind: Redo})
	w.Update(time.Second)
	if w.Document().View.Application.Layouts != after || w.windowThrow != nil {
		t.Fatal("redo lost independent resting positions or replayed velocity")
	}
}

func TestWindowThrowLaterSelectionUndoDoesNotRewindWindowMotion(t *testing.T) {
	for _, finish := range []string{"natural", "undo during coast"} {
		t.Run(finish, func(t *testing.T) {
			w, apps := windowThrowWorkspace(t, 2)
			a, b := apps.surfaces[0], apps.surfaces[1]
			before := w.Document()
			startWindowThrow(t, w, a)
			w.Update(50 * time.Millisecond)
			x, y := visibleApplication(t, w, b)
			pointer(w, experience.PointerDown, x, y)
			pointer(w, experience.PointerUp, x, y)
			if !throwingSurface(w, a.ID) {
				t.Fatal("selecting another window stopped the throw before the history regression")
			}
			w.Update(100 * time.Millisecond)
			if finish == "natural" {
				w.Update(10 * time.Second)
			}
			after := w.Document()
			command(t, w, Action{Kind: Undo})
			if w.Document().View.Application.Layouts != after.View.Application.Layouts || w.Document().View.Application.Active != before.View.Application.Active || w.windowThrow != nil {
				t.Fatal("undoing a later selection rewound window motion or left a throw active")
			}
			command(t, w, Action{Kind: Undo})
			if w.Document() != before {
				t.Fatal("the next undo failed to restore only the earlier drag/throw")
			}
			command(t, w, Action{Kind: Redo})
			command(t, w, Action{Kind: Redo})
			w.Update(time.Second)
			if w.Document() != after || w.windowThrow != nil {
				t.Fatal("redo mixed selection and placement or replayed the coast")
			}
		})
	}
}

func TestWindowThrowConcurrentHistoryIsIndependentOfUpdateGranularity(t *testing.T) {
	fast, fastApps := windowThrowWorkspace(t, 2)
	slow, slowApps := windowThrowWorkspace(t, 2)
	for _, fixture := range []struct {
		workspace *Workspace
		apps      *fakeApplications
	}{{fast, fastApps}, {slow, slowApps}} {
		startWindowThrow(t, fixture.workspace, fixture.apps.surfaces[0])
		fixture.workspace.Update(100 * time.Millisecond)
		startWindowThrow(t, fixture.workspace, fixture.apps.surfaces[1])
	}
	for i := 0; i < 150; i++ {
		fast.Update(20 * time.Millisecond)
	}
	slow.Update(3 * time.Second)
	if fast.Document() != slow.Document() || fast.windowThrow != nil || slow.windowThrow != nil {
		t.Fatal("different update granularity changed the resting placements")
	}
	final := fast.Document()
	for i := 0; i < 2; i++ {
		command(t, fast, Action{Kind: Undo})
		command(t, slow, Action{Kind: Undo})
		if fast.Document() != slow.Document() {
			t.Fatal("coasts finishing in one frame changed chronological undo behavior")
		}
	}
	for i := 0; i < 2; i++ {
		command(t, fast, Action{Kind: Redo})
		command(t, slow, Action{Kind: Redo})
		if fast.Document() != slow.Document() {
			t.Fatal("coasts finishing in one frame changed chronological redo behavior")
		}
	}
	if fast.Document() != final || fast.windowThrow != nil || slow.windowThrow != nil {
		t.Fatal("redo lost resting placement or restarted independent motion")
	}
}

func TestWindowThrowReturnToStartStillReservesUndoForItsCoast(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 1)
	before, history := w.Document(), w.historyPosition
	x, y := windowGripPoint(t, w, apps.surfaces[0])
	for _, event := range []experience.Event{
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y, Time: 1000},
		{Kind: experience.PointerMove, X: x + 40, Y: y, Time: 1020},
		{Kind: experience.PointerMove, X: x + 80, Y: y, Time: 1100},
		{Kind: experience.PointerMove, X: x, Y: y, Time: 1200},
	} {
		if !w.Handle(event) {
			t.Fatal("round-trip grip gesture was not consumed")
		}
	}
	// Ray mapping introduces small rounding differences on a round trip. Snap
	// just that error at the release boundary to exercise exactly zero net drag
	// with the real gesture's nonzero recent velocity.
	start, returned := before.View.Application.Layouts[0], w.Document().View.Application.Layouts[0]
	if abs(start.X-returned.X) > .00001 || abs(start.Y-returned.Y) > .00001 {
		t.Fatal("round-trip fixture failed to return to the starting point")
	}
	w.m.applicationState.Layouts[0] = start
	sample := &w.pointer.dragSamples[w.pointer.dragSampleCount-1]
	sample.x, sample.y = start.X, start.Y
	w.releaseWindowDrag()
	w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x, Y: y, Time: 1200})
	if w.windowThrow == nil {
		t.Fatal("returning to the starting point with release velocity failed to start a throw")
	}
	if w.Document() != before {
		t.Fatal("round-trip fixture did not return to exactly the pre-gesture placement")
	}
	w.Update(3 * time.Second)
	after := w.Document()
	if after.View.Application.Layouts == before.View.Application.Layouts || w.historyPosition != history+1 {
		t.Fatal("returning to the starting point lost the coast's single undo entry")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("undo failed to restore a throw whose held drag ended at its starting point")
	}
	command(t, w, Action{Kind: Redo})
	if w.Document() != after || w.windowThrow != nil {
		t.Fatal("redo lost the round-trip throw's resting placement or replayed motion")
	}
}
