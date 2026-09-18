package workspace

import (
	"math"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

func overviewKeyEvent(code uint32, pressed bool) experience.Event {
	event := experience.Event{Kind: experience.KeyInput, Keycode: code, Pressed: pressed}
	if code == 105 {
		event.Key = experience.KeyLeft
	}
	if code == 106 {
		event.Key = experience.KeyRight
	}
	return event
}

func tapOverviewKey(t *testing.T, w *Workspace, code uint32) {
	t.Helper()
	for _, pressed := range []bool{true, false} {
		if !w.Handle(overviewKeyEvent(code, pressed)) {
			t.Fatalf("workspace navigation stroke not consumed: code=%d pressed=%v", code, pressed)
		}
	}
}

func TestSingleApplicationOverviewCentersAndFitsItsOnlyThumbnail(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	command(t, w, Action{Kind: ToggleApplicationOverview})
	for _, size := range [][2]int{{1440, 900}, {900, 1400}, {2880, 1800}} {
		w.Draw(size[0], size[1])
		centerX, centerY := projectedApplication(w, apps.surfaces[0], 480, 300)
		left, top := projectedApplication(w, apps.surfaces[0], 0, 0)
		right, bottom := projectedApplication(w, apps.surfaces[0], 960, 600)
		v := w.viewport
		if math.Abs(float64(centerX-v.X-v.Width/2)) > .01 || math.Abs(float64(centerY-v.Y-v.Height/2)) > .01 {
			t.Fatalf("single overview window is not centered at %v: (%v, %v)", size, centerX, centerY)
		}
		if left <= v.X || right >= v.X+v.Width || top <= v.Y || bottom >= v.Y+v.Height || right-left < v.Width*.55 || bottom-top < v.Height*.65 {
			t.Fatalf("single overview window did not fit and use the available area at %v: bounds (%v, %v)-(%v, %v), viewport %+v", size, left, top, right, bottom, v)
		}
		for _, code := range []uint32{105, 106, 103, 108} {
			tapOverviewKey(t, w, code)
			if w.application.ID != apps.surfaces[0].ID {
				t.Fatal("single-app navigation selected an empty grid slot")
			}
		}
	}
}

func TestOverviewArrowNavigationMatchesRowsWithoutChangingTimeline(t *testing.T) {
	w, apps := multipleApplications(t, 10)
	command(t, w, Action{Kind: ToggleApplicationOverview})
	w.Draw(1440, 900)
	// Inspect projected centers to establish the actual four-column layout,
	// independently of the navigation helper's column calculation.
	x0, y0 := projectedApplication(w, apps.surfaces[0], 480, 300)
	x3, y3 := projectedApplication(w, apps.surfaces[3], 480, 300)
	x4, y4 := projectedApplication(w, apps.surfaces[4], 480, 300)
	if math.Abs(float64(y3-y0)) > .01 || math.Abs(float64(x4-x0)) > .01 || x3 <= x0 || y4 <= y0 {
		t.Fatal("fixture did not render four columns in the overview")
	}
	before, events := w.Document(), len(apps.events)
	for _, step := range []struct {
		code  uint32
		index int
	}{
		{105, 0}, {103, 0}, // Left/top boundaries.
		{106, 1}, {106, 2}, {106, 3}, {106, 3}, // Right stays in its row.
		{108, 7}, {108, 9}, {108, 9}, // Incomplete last row and bottom boundary.
		{106, 9}, {105, 8}, {105, 8},
		{103, 4}, {103, 0},
	} {
		tapOverviewKey(t, w, step.code)
		if w.application.ID != apps.surfaces[step.index].ID {
			t.Fatalf("arrow %d selected %d, want live item %d", step.code, w.application.ID, step.index)
		}
		if w.Document().Timeline != before.Timeline || w.Document().View.Camera != before.View.Camera || w.m.applicationState.Layouts != before.View.Application.Layouts || len(apps.events) != events || w.OwnsKeyboard() {
			t.Fatal("overview navigation changed time/placement/camera or sent application input")
		}
	}
}

func TestOverviewEnterThenFreshEnterExplicitlyReadsAndFocuses(t *testing.T) {
	for _, appID := range []string{"native:terminal", "legacy:foot"} {
		for _, code := range []uint32{28, 96} {
			t.Run(appID+map[uint32]string{28: "/enter", 96: "/keypad-enter"}[code], func(t *testing.T) {
				w, apps := multipleApplications(t, 2)
				apps.surfaces[0].AppID = appID
				command(t, w, Action{Kind: MoveApplications, DeltaX: 60, DeltaDepth: -15})
				command(t, w, Action{Kind: ZoomCamera, DeltaZoom: .2})
				command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[1].Key})
				command(t, w, Action{Kind: ToggleApplicationOverview})
				w.Draw(1440, 900)
				tapOverviewKey(t, w, 105)
				before, events := w.Document(), len(apps.events)
				if !w.Handle(overviewKeyEvent(code, true)) || w.m.applicationState.Overview || w.OwnsKeyboard() {
					t.Fatal("overview Enter did not return without typing focus")
				}
				for _, repeat := range []bool{true, false, true} {
					event := overviewKeyEvent(code, true)
					event.Repeat = repeat
					if !w.Handle(event) || w.OwnsKeyboard() || w.m.applicationReading {
						t.Fatal("held overview Enter became a focus command")
					}
				}
				if !w.Handle(overviewKeyEvent(code, false)) || len(apps.events) != events {
					t.Fatal("overview Enter release reached an application")
				}
				if !w.Handle(overviewKeyEvent(code, true)) || !w.OwnsKeyboard() || !w.m.applicationReading || apps.focus[len(apps.focus)-1] != 700 {
					t.Fatal("fresh Enter did not explicitly read and focus the selected window")
				}
				for _, repeat := range []bool{false, true} {
					event := overviewKeyEvent(code, true)
					event.Repeat = repeat
					w.Handle(event)
				}
				w.Handle(overviewKeyEvent(code, false))
				if len(apps.events) != events || w.Document().Timeline != before.Timeline || w.Document().View.Camera != before.View.Camera || w.m.applicationState.Layouts != before.View.Application.Layouts {
					t.Fatal("focus command leaked its Enter stroke or changed saved work")
				}
				w.Draw(1440, 900)
				if hit, ok := w.applicationHit(w.viewport.X+w.viewport.Width/2, w.viewport.Y+w.viewport.Height/2); !ok || hit.Node != w.applicationNodes[700] {
					t.Fatal("Enter failed to retrieve the selected background window into Read")
				}
				for _, pressed := range []bool{true, false} {
					event := overviewKeyEvent(code, pressed)
					count := len(apps.events)
					if !w.Handle(event) || len(apps.events) != count+1 || apps.events[count].id != 700 || apps.events[count].event != event {
						t.Fatal("ordinary focused application Enter was intercepted")
					}
				}
				w.Handle(experience.Event{Kind: experience.KeyboardCancel})
				if w.OwnsKeyboard() || apps.focus[len(apps.focus)-1] != 0 || apps.events[len(apps.events)-1].event.Kind != experience.KeyboardCancel {
					t.Fatal("keyboard loss did not cancel explicit Enter focus")
				}
			})
		}
	}
}

