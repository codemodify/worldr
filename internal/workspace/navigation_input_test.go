package workspace

import (
	"math"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

func navigatorApplications(t *testing.T) (*Workspace, *dockApplications) {
	t.Helper()
	w, err := NewNavigator()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	apps := newDockApplications(t, "note", "files", "terminal")
	for _, kind := range []string{"files", "note", "terminal"} {
		if _, err := apps.LaunchApplication(kind); err != nil {
			t.Fatal(err)
		}
	}
	apps.launched = nil
	w.navigation.motionEnabled = false
	w.SetApplications(apps)
	w.Draw(1280, 820)
	return w, apps
}

func navigatorClick(t *testing.T, w *Workspace, b box) {
	t.Helper()
	for _, kind := range []experience.EventKind{experience.PointerDown, experience.PointerUp} {
		if !w.Handle(experience.Event{Kind: kind, X: b.x + b.w/2, Y: b.y + b.h/2, Button: experience.ButtonPrimary}) {
			t.Fatalf("navigation click not consumed: %v at %+v", kind, b)
		}
	}
	w.Draw(w.width, w.height)
}

func navigatorStroke(t *testing.T, w *Workspace, key experience.Key, code uint32, mods experience.Modifiers) {
	t.Helper()
	for _, pressed := range []bool{true, false} {
		if !w.Handle(experience.Event{Kind: experience.KeyInput, Key: key, Keycode: code, Pressed: pressed, Modifiers: mods}) {
			t.Fatalf("navigation stroke not consumed: %s (%d), pressed=%v", key, code, pressed)
		}
	}
	w.Draw(w.width, w.height)
}

func navigatorFocus(t *testing.T, w *Workspace, key string) {
	t.Helper()
	if w.navigation.level == navigationHome {
		space := w.m.applicationState.Layouts[w.m.applicationState.index(key)].Space
		for _, project := range w.navigation.layout.projects {
			if project.space == space {
				navigatorClick(t, w, project.bounds)
				break
			}
		}
		if w.navigation.level != navigationProject {
			t.Fatal("Home project click did not enter the project")
		}
	}
	for _, app := range w.navigation.layout.apps {
		if app.key == key && app.bounds.w > 0 {
			navigatorClick(t, w, w.navigation.motion.poses()[app.slot].bounds)
			if w.navigation.level != navigationApp || w.application.Key != key || !w.OwnsKeyboard() {
				t.Fatal("preview did not focus the selected live application")
			}
			return
		}
	}
	t.Fatal("application preview is missing", key)
}

func TestNavigatorHomePreviewFocusAndClientCoordinates(t *testing.T) {
	w, apps := navigatorApplications(t)
	if w.navigation.level != navigationHome || w.OwnsKeyboard() {
		t.Fatal("navigator should begin at Home without a client keyboard owner")
	}
	navigatorFocus(t, w, apps.surfaces[1].Key)
	for _, sent := range apps.events {
		if sent.event.Kind == experience.PointerDown || sent.event.Kind == experience.PointerUp {
			t.Fatal("opening preview click leaked into client content")
		}
	}
	id := apps.surfaces[1].ID
	if apps.focus[len(apps.focus)-1] != id {
		t.Fatal("provider focus does not match the enlarged app")
	}
	apps.events = nil
	x, y := projectedApplication(w, apps.surfaces[1], 317, 211)
	for _, e := range []experience.Event{
		{Kind: experience.PointerScroll, X: x, Y: y, ScrollY: 12, ScrollX: -3},
		{Kind: experience.KeyInput, Key: experience.KeyEscape, Keycode: 1, Pressed: true},
		{Kind: experience.KeyInput, Key: experience.KeyEscape, Keycode: 1},
		{Kind: experience.KeyInput, Key: experience.KeyS, Keycode: 31, Pressed: true, Modifiers: experience.ModControl},
		{Kind: experience.KeyInput, Key: experience.KeyS, Keycode: 31},
	} {
		if !w.Handle(e) {
			t.Fatalf("focused client event was not consumed: %+v", e)
		}
		last := apps.events[len(apps.events)-1]
		if last.id != id || last.event.Kind != e.Kind {
			t.Fatal("event reached a different app")
		}
		if e.Kind == experience.PointerScroll {
			if math.Abs(float64(last.event.X-317)) > .1 || math.Abs(float64(last.event.Y-211)) > .1 || last.event.ScrollX != -3 || last.event.ScrollY != 12 {
				t.Fatalf("presentation and client coordinate mapping disagree: %+v", last.event)
			}
		} else if last.event != e {
			t.Fatal("ordinary client key changed in transit")
		}
	}
	if w.navigation.level != navigationApp || w.applicationFocusedID != id {
		t.Fatal("client Escape or scrolling navigated away")
	}
	navigatorClick(t, w, w.navigation.layout.back)
	if w.navigation.level != navigationProject || w.OwnsKeyboard() {
		t.Fatal("Back did not return to the project's previews")
	}
	navigatorClick(t, w, w.navigation.layout.back)
	if w.navigation.level != navigationHome {
		t.Fatal("second Back did not return Home")
	}
}

func TestNavigatorReservedStrokesDrainAcrossModalAndModifierChanges(t *testing.T) {
	w, apps := navigatorApplications(t)
	navigatorFocus(t, w, apps.surfaces[0].Key)
	mods := experience.ModControl | experience.ModAlt
	o := experience.Event{Kind: experience.KeyInput, Key: experience.KeyO, Keycode: 24, Pressed: true, Modifiers: mods}
	w.Handle(o)
	if w.navigation.level != navigationProject || w.OwnsKeyboard() {
		t.Fatal("overview chord did not leave client focus")
	}
	// Open Search before the earlier O stroke releases its physical key.
	w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeySpace, Keycode: 57, Pressed: true, Modifiers: mods})
	if w.commands == nil || !w.commands.open {
		t.Fatal("Search chord did not open the palette")
	}
	apps.events = nil
	for _, e := range []experience.Event{
		{Kind: experience.KeyboardModifiers},
		{Kind: experience.KeyInput, Key: experience.KeyO, Keycode: 24, Pressed: true, Repeat: true},
		{Kind: experience.KeyInput, Key: experience.KeyO, Keycode: 24},
		{Kind: experience.KeyInput, Key: experience.KeySpace, Keycode: 57},
	} {
		w.Handle(e)
	}
	if len(w.navigation.held) != 0 || len(w.commands.held) != 0 || len(apps.events) != 0 || w.commands.field.Text() != "" || !w.commands.open {
		t.Fatal("navigation release stranded a hold, typed into Search, or leaked into a client")
	}
	navigatorStroke(t, w, experience.KeyEscape, 1, 0)
	if w.commands.open || w.OwnsKeyboard() {
		t.Fatal("closing project Search incorrectly restored client focus")
	}
	navigatorStroke(t, w, experience.KeyO, 24, mods)
	if w.navigation.level != navigationApp || w.applicationFocusedID != apps.surfaces[0].ID {
		t.Fatal("overview return lost the previously focused app")
	}
}

