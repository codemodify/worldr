package workspace

import (
	"bytes"
	"math"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

func windowThrowWorkspace(t *testing.T, count int) (*Workspace, *fakeApplications) {
	t.Helper()
	w, apps := windowDragWorkspace(t, count)
	command(t, w, Action{Kind: SetReducedMotion, Enabled: false})
	return w, apps
}

func timedWindowGesture(t *testing.T, w *Workspace, surface experience.ApplicationSurface, times [4]uint32) {
	t.Helper()
	x, y := windowGripPoint(t, w, surface)
	timedWindowGestureAt(t, w, x, y, times)
}

func timedWindowGestureAt(t *testing.T, w *Workspace, x, y float32, times [4]uint32) {
	t.Helper()
	for i, kind := range []experience.EventKind{experience.PointerDown, experience.PointerMove, experience.PointerMove, experience.PointerUp} {
		delta := []float32{0, 15, 35, 45}[i]
		if !w.Handle(experience.Event{Kind: kind, Button: experience.ButtonPrimary, X: x + delta, Y: y + delta/3, Time: times[i]}) {
			t.Fatalf("timed grip gesture event %d was not consumed", i)
		}
	}
}

func startWindowThrow(t *testing.T, w *Workspace, surface experience.ApplicationSurface) {
	t.Helper()
	timedWindowGesture(t, w, surface, [4]uint32{1000, 1020, 1040, 1050})
	if w.windowThrow == nil || w.pointer.kind != captureNone {
		t.Fatal("releasing a moving grip did not start a throw")
	}
}

func TestWindowThrowDeceleratesAndStopsWithoutFurtherInput(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 1)
	before, history := w.Document(), w.historyPosition
	startWindowThrow(t, w, apps.surfaces[0])
	if !w.CanUndo() || w.CanRedo() || w.historyPosition != history+1 {
		t.Fatal("active throw did not reserve one undo entry in gesture order")
	}
	previous := w.Document().View.Application.Layouts[0]
	lastDistance := math.Inf(1)
	steps := 0
	for ; w.windowThrow != nil && steps < 40; steps++ {
		w.Update(100 * time.Millisecond)
		current := w.Document().View.Application.Layouts[0]
		distance := math.Hypot(float64(current.X-previous.X), float64(current.Y-previous.Y))
		if distance <= 0 || distance >= lastDistance || current.X <= previous.X {
			t.Fatalf("throw failed to decelerate in its release direction: step %d distance %g, prior %g", steps, distance, lastDistance)
		}
		previous, lastDistance = current, distance
	}
	if w.windowThrow != nil || steps < 3 || w.historyPosition != history+1 {
		t.Fatal("throw did not reach a finite stop and commit one move")
	}
	after := w.Document()
	w.Update(10 * time.Second)
	if w.Document() != after || after.View.Camera != before.View.Camera || len(apps.events) != 0 {
		t.Fatal("finished throw resumed, moved the camera, or sent client input")
	}
}

func TestWindowThrowPlacementIsIndependentOfFrameRate(t *testing.T) {
	fast, fastApps := windowThrowWorkspace(t, 1)
	slow, slowApps := windowThrowWorkspace(t, 1)
	startWindowThrow(t, fast, fastApps.surfaces[0])
	startWindowThrow(t, slow, slowApps.surfaces[0])
	for i := 0; i < 20; i++ {
		fast.Update(20 * time.Millisecond)
	}
	for i := 0; i < 5; i++ {
		slow.Update(80 * time.Millisecond)
	}
	if fast.Document() != slow.Document() {
		t.Fatal("equal elapsed time produced different placements at different frame rates")
	}
	for i := 0; fast.windowThrow != nil && i < 200; i++ {
		fast.Update(20 * time.Millisecond)
	}
	slow.Update(time.Minute)
	if fast.Document() != slow.Document() || fast.windowThrow != nil || slow.windowThrow != nil {
		t.Fatal("a stalled frame changed the final resting position")
	}
}

func TestWindowThrowGroupRemainsOneUndoableMove(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 2)
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[1].Key, Additive: true})
	command(t, w, Action{Kind: GroupApplications})
	before, history := w.Document(), w.historyPosition
	startWindowThrow(t, w, apps.surfaces[1])
	w.Update(200 * time.Millisecond)
	for _, duration := range []time.Duration{0, time.Second, 10 * time.Second} {
		w.Update(duration)
		current := w.Document().View.Application.Layouts
		initial := before.View.Application.Layouts
		if abs((current[0].X-current[1].X)-(initial[0].X-initial[1].X)) > .00001 || abs((current[0].Y-current[1].Y)-(initial[0].Y-initial[1].Y)) > .00001 || current[0].Depth != initial[0].Depth || current[1].Depth != initial[1].Depth {
			t.Fatal("throw distorted the group's arrangement or depth")
		}
	}
	after := w.Document()
	if w.windowThrow != nil || w.historyPosition != history+1 {
		t.Fatal("group throw recorded more than one edit")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("one undo failed to restore the whole group's drag and throw")
	}
	command(t, w, Action{Kind: Redo})
	w.Update(time.Second)
	if w.Document() != after || w.windowThrow != nil {
		t.Fatal("redo failed to restore resting placement or replayed motion")
	}
}