func TestOverviewHeldArrowsDoNotLeakAcrossViewAndFocus(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	command(t, w, Action{Kind: ToggleApplicationOverview})
	before := w.Document().Timeline
	press := overviewKeyEvent(106, true)
	w.Handle(press)
	selected := w.application.ID
	for _, repeat := range []bool{true, false} {
		press.Repeat = repeat
		w.Handle(press)
		if w.application.ID != selected {
			t.Fatal("held navigation repeated selection without a fresh press")
		}
	}
	key(w, experience.KeyEscape, 0)
	tapOverviewKey(t, w, 28) // Explicitly grant typing focus after return.
	events := len(apps.events)
	press.Modifiers = experience.ModControl | experience.ModAlt
	press.Repeat = true
	w.Handle(press)
	w.Handle(overviewKeyEvent(106, false))
	if len(apps.events) != events || w.Document().Timeline != before {
		t.Fatal("reserved arrow repeat/release leaked after overview lost ownership")
	}
	press = overviewKeyEvent(106, true)
	if !w.Handle(press) || len(apps.events) != events+1 || apps.events[events].event != press {
		t.Fatal("fresh application arrow did not resume after the reserved release")
	}
	w.Handle(overviewKeyEvent(106, false))
	w.Handle(experience.Event{Kind: experience.KeyboardCancel})
	press = overviewKeyEvent(105, true)
	if !w.Handle(press) || w.Document().Timeline.Seconds != before.Seconds-.5 {
		t.Fatal("ordinary workspace arrows stopped scrubbing outside overview")
	}
}