func TestNavigatorAppSwitchKeepsTheWholeStroke(t *testing.T) {
	w, apps := navigatorApplications(t)
	navigatorFocus(t, w, apps.surfaces[0].Key)
	for _, tc := range []struct {
		key  experience.Key
		code uint32
		want int
	}{{experience.KeyK, 37, 1}, {experience.KeyJ, 36, 0}, {experience.KeyJ, 36, 2}} {
		press := experience.Event{Kind: experience.KeyInput, Key: tc.key, Keycode: tc.code, Pressed: true, Modifiers: experience.ModControl | experience.ModAlt}
		w.Handle(press)
		if w.applicationFocusedID != apps.surfaces[tc.want].ID {
			t.Fatal("app switch selected the wrong stable slot")
		}
		apps.events = nil
		for _, repeat := range []bool{false, true} {
			press.Repeat, press.Modifiers = repeat, 0
			w.Handle(press)
		}
		press.Pressed = false
		w.Handle(press)
		if len(apps.events) != 0 || w.applicationFocusedID != apps.surfaces[tc.want].ID || len(w.navigation.held) != 0 {
			t.Fatal("switch chord repeated or leaked its tail into the new client")
		}
		press.Pressed, press.Repeat = true, false
		w.Handle(press)
		press.Pressed = false
		w.Handle(press)
		if len(apps.events) != 2 || apps.events[0].id != apps.surfaces[tc.want].ID {
			t.Fatal("fresh ordinary key did not resume client ownership")
		}
	}
}