func TestWindowThrowRequiresRecentTimedMovement(t *testing.T) {
	for _, test := range []struct {
		name  string
		times [4]uint32
		throw bool
	}{
		{"untimed", [4]uint32{}, false},
		{"same timestamp", [4]uint32{1000, 1000, 1000, 1000}, false},
		{"pause before release", [4]uint32{1000, 1020, 1040, 1400}, false},
		{"backwards timestamp", [4]uint32{1000, 1020, 1040, 900}, false},
		{"unsigned wrap", [4]uint32{math.MaxUint32 - 30, math.MaxUint32 - 10, 9, 19}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			w, apps := windowThrowWorkspace(t, 1)
			before := w.Document()
			timedWindowGesture(t, w, apps.surfaces[0], test.times)
			if (w.windowThrow != nil) != test.throw {
				t.Fatal("timestamp history gave the wrong release behavior")
			}
			if w.Document().View.Application.Layouts == before.View.Application.Layouts {
				t.Fatal("timestamp policy prevented ordinary placement")
			}
			if !test.throw {
				after := w.Document()
				w.Update(time.Second)
				if w.Document() != after {
					t.Fatal("untimed or stale movement started delayed motion")
				}
			}
		})
	}
}

func TestWindowThrowReducedMotionKeepsPlacementAndSettlesMotion(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	before := w.Document()
	timedWindowGesture(t, w, apps.surfaces[0], [4]uint32{1000, 1020, 1040, 1050})
	if w.windowThrow != nil || w.Document().View.Application.Layouts == before.View.Application.Layouts {
		t.Fatal("Reduced Motion either threw a window or disabled dragging")
	}
	command(t, w, Action{Kind: SetReducedMotion, Enabled: false})
	startWindowThrow(t, w, apps.surfaces[0])
	w.Update(100 * time.Millisecond)
	position := w.Document().View.Application.Layouts
	command(t, w, Action{Kind: SetReducedMotion, Enabled: true})
	w.Update(time.Second)
	if w.windowThrow != nil || w.Document().View.Application.Layouts != position {
		t.Fatal("enabling Reduced Motion did not stop the current throw immediately")
	}
}

func TestWindowThrowInterruptionsSettleAtCurrentPosition(t *testing.T) {
	for _, reason := range []string{"escape", "grab", "pointer cancel", "keyboard cancel", "resize", "save", "withdraw", "overview"} {
		t.Run(reason, func(t *testing.T) {
			w, apps := windowThrowWorkspace(t, 1)
			before, history := w.Document(), w.historyPosition
			startWindowThrow(t, w, apps.surfaces[0])
			w.Update(100 * time.Millisecond)
			position := w.Document().View.Application.Layouts
			switch reason {
			case "escape":
				w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyEscape, Keycode: 1, Pressed: true})
			case "grab":
				x, y := windowGripPoint(t, w, apps.surfaces[0])
				w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y, Time: 2000})
				if !w.pointer.windowDrag {
					t.Fatal("a moving window could not be grabbed again")
				}
				w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x, Y: y, Time: 2010})
			case "pointer cancel":
				w.Handle(experience.Event{Kind: experience.PointerCancel})
			case "keyboard cancel":
				w.Handle(experience.Event{Kind: experience.KeyboardCancel})
			case "resize":
				w.Draw(1200, 800)
			case "save":
				data, err := w.SaveState()
				if err != nil || bytes.Contains(data, []byte("velocity")) || bytes.Contains(data, []byte("throw")) {
					t.Fatalf("saving a throw failed or serialized transient motion: %s, %v", data, err)
				}
				loaded := desktop(t)
				if err := loaded.LoadState(data); err != nil {
					t.Fatal(err)
				}
				loaded.SetApplications(&fakeApplications{surfaces: append([]experience.ApplicationSurface(nil), apps.surfaces...)})
				loaded.Update(time.Second)
				if loaded.Document().View.Application.Layouts != position || loaded.windowThrow != nil {
					t.Fatal("saved throw failed to retain its current placement or replayed velocity")
				}
			case "withdraw":
				apps.surfaces = nil
				w.Update(0)
			case "overview":
				command(t, w, Action{Kind: ToggleApplicationOverview})
			}
			w.Update(time.Second)
			if w.windowThrow != nil || w.Document().View.Application.Layouts != position {
				t.Fatal("interruption did not preserve the current resting position")
			}
			if reason == "overview" {
				command(t, w, Action{Kind: Undo})
			}
			if w.historyPosition != history+1 {
				t.Fatal("interrupted throw was not one history entry")
			}
			command(t, w, Action{Kind: Undo})
			if w.Document().View.Application.Layouts != before.View.Application.Layouts {
				t.Fatal("interrupted throw could not be undone in one step")
			}
		})
	}
}