func TestOverviewModifiedNavigationIsConsumedAndGlobalLaunchStillWorks(t *testing.T) {
	w, apps := multipleApplications(t, 3)
	launcher := terminalLauncher(t, apps)
	w.SetApplications(launcher)
	command(t, w, Action{Kind: ToggleApplicationOverview})
	before := w.Document()
	for _, code := range []uint32{103, 105, 106, 108, 28, 96} {
		for _, mods := range []experience.Modifiers{experience.ModControl, experience.ModShift, experience.ModAlt, experience.ModSuper, experience.ModControl | experience.ModShift} {
			event := overviewKeyEvent(code, true)
			event.Modifiers = mods
			if !w.Handle(event) || w.Document() != before {
				t.Fatalf("modified overview key changed the document: %+v", event)
			}
			event.Pressed, event.Modifiers = false, 0
			if !w.Handle(event) || w.Document() != before {
				t.Fatal("modified key release fell through to a native shortcut")
			}
		}
	}
	if !w.Handle(terminalChord(28)) || len(launcher.launched) != 1 || !w.m.applicationState.Overview {
		t.Fatal("overview intercepted the global terminal launcher")
	}
	w.Handle(overviewKeyEvent(28, false))
	if w.terminalShortcutKeys != 0 {
		t.Fatal("overview stole the launch chord's release")
	}
	tapOverviewKey(t, w, 28)
	if w.m.applicationState.Overview || w.OwnsKeyboard() {
		t.Fatal("ordinary overview Enter did not resume after the global launch")
	}
	if len(apps.events) != 0 {
		t.Fatal("modified overview navigation reached an application")
	}
}

func TestOverviewNavigationReflowsAfterDisconnectAndResize(t *testing.T) {
	w, apps := multipleApplications(t, 7)
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[5].Key})
	command(t, w, Action{Kind: ToggleApplicationOverview})
	w.Draw(1440, 900)
	before := w.Document().Timeline
	apps.surfaces = append(apps.surfaces[:5], apps.surfaces[6:]...)
	w.Update(0)
	w.Draw(900, 1400)
	// Six surviving windows form three columns; use the rendered centers to
	// verify that the fourth live item is below the newly selected first one.
	x0, y0 := projectedApplication(w, apps.surfaces[0], 480, 300)
	x3, y3 := projectedApplication(w, apps.surfaces[3], 480, 300)
	if math.Abs(float64(x3-x0)) > .01 || y3 <= y0 {
		t.Fatal("surviving overview did not reflow into three columns")
	}
	tapOverviewKey(t, w, 108)
	if w.application.ID != apps.surfaces[3].ID {
		t.Fatal("Down used the disconnected overview's old column count")
	}
	w.Draw(2880, 1800)
	tapOverviewKey(t, w, 106)
	if w.application.ID != apps.surfaces[4].ID || w.Document().Timeline != before {
		t.Fatal("scaled overview navigation changed order or scrubbed the timeline")
	}
	apps.surfaces = nil
	w.Update(0)
	for _, code := range []uint32{105, 106, 103, 108} {
		tapOverviewKey(t, w, code)
	}
	tapOverviewKey(t, w, 96)
	if w.m.applicationState.Overview || w.OwnsKeyboard() || w.Document().Timeline != before {
		t.Fatal("empty overview leaked keys or failed to return safely")
	}
}

func TestOverviewKeyboardCancelClearsHeldEnter(t *testing.T) {
	w, _ := applicationStudy(t)
	command(t, w, Action{Kind: ToggleApplicationOverview})
	w.Handle(overviewKeyEvent(28, true))
	w.Handle(experience.Event{Kind: experience.KeyboardCancel})
	if w.overviewNavigationKeys != 0 {
		t.Fatal("keyboard cancellation retained overview holds")
	}
	if !w.Handle(overviewKeyEvent(28, true)) || !w.OwnsKeyboard() || !w.m.applicationReading {
		t.Fatal("fresh Enter after host focus loss remained suppressed")
	}
}

func TestEnterReadFocusCancelsPlacementPreview(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	command(t, w, Action{Kind: ToggleApplicationPlacement})
	w.Draw(1440, 900)
	before := w.m.applicationState.Layouts
	x, y := visibleApplication(t, w, apps.surfaces[0])
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerMove, x+30, y+15)
	if w.pointer.kind != captureApplicationPlacement || w.m.applicationState.Layouts == before {
		t.Fatal("fixture did not start placement preview")
	}
	tapOverviewKey(t, w, 28)
	if !w.OwnsKeyboard() || !w.m.applicationReading || w.m.applicationState.Placing || w.pointer.kind != captureNone || w.m.applicationState.Layouts != before || len(apps.events) != 0 {
		t.Fatal("explicit Read focus retained placement preview or sent the command to the app")
	}
}