func TestNavigatorOrdinaryHeldKeysCannotBecomeNavigationCommands(t *testing.T) {
	for _, tc := range []struct {
		key  experience.Key
		code uint32
	}{{experience.KeyO, 24}, {experience.KeyJ, 36}, {experience.KeySpace, 57}} {
		t.Run(string(tc.key), func(t *testing.T) {
			w, apps := navigatorApplications(t)
			navigatorFocus(t, w, apps.surfaces[0].Key)
			press := experience.Event{Kind: experience.KeyInput, Key: tc.key, Keycode: tc.code, Pressed: true}
			w.Handle(press)
			press.Modifiers = experience.ModControl | experience.ModAlt
			for _, repeat := range []bool{true, false} {
				press.Repeat = repeat
				w.Handle(press)
			}
			press.Pressed = false
			w.Handle(press)
			if w.navigation.level != navigationApp || w.applicationFocusedID != apps.surfaces[0].ID || w.commands != nil && w.commands.open || len(w.navigation.held) != 0 {
				t.Fatal("changing modifiers converted a held client key into navigation")
			}
		})
	}
}

func TestNavigatorClientDragRetainsCaptureAcrossChromeAndResize(t *testing.T) {
	w, apps := navigatorApplications(t)
	navigatorFocus(t, w, apps.surfaces[0].Key)
	x, y := projectedApplication(w, apps.surfaces[0], 480, 300)
	apps.events = nil
	pointer(w, experience.PointerDown, x, y)
	if w.applicationCapturedID != apps.surfaces[0].ID {
		t.Fatal("client content did not acquire capture")
	}
	tools := w.navigation.layout.tools
	tx, ty := tools.x+tools.w/2, tools.y+tools.h/2
	pointer(w, experience.PointerMove, tx, ty)
	w.Handle(experience.Event{Kind: experience.PointerScroll, X: tx, Y: ty, ScrollY: 10})
	pointer(w, experience.PointerUp, tx, ty)
	if w.navigation.toolsOpen || w.applicationCapturedID != 0 || len(w.applicationButtons) != 0 {
		t.Fatal("crossing Tools stole client capture or opened a menu")
	}
	want := []experience.EventKind{experience.PointerDown, experience.PointerMove, experience.PointerScroll, experience.PointerUp}
	if len(apps.events) < len(want) {
		t.Fatal("captured client missed part of its pointer gesture")
	}
	for i, kind := range want {
		if apps.events[i].id != apps.surfaces[0].ID || apps.events[i].event.Kind != kind {
			t.Fatal("captured pointer gesture changed owner or ordering")
		}
	}
	pointer(w, experience.PointerDown, x, y)
	apps.events = nil
	w.Draw(900, 650)
	if w.applicationCapturedID != 0 || len(w.applicationButtons) != 0 {
		t.Fatal("resize retained a capture with old coordinate mapping")
	}
	for _, e := range apps.events {
		if e.event.Kind == experience.PointerUp {
			t.Fatal("resize committed a captured client gesture")
		}
	}
	x, y = projectedApplication(w, apps.surfaces[0], 321, 123)
	apps.events = nil
	pointer(w, experience.PointerDown, x, y)
	if len(apps.events) != 1 || math.Abs(float64(apps.events[0].event.X-321)) > .1 || math.Abs(float64(apps.events[0].event.Y-123)) > .1 {
		t.Fatal("first client click after resize used stale geometry")
	}
	pointer(w, experience.PointerUp, x, y)
}