func TestWindowThrowUndoAndLoadNeverReplayMotion(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 1)
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	before := w.Document()
	startWindowThrow(t, w, apps.surfaces[0])
	w.Update(200 * time.Millisecond)
	after := w.Document()
	command(t, w, Action{Kind: Undo})
	w.Update(time.Second)
	if w.Document() != before || w.windowThrow != nil {
		t.Fatal("undo during motion did not restore the gesture's starting state")
	}
	command(t, w, Action{Kind: Redo})
	w.Update(time.Second)
	if w.Document() != after || w.windowThrow != nil {
		t.Fatal("redo during motion replayed the animation or lost its position")
	}
	startWindowThrow(t, w, apps.surfaces[0])
	if err := w.LoadState([]byte(`{"version":99}`)); err == nil || w.windowThrow == nil {
		t.Fatal("invalid load changed an active throw")
	}
	if err := w.LoadState(data); err != nil {
		t.Fatal(err)
	}
	w.Update(time.Second)
	if w.Document() != before || w.windowThrow != nil || w.CanUndo() {
		t.Fatal("successful load retained a transient throw or old history")
	}
}

func TestWindowThrowStopsWholeGroupAtSpatialBounds(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 2)
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[1].Key})
	command(t, w, Action{Kind: MoveApplications, DeltaX: 98.5 - w.Document().View.Application.Layouts[1].X})
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[0].Key, Additive: true})
	command(t, w, Action{Kind: GroupApplications})
	before := w.Document().View.Application.Layouts
	startWindowThrow(t, w, apps.surfaces[0])
	w.Update(10 * time.Second)
	after := w.Document().View.Application.Layouts
	if w.windowThrow != nil || after[1].X != 100 || abs((after[1].X-after[0].X)-(before[1].X-before[0].X)) > .00001 || abs((after[1].Y-after[0].Y)-(before[1].Y-before[0].Y)) > .00001 {
		t.Fatal("group failed to stop together at the first spatial bound")
	}
	if err := w.Document().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWindowThrowPlaceKeepsUngroupedSelectionWhenGrabbingAnotherMember(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 2)
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[1].Key, Additive: true})
	command(t, w, Action{Kind: ToggleApplicationPlacement})
	before, history := w.Document(), w.historyPosition
	if before.View.Application.Active != apps.surfaces[1].Key || before.View.Application.Selected != 3 || before.View.Application.Layouts[0].Group != 0 || before.View.Application.Layouts[1].Group != 0 {
		t.Fatal("test did not prepare two ungrouped selected windows with the second active")
	}
	x, y := visibleApplication(t, w, apps.surfaces[0])
	timedWindowGestureAt(t, w, x, y, [4]uint32{1000, 1020, 1040, 1050})
	if w.windowThrow == nil || w.Document().View.Application.Selected != before.View.Application.Selected || w.Document().View.Application.Active != apps.surfaces[0].Key {
		t.Fatal("Place drag did not throw all selected windows while activating the grabbed member")
	}
	w.Update(10 * time.Second)
	after := w.Document()
	a, b := before.View.Application.Layouts, after.View.Application.Layouts
	if b[0].X == a[0].X || abs((b[0].X-a[0].X)-(b[1].X-a[1].X)) > .00001 || abs((b[0].Y-a[0].Y)-(b[1].Y-a[1].Y)) > .00001 || b[0].Group != 0 || b[1].Group != 0 {
		t.Fatal("Place throw changed only one selected member or changed their grouping")
	}
	if w.historyPosition != history+1 || len(apps.events) != 0 || w.OwnsKeyboard() {
		t.Fatal("Place throw split its history or forwarded input to application content")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("one undo failed to restore Place movement and previous active selection")
	}
	command(t, w, Action{Kind: Redo})
	if w.Document() != after || w.windowThrow != nil {
		t.Fatal("Place redo failed to restore the complete move without replaying it")
	}
}

func TestWindowThrowLeavesNewlyArrivingApplicationUntouchedThroughUndo(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 1)
	before := w.Document()
	startWindowThrow(t, w, apps.surfaces[0])
	w.Update(100 * time.Millisecond)
	arrival := experience.ApplicationSurface{ID: 999, Key: "fresh:arrival", Texture: apps.surfaces[0].Texture}
	apps.surfaces = append(apps.surfaces, arrival)
	w.Update(0)
	index := w.Document().View.Application.index(arrival.Key)
	if index < 0 || w.windowThrow == nil {
		t.Fatal("new application failed to join the workspace or interrupted an unrelated throw")
	}
	position := w.Document().View.Application.Layouts[index]
	w.Update(10 * time.Second)
	if w.Document().View.Application.Layouts[index] != position {
		t.Fatal("a window arriving during a throw inherited the moving selection")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document().View.Application.Layouts[index] != position || w.scene.Node(w.applicationNodes[arrival.ID]) == nil || w.Document().View.Application.Layouts[0] != before.View.Application.Layouts[0] {
		t.Fatal("throw undo removed or changed the newly arrived window, or lost the original position")
	}
	command(t, w, Action{Kind: Redo})
	if w.Document().View.Application.Layouts[index] != position || w.windowThrow != nil {
		t.Fatal("throw redo changed the newly arrived window or restarted motion")
	}
}
