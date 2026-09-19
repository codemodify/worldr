package workspace

import (
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

func TestOverviewShortcutRevokesApplicationInput(t *testing.T) {
	for _, appID := range []string{"native:terminal", "legacy:foot"} {
		t.Run(appID, func(t *testing.T) {
			w, apps := applicationStudy(t)
			apps.surfaces[0].AppID = appID
			x, y := visibleApplicationPoint(t, w)
			pointer(w, experience.PointerDown, x, y)
			// Model a client with held pointer and modifier keys when the
			// reserved chord arrives. Both input streams must be cancelled.
			for _, code := range []uint32{29, 56} {
				w.Handle(experience.Event{Kind: experience.KeyInput, Keycode: code, Pressed: true})
			}
			before := len(apps.events)
			chord := experience.Event{Kind: experience.KeyInput, Key: experience.KeyO, Keycode: 24, Pressed: true, Modifiers: experience.ModControl | experience.ModAlt}
			if !w.Handle(chord) || !w.m.applicationState.Overview {
				t.Fatal("focused application prevented the overview shortcut")
			}
			if w.OwnsKeyboard() || w.applicationCapturedID != 0 || len(w.applicationButtons) != 0 || apps.focus[len(apps.focus)-1] != 0 {
				t.Fatal("overview retained application keyboard or pointer ownership")
			}
			keyboardCancels, pointerCancels := 0, 0
			for _, event := range apps.events[before:] {
				if event.id != 777 {
					t.Fatalf("cancellation reached the wrong application: %+v", event)
				}
				switch event.event.Kind {
				case experience.KeyboardCancel:
					keyboardCancels++
				case experience.PointerCancel:
					pointerCancels++
				default:
					t.Fatalf("reserved chord leaked into the application: %+v", event)
				}
			}
			if keyboardCancels != 1 || pointerCancels != 1 {
				t.Fatalf("held input was not cancelled exactly once: keyboard=%d pointer=%d", keyboardCancels, pointerCancels)
			}
			chord.Repeat = true
			if !w.Handle(chord) || !w.m.applicationState.Overview {
				t.Fatal("repeating the shortcut toggled overview again")
			}
			if !key(w, experience.KeyEscape, 0) || w.m.applicationState.Overview || w.OwnsKeyboard() {
				t.Fatal("Escape did not return from overview without application focus")
			}
			// A new click can restore app focus before O is released. The
			// app must not receive that unmatched release, even after Alt/Ctrl.
			x, y = visibleApplicationPoint(t, w)
			pointer(w, experience.PointerDown, x, y)
			pointer(w, experience.PointerUp, x, y)
			before = len(apps.events)
			chord.Pressed, chord.Repeat, chord.Modifiers = false, false, 0
			if !w.Handle(chord) || len(apps.events) != before {
				t.Fatal("reserved key release reached the newly focused application")
			}
			chord.Pressed = true
			if !w.Handle(chord) || len(apps.events) != before+1 || apps.events[before].event != chord {
				t.Fatal("ordinary O did not resume after the reserved key was released")
			}
		})
	}
}

func TestFocusedApplicationRetainsOrdinaryOverviewAndEscapeKeys(t *testing.T) {
	w, apps := applicationStudy(t)
	x, y := visibleApplicationPoint(t, w)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	before := w.Document()
	for _, event := range []experience.Event{
		{Kind: experience.KeyInput, Key: experience.KeyO, Keycode: 24, Pressed: true},
		{Kind: experience.KeyInput, Key: experience.KeyO, Keycode: 24},
		{Kind: experience.KeyInput, Key: experience.KeyO, Keycode: 24, Pressed: true, Modifiers: experience.ModControl},
		{Kind: experience.KeyInput, Key: experience.KeyO, Keycode: 24, Modifiers: experience.ModControl},
		{Kind: experience.KeyInput, Key: experience.KeyO, Keycode: 24, Pressed: true, Modifiers: experience.ModControl | experience.ModAlt | experience.ModShift},
		{Kind: experience.KeyInput, Key: experience.KeyEscape, Keycode: 1, Pressed: true},
	} {
		count := len(apps.events)
		if !w.Handle(event) || len(apps.events) != count+1 || apps.events[count].event != event {
			t.Fatalf("application did not retain its ordinary key: %+v", event)
		}
		if !w.OwnsKeyboard() || w.Document() != before {
			t.Fatal("application key changed workspace view or focus")
		}
	}
}

func TestEscapeFromOverviewPreservesPreviousView(t *testing.T) {
	for _, reading := range []bool{false, true} {
		name := "space"
		if reading {
			name = "reading"
		}
		t.Run(name, func(t *testing.T) {
			w, apps := applicationStudy(t)
			command(t, w, Action{Kind: OrbitCamera, DeltaX: 35, DeltaY: -15})
			command(t, w, Action{Kind: MoveApplications, DeltaX: .1, DeltaDepth: -.3})
			if reading {
				command(t, w, Action{Kind: ToggleApplicationReading})
			}
			before := w.Document()
			w.Draw(1440, 900)
			x, y := visibleApplicationPoint(t, w)
			pointer(w, experience.PointerDown, x, y)
			pointer(w, experience.PointerUp, x, y)
			if !key(w, experience.KeyO, experience.ModControl|experience.ModAlt) {
				t.Fatal("overview shortcut was not consumed")
			}
			w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyO})
			count := len(apps.events)
			if !key(w, experience.KeyEscape, 0) || w.Document() != before {
				t.Fatal("Escape changed the prior view, camera, or application placement")
			}
			if w.OwnsKeyboard() || len(apps.events) != count {
				t.Fatal("returning from overview sent input or restored application focus")
			}
			if !key(w, experience.KeyO, 0) || !w.m.applicationState.Overview {
				t.Fatal("ordinary O stopped opening overview from workspace focus")
			}
			if !key(w, experience.KeyO, experience.ModControl|experience.ModAlt) || w.Document() != before {
				t.Fatal("reserved shortcut did not return from overview")
			}
		})
	}
}