func TestNavigatorToolsDismissalAndKeyboardLaunch(t *testing.T) {
	w, apps := navigatorApplications(t)
	navigatorFocus(t, w, apps.surfaces[0].Key)
	navigatorClick(t, w, w.navigation.layout.tools)
	if !w.navigation.toolsOpen || w.applicationKeyboard {
		t.Fatal("Tools did not suspend app keyboard focus")
	}
	apps.events = nil
	x, y := projectedApplication(w, apps.surfaces[0], 96, 500)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if w.navigation.toolsOpen || !w.applicationKeyboard || w.applicationCapturedID != 0 {
		t.Fatal("outside menu click did not dismiss and restore app focus")
	}
	for _, e := range apps.events {
		if e.event.Kind == experience.PointerDown || e.event.Kind == experience.PointerUp {
			t.Fatal("menu dismissal click passed through to client content")
		}
	}
	navigatorClick(t, w, w.navigation.layout.tools)
	navigatorStroke(t, w, experience.KeyTab, 15, 0)
	if w.navigation.keyboard != "tool:launch:note" {
		t.Fatal("menu Tab did not select the first enabled tool")
	}
	apps.events = nil
	navigatorStroke(t, w, experience.KeyEnter, 28, 0)
	if len(apps.launched) != 1 || apps.launched[0] != "note" || w.navigation.toolsOpen || w.applicationFocusedID != apps.surfaces[len(apps.surfaces)-1].ID {
		t.Fatal("menu Enter did not launch and focus the selected real app")
	}
	for _, e := range apps.events {
		if e.event.Kind == experience.KeyInput && e.event.Keycode == 28 {
			t.Fatal("menu activation Enter leaked into the new app")
		}
	}
}

func TestNavigatorSearchRejectsStaleTextContexts(t *testing.T) {
	w, apps := navigatorApplications(t)
	navigatorFocus(t, w, apps.surfaces[0].Key)
	navigatorStroke(t, w, experience.KeySpace, 57, experience.ModControl|experience.ModAlt)
	old := w.TextInput().ContextID
	navigatorStroke(t, w, experience.KeyEscape, 1, 0)
	navigatorStroke(t, w, experience.KeySpace, 57, experience.ModControl|experience.ModAlt)
	if old == w.TextInput().ContextID || !w.TextInput().Enabled {
		t.Fatal("Search did not create a new text context")
	}
	w.Handle(experience.Event{Kind: experience.TextCommit, Text: "stale", TextContext: old})
	if w.commands.field.Text() != "" {
		t.Fatal("old input-method commit changed the reopened Search field")
	}
	w.Handle(experience.Event{Kind: experience.TextCommit, Text: "files", TextContext: w.TextInput().ContextID})
	if w.commands.field.Text() != "files" {
		t.Fatal("current Search text context was rejected")
	}
	navigatorStroke(t, w, experience.KeyEscape, 1, 0)
	apps.events = nil
	w.Handle(experience.Event{Kind: experience.TextCommit, Text: "stale", TextContext: old})
	if len(apps.events) != 0 {
		t.Fatal("stale Search text reached the focused client")
	}
}

func TestNavigatorInputCanReverseAnInFlightTransition(t *testing.T) {
	w, apps := navigatorApplications(t)
	w.navigation.motionEnabled = true
	navigatorFocus(t, w, apps.surfaces[0].Key)
	w.Update(80 * time.Millisecond)
	before := w.navigation.motion.poses()
	navigatorStroke(t, w, experience.KeyO, 24, experience.ModControl|experience.ModAlt)
	if w.navigation.level != navigationProject || w.navigation.motion.poses() != before {
		t.Fatal("navigation reversal was blocked or jumped instead of retaining current presentation")
	}
	w.Update(time.Second)
	w.Draw(w.width, w.height)
	if w.navigation.motion.moving() || w.OwnsKeyboard() {
		t.Fatal("reversed transition failed to finish at project overview")
	}
}

func TestNavigatorFocusHandoffDrainsFormerClientKeys(t *testing.T) {
	w, apps := navigatorApplications(t)
	navigatorFocus(t, w, apps.surfaces[0].Key)
	for _, code := range []uint32{30, 29, 56} {
		w.Handle(experience.Event{Kind: experience.KeyInput, Keycode: code, Pressed: true})
	}
	navigatorStroke(t, w, experience.KeyK, 37, experience.ModControl|experience.ModAlt)
	if w.applicationFocusedID != apps.surfaces[1].ID {
		t.Fatal("fixture did not switch keyboard owner")
	}
	apps.events = nil
	for _, code := range []uint32{30, 29, 56} {
		for _, pressed := range []bool{true, false} {
			if !w.Handle(experience.Event{Kind: experience.KeyInput, Keycode: code, Pressed: pressed, Repeat: pressed}) {
				t.Fatal("former client's held stroke was not drained")
			}
		}
	}
	if len(apps.events) != 0 || len(w.navigation.held) != 0 {
		t.Fatal("former client's key tail reached the newly focused application")
	}
	modifierRelease := experience.Event{Kind: experience.KeyboardModifiers}
	w.Handle(modifierRelease)
	if len(apps.events) != 1 || apps.events[0].id != apps.surfaces[1].ID || apps.events[0].event != modifierRelease {
		t.Fatal("new client did not receive the current released modifier state")
	}
}

func TestNavigatorFocusedSwitcherCrossesProjects(t *testing.T) {
	w, apps := navigatorApplications(t)
	command(t, w, Action{Kind: CreateSpace, SpaceName: "Code"})
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[1].Key})
	command(t, w, Action{Kind: MoveToSpace, Space: 1})
	w.Draw(w.width, w.height)
	navigatorFocus(t, w, apps.surfaces[0].Key)
	navigatorStroke(t, w, experience.KeyK, 37, experience.ModControl|experience.ModAlt)
	if w.m.applicationState.Space != 1 || w.applicationFocusedID != apps.surfaces[1].ID {
		t.Fatal("focused app switcher could not reach an app in another project")
	}
	navigatorStroke(t, w, experience.KeyJ, 36, experience.ModControl|experience.ModAlt)
	if w.m.applicationState.Space != 0 || w.applicationFocusedID != apps.surfaces[0].ID {
		t.Fatal("global app switcher did not return to the former project")
	}
	navigatorStroke(t, w, experience.KeyO, 24, experience.ModControl|experience.ModAlt)
	navigatorStroke(t, w, experience.KeyK, 37, experience.ModControl|experience.ModAlt)
	if w.applicationFocusedID != apps.surfaces[2].ID || w.m.applicationState.Space != 0 {
		t.Fatal("project overview switching escaped its current project")
	}
}

func TestNavigatorShiftTabAndOrphanRepeats(t *testing.T) {
	w, apps := navigatorApplications(t)
	navigatorStroke(t, w, experience.KeyTab, 15, experience.ModShift)
	if w.navigation.keyboard != "motion" {
		t.Fatal("Shift+Tab did not wrap backward from the initial Home selection")
	}
	navigatorFocus(t, w, apps.surfaces[0].Key)
	navigatorClick(t, w, w.navigation.layout.tools)
	navigatorStroke(t, w, experience.KeyTab, 15, 0)
	navigatorStroke(t, w, experience.KeyTab, 15, experience.ModShift)
	if w.navigation.keyboard != "tool:home" {
		t.Fatal("Shift+Tab did not reverse menu selection")
	}
	navigatorStroke(t, w, experience.KeyEscape, 1, 0)
	apps.events = nil
	for _, tc := range []struct {
		key  experience.Key
		code uint32
	}{{experience.KeyO, 24}, {experience.KeyK, 37}, {experience.KeySpace, 57}} {
		e := experience.Event{Kind: experience.KeyInput, Key: tc.key, Keycode: tc.code, Pressed: true, Repeat: true, Modifiers: experience.ModControl | experience.ModAlt}
		w.Handle(e)
		e.Pressed, e.Modifiers = false, 0
		w.Handle(e)
	}
	if w.navigation.level != navigationApp || w.applicationFocusedID != apps.surfaces[0].ID || len(apps.events) != 0 || w.commands != nil && w.commands.open {
		t.Fatal("orphan shortcut repeat changed navigation or reached a client")
	}
}