func TestOverviewShortcutFocusLossClearsSuppressedKey(t *testing.T) {
	w, apps := applicationStudy(t)
	key(w, experience.KeyO, experience.ModControl|experience.ModAlt)
	w.Handle(experience.Event{Kind: experience.KeyboardCancel})
	key(w, experience.KeyEscape, 0)
	x, y := visibleApplicationPoint(t, w)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	before := len(apps.events)
	if !key(w, experience.KeyO, 0) || len(apps.events) != before+1 || apps.events[before].event.Key != experience.KeyO {
		t.Fatal("host focus loss left the reserved key suppressed")
	}
}

func TestOverviewShortcutsRecoverWhenEveryLiveWindowIsMinimized(t *testing.T) {
	for _, test := range []struct {
		name      string
		modifiers experience.Modifiers
	}{
		{name: "workspace O"},
		{name: "reserved Ctrl Alt O", modifiers: experience.ModControl | experience.ModAlt},
	} {
		t.Run(test.name, func(t *testing.T) {
			w, apps := multipleApplications(t, 2)
			for _, surface := range apps.surfaces {
				command(t, w, Action{Kind: ToggleApplicationMinimized, ApplicationKey: surface.Key})
			}
			w.Update(0)
			if w.application.ID != 0 || len(w.visibleApplications()) != len(apps.surfaces) {
				t.Fatal("fixture did not retain only minimized live windows")
			}
			before := len(apps.events)
			if !key(w, experience.KeyO, test.modifiers) || !w.m.applicationState.Overview {
				t.Fatal("overview shortcut could not recover minimized windows")
			}
			if len(apps.events) != before || w.OwnsKeyboard() {
				t.Fatal("minimized-window recovery leaked input or granted application focus")
			}
			w.Draw(1440, 900)
			for _, surface := range apps.surfaces {
				if node := w.scene.Node(w.applicationNodes[surface.ID]); node == nil || node.Hidden || node.Surface != surface.Texture {
					t.Fatalf("overview did not reveal minimized window %q", surface.Key)
				}
			}
		})
	}
}