func TestNavigatorInvisibleToolRowsCannotActivate(t *testing.T) {
	w, apps := navigatorApplications(t)
	navigatorFocus(t, w, apps.surfaces[0].Key)
	w.navigation.motionEnabled = true
	navigatorClick(t, w, w.navigation.layout.tools)
	row := w.navigation.layout.toolItems[0].bounds
	w.Update(20 * time.Millisecond)
	if hit := w.navigationHit(row.x+row.w/2, row.y+row.h/2); hit != "menu" {
		t.Fatalf("invisible row became a live tool target: %q", hit)
	}
	navigatorClick(t, w, row)
	if len(apps.launched) != 0 || !w.navigation.toolsOpen {
		t.Fatal("clicking the unrevealed panel region launched a tool or dismissed the menu")
	}
	w.Update(200 * time.Millisecond)
	navigatorClick(t, w, row)
	if len(apps.launched) != 1 || apps.launched[0] != "note" {
		t.Fatal("fully visible tool row did not become interactive")
	}
}

func TestNavigatorModalDismissalOwnsTheChangedButton(t *testing.T) {
	for _, modal := range []string{"tools", "search"} {
		for _, button := range []experience.Button{experience.ButtonPrimary, experience.ButtonSecondary} {
			t.Run(modal+map[experience.Button]string{experience.ButtonPrimary: "/primary", experience.ButtonSecondary: "/secondary"}[button], func(t *testing.T) {
				w, apps := navigatorApplications(t)
				navigatorFocus(t, w, apps.surfaces[0].Key)
				b := w.navigation.layout.tools
				if modal == "search" {
					b = w.navigation.layout.search
				}
				navigatorClick(t, w, b)
				apps.events = nil
				// This corner is outside both overlays and all navigation buttons.
				for _, kind := range []experience.EventKind{experience.PointerDown, experience.PointerUp} {
					if !w.Handle(experience.Event{Kind: kind, Button: button, X: 10, Y: 800}) {
						t.Fatal("modal dismissal did not consume both button edges")
					}
				}
				if w.navigation.toolsOpen || w.commands != nil && w.commands.open || w.navigation.pressed != "" || w.navigation.pressButton != 0 {
					t.Fatal("modal dismissal left a stuck navigation capture")
				}
				for _, sent := range apps.events {
					if sent.event.Kind == experience.PointerDown || sent.event.Kind == experience.PointerUp {
						t.Fatal("modal dismissal click leaked to the client")
					}
				}
				x, y := projectedApplication(w, apps.surfaces[0], 480, 300)
				pointer(w, experience.PointerDown, x, y)
				if w.applicationCapturedID != apps.surfaces[0].ID {
					t.Fatal("dismissal stranded input instead of returning to the client")
				}
				pointer(w, experience.PointerUp, x, y)
			})
		}
	}
}

func TestNavigatorActiveContentWinsDuringOverlappingTransition(t *testing.T) {
	w, apps := navigatorApplications(t)
	if _, err := apps.LaunchApplication("note"); err != nil {
		t.Fatal(err)
	}
	w.Update(0)
	w.navigationProject(0)
	w.Draw(w.width, w.height)
	w.navigation.motionEnabled = true
	navigatorFocus(t, w, apps.surfaces[0].Key)
	w.Update(130 * time.Millisecond)
	w.Draw(w.width, w.height)
	poses := w.navigation.motion.poses()
	active := poses[w.m.applicationState.index(apps.surfaces[0].Key)].bounds
	found := false
	for _, card := range w.navigation.layout.apps {
		if card.active || card.bounds.w <= 0 {
			continue
		}
		other := poses[card.slot].bounds
		left, top := max(active.x, other.x), max(active.y, other.y)
		right, bottom := min(active.x+active.w, other.x+other.w), min(active.y+active.h, other.y+other.h)
		if right-left < 10 || bottom-top < 10 {
			continue
		}
		found = true
		x, y := (left+right)/2, (top+bottom)/2
		if hit := w.navigationHit(x, y); hit != "" {
			t.Fatalf("covered companion stole visible active content: %q", hit)
		}
		pointer(w, experience.PointerDown, x, y)
		if w.applicationCapturedID != apps.surfaces[0].ID {
			t.Fatal("frontmost active content did not acquire client capture")
		}
		before := w.navigation.motion.poses()
		w.Update(100 * time.Millisecond)
		if w.navigation.motion.poses() != before {
			t.Fatal("client drag's coordinate frame moved during its capture")
		}
		pointer(w, experience.PointerUp, x, y)
		break
	}
	if !found {
		t.Fatal("fixture did not exercise an overlapping transition")
	}
}

func TestNavigatorProjectPagesAndReturnSelectionStayIndependent(t *testing.T) {
	w, apps := navigatorApplications(t)
	for len(apps.surfaces) < 15 {
		if _, err := apps.LaunchApplication("note"); err != nil {
			t.Fatal(err)
		}
	}
	w.Update(0)
	command(t, w, Action{Kind: CreateSpace, SpaceName: "Code"})
	for _, app := range apps.surfaces[8:] {
		command(t, w, Action{Kind: SelectApplication, ApplicationKey: app.Key})
		command(t, w, Action{Kind: MoveToSpace, Space: 1})
	}
	w.navigationProject(0)
	w.Draw(w.width, w.height)
	navigatorClick(t, w, w.navigation.layout.pageNext)
	if w.navigation.page != 1 {
		t.Fatal("fixture did not reach the second project page")
	}
	navigatorClick(t, w, w.navigation.layout.home)
	for _, project := range w.navigation.layout.projects {
		if project.space == 0 {
			navigatorClick(t, w, project.bounds)
			break
		}
	}
	if w.navigation.page != 1 {
		t.Fatal("return from Home replaced the project's page with the Home page")
	}
	clickTab := func(space uint8) {
		t.Helper()
		for _, tab := range w.navigation.layout.tabs {
			if tab.space == space {
				navigatorClick(t, w, tab.bounds)
				return
			}
		}
		t.Fatal("missing project tab", space)
	}
	clickTab(1)
	if w.navigation.page != 0 {
		t.Fatal("new project inherited another project's page")
	}
	navigatorClick(t, w, w.navigation.layout.pageNext)
	clickTab(0)
	if w.navigation.page != 1 || w.navigation.projectPages[1] != 1 {
		t.Fatal("project switches lost independent page positions")
	}
	navigatorFocus(t, w, apps.surfaces[6].Key)
	navigatorClick(t, w, w.navigation.layout.back)
	if w.navigation.page != 1 || w.navigation.keyboard != navigationID("app", w.m.applicationState.index(apps.surfaces[6].Key)) {
		t.Fatal("return from an app lost the project page or selected preview")
	}
}

func TestNavigatorLaunchingExistingToolPreservesItsProject(t *testing.T) {
	w, apps := navigatorApplications(t)
	files := apps.surfaces[0]
	apps.reuse = map[string]string{"files": files.Key}
	command(t, w, Action{Kind: CreateSpace, SpaceName: "Code"})
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: files.Key})
	command(t, w, Action{Kind: MoveToSpace, Space: 1})
	before := w.m.applicationState.Layouts[w.m.applicationState.index(files.Key)]
	w.Draw(w.width, w.height)
	navigatorFocus(t, w, apps.surfaces[1].Key)
	if w.m.applicationState.Space != 0 {
		t.Fatal("fixture did not open another project")
	}
	navigatorClick(t, w, w.navigation.layout.tools)
	w.Update(200 * time.Millisecond)
	for _, tool := range w.navigation.layout.toolItems {
		if tool.id == "launch:files" {
			navigatorClick(t, w, tool.bounds)
			break
		}
	}
	if len(apps.launched) != 1 || apps.launched[0] != "files" || len(apps.surfaces) != 3 {
		t.Fatal("tool action did not reuse the existing Files application")
	}
	if after := w.m.applicationState.Layouts[w.m.applicationState.index(files.Key)]; after != before {
		t.Fatalf("reopening Files relocated its saved placement: before=%+v after=%+v", before, after)
	}
	if w.applicationFocusedID != files.ID || w.m.applicationState.Space != 1 || w.navigation.level != navigationApp {
		t.Fatal("existing Files did not focus in its original project")
	}
}
