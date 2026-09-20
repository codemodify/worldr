package workspace

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
)

func settingsPoint(w *Workspace, target box) (float32, float32) {
	return w.ox + (target.x+target.w/2)*w.scale, w.oy + (target.y+target.h/2)*w.scale
}

func copiedVertices(frame render.Frame) []render.Vertex {
	return append([]render.Vertex(nil), frame.Vertices...)
}

func TestSceneControllerGearOpensSettingsOnCompletedClick(t *testing.T) {
	for _, dimensions := range []struct{ width, height int }{{1440, 900}, {2880, 1800}} {
		w := desktop(t)
		w.Draw(dimensions.width, dimensions.height)
		before, history, historyLength := w.Document(), w.historyPosition, len(w.history)
		x, y := settingsPoint(w, orbitSettingsButton)
		if !pointer(w, experience.PointerDown, x, y) || !w.orbitControl.active || w.settingsOpen {
			t.Fatal("scaled gear press did not acquire its completed-click gesture")
		}
		if !pointer(w, experience.PointerUp, x, y) || !w.settingsOpen || w.orbitControl.active {
			t.Fatal("completed gear click did not open Settings")
		}
		if w.Document() != before || w.historyPosition != history || len(w.history) != historyLength {
			t.Fatal("opening Settings from the scene controller changed workspace state")
		}
	}
}

func TestSettingsIsTransientAndBlocksUnderlyingApplicationInput(t *testing.T) {
	w, apps := headerDesktopApplications(t, 1)
	w.Draw(1440, 900)
	x, y := visibleApplication(t, w, apps.surfaces[0])
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if !w.applicationKeyboard {
		t.Fatal("fixture did not focus the application")
	}
	before, history, historyLength := w.Document(), w.historyPosition, len(w.history)
	w.openSettings()
	if !w.settingsOpen || !w.OwnsKeyboard() || w.applicationKeyboard || w.applicationFocusedID != 0 || w.applicationHoveredID != 0 || w.applicationCapturedID != 0 {
		t.Fatal("Settings did not take modal ownership and clear application focus")
	}
	if len(apps.focus) == 0 || apps.focus[len(apps.focus)-1] != 0 {
		t.Fatal("opening Settings did not release provider keyboard focus")
	}
	if w.Document() != before || w.historyPosition != history || len(w.history) != historyLength {
		t.Fatal("opening Settings changed the document or edit history")
	}
	apps.events = nil
	for _, event := range []experience.Event{
		{Kind: experience.PointerMove, X: x, Y: y},
		{Kind: experience.PointerDown, X: x, Y: y, Button: experience.ButtonPrimary},
		{Kind: experience.PointerMove, X: x + 24, Y: y + 12},
		{Kind: experience.PointerUp, X: x + 24, Y: y + 12, Button: experience.ButtonPrimary},
		{Kind: experience.PointerScroll, X: x, Y: y, ScrollY: 8},
		{Kind: experience.KeyInput, Key: experience.KeyE, Keycode: 18, Pressed: true},
		{Kind: experience.KeyInput, Key: experience.KeyE, Keycode: 18},
		{Kind: experience.KeyInput, Key: experience.KeyF1, Keycode: 59, Pressed: true},
		{Kind: experience.KeyInput, Key: experience.KeyF1, Keycode: 59},
		{Kind: experience.KeyInput, Key: experience.KeySpace, Keycode: 57, Modifiers: experience.ModControl | experience.ModAlt, Pressed: true},
		{Kind: experience.KeyInput, Key: experience.KeySpace, Keycode: 57},
	} {
		if !w.Handle(event) {
			t.Fatalf("Settings did not consume modal event %+v", event)
		}
	}
	if len(apps.events) != 0 || w.helpOpen || w.commands != nil && w.commands.open || w.Document() != before || w.historyPosition != history || len(w.history) != historyLength {
		t.Fatal("Settings leaked input to an application, command surface, or document")
	}
	if _, ok := w.Cursor(); ok {
		t.Fatal("Settings inherited the underlying application's cursor")
	}
}

func TestSettingsCategoriesScaleAndRenderDistinctPreviews(t *testing.T) {
	for _, dimensions := range []struct{ width, height int }{{1440, 900}, {2880, 1800}} {
		w := desktop(t)
		w.Draw(dimensions.width, dimensions.height)
		before, history, historyLength := w.Document(), w.historyPosition, len(w.history)
		w.openSettings()
		terminal := copiedVertices(w.Draw(dimensions.width, dimensions.height))
		if w.settingsCategory != settingsTerminal || len(terminal) == 0 {
			t.Fatal("Settings did not start with the Terminal preview")
		}
		x, y := settingsPoint(w, settingsMediaButton)
		if !pointer(w, experience.PointerDown, x, y) || !pointer(w, experience.PointerUp, x, y) || w.settingsCategory != settingsMedia {
			t.Fatal("scaled Media category click did not select its preview")
		}
		media := copiedVertices(w.Draw(dimensions.width, dimensions.height))
		if reflect.DeepEqual(terminal, media) {
			t.Fatal("Terminal and Media categories rendered the same preview")
		}
		x, y = settingsPoint(w, settingsWindowsButton)
		if !pointer(w, experience.PointerDown, x, y) || !pointer(w, experience.PointerUp, x, y) || w.settingsCategory != settingsWindows {
			t.Fatal("scaled Windows category click did not select its controls")
		}
		windows := copiedVertices(w.Draw(dimensions.width, dimensions.height))
		if reflect.DeepEqual(terminal, windows) || reflect.DeepEqual(media, windows) {
			t.Fatal("Windows category did not render a distinct border chooser")
		}
		x, y = settingsPoint(w, settingsEnvironmentButton)
		if !pointer(w, experience.PointerDown, x, y) || !pointer(w, experience.PointerUp, x, y) || w.settingsCategory != settingsEnvironment {
			t.Fatal("scaled Environment category click did not select its controls")
		}
		environment := copiedVertices(w.Draw(dimensions.width, dimensions.height))
		if reflect.DeepEqual(media, environment) || reflect.DeepEqual(terminal, environment) || reflect.DeepEqual(windows, environment) {
			t.Fatal("Environment category did not render distinct controls")
		}
		x, y = settingsPoint(w, settingsTerminalButton)
		pointer(w, experience.PointerDown, x, y)
		pointer(w, experience.PointerUp, x, y)
		if w.settingsCategory != settingsTerminal || w.Document() != before || w.historyPosition != history || len(w.history) != historyLength {
			t.Fatal("category navigation changed persistent workspace state")
		}
	}
}

func TestWindowBorderSettingsApplyLiveAndPersistWithoutUndoHistory(t *testing.T) {
	for _, dimensions := range []struct{ width, height int }{{1440, 900}, {2880, 1800}} {
		w := desktop(t)
		w.Draw(dimensions.width, dimensions.height)
		beforeDocument, history, historyLength := w.Document(), w.historyPosition, len(w.history)
		beforeState, err := w.SaveState()
		if err != nil {
			t.Fatal(err)
		}
		w.openSettings()
		x, y := settingsPoint(w, settingsWindowsButton)
		pointer(w, experience.PointerDown, x, y)
		pointer(w, experience.PointerUp, x, y)
		for _, choice := range []struct {
			bounds box
			style  windowBorderStyle
		}{
			{settingsWindowInstrument, windowBorderInstrument},
			{settingsWindowAperture, windowBorderAperture},
			{settingsWindowGlass, windowBorderGlass},
			{settingsWindowTelemetry, windowBorderTelemetry},
		} {
			x, y = settingsPoint(w, choice.bounds)
			if !pointer(w, experience.PointerDown, x, y) || !pointer(w, experience.PointerUp, x, y) || w.windows.Border != choice.style {
				t.Fatalf("border card did not apply %q live", choice.style)
			}
		}
		if w.Document() != beforeDocument || w.historyPosition != history || len(w.history) != historyLength || w.CanUndo() {
			t.Fatal("window border preference entered the workspace document or Undo history")
		}
		afterState, err := w.SaveState()
		if err != nil || bytes.Equal(beforeState, afterState) {
			t.Fatal("window border preference did not change persisted desktop state")
		}
		loaded := desktop(t)
		if err := loaded.LoadState(afterState); err != nil || loaded.windows.Border != windowBorderTelemetry {
			t.Fatalf("window border preference did not round-trip: state=%+v err=%v", loaded.windows, err)
		}
	}
}

func TestWindowBorderSettingsCardUpdatesAttachedWindowChromeOnNextDraw(t *testing.T) {
	w := desktop(t)
	apps := desktopApplications(t, w)
	id := apps.surfaces[0].ID
	frameID := w.applicationFrames[id]
	beforeFrame := w.scene.Node(frameID).Mesh
	beforeGrip := w.scene.Node(w.applicationDragHandles[id]).Mesh
	beforeControls := w.scene.Node(w.applicationWindowControls[id]).Mesh
	beforeResize := w.scene.Node(w.applicationResizeHandles[id]).Mesh

	w.openSettings()
	x, y := settingsPoint(w, settingsWindowsButton)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	x, y = settingsPoint(w, settingsWindowTelemetry)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	w.Draw(1440, 900)

	if w.windows.Border != windowBorderTelemetry || w.scene.Node(frameID).Mesh == beforeFrame || w.scene.Node(frameID).Mesh != w.windowBorderFrameMesh(windowBorderTelemetry) {
		t.Fatal("Telemetry card did not swap the attached window frame on the next draw")
	}
	if w.scene.Node(w.applicationDragHandles[id]).Mesh == beforeGrip || w.scene.Node(w.applicationWindowControls[id]).Mesh == beforeControls || w.scene.Node(w.applicationResizeHandles[id]).Mesh == beforeResize {
		t.Fatal("Telemetry card left Instrument grip, controls, or resize chrome attached")
	}
}

func TestWindowBorderSettingsRequireVisibleCompletedUnmovedClicks(t *testing.T) {
	w := desktop(t)
	w.Draw(1440, 900)
	w.openSettings()
	x, y := settingsPoint(w, settingsWindowsButton)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)

	x, y = settingsPoint(w, settingsWindowAperture)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerMove, x+12, y)
	pointer(w, experience.PointerMove, x, y)
	pointer(w, experience.PointerUp, x, y)
	if w.windows.Border != windowBorderInstrument {
		t.Fatal("dragging away and back activated a window border card")
	}
	x, y = settingsPoint(w, settingsWindowGlass)
	pointer(w, experience.PointerDown, x, y)
	w.Handle(experience.Event{Kind: experience.PointerCancel})
	pointer(w, experience.PointerUp, x, y)
	if w.windows.Border != windowBorderInstrument {
		t.Fatal("a cancelled border press activated on a later release")
	}
	x, y = settingsPoint(w, settingsWindowTelemetry)
	pointer(w, experience.PointerDown, x, y)
	w.Draw(2880, 1800)
	pointer(w, experience.PointerUp, x*2, y*2)
	if w.windows.Border != windowBorderInstrument {
		t.Fatal("a resized Settings press activated a border using stale coordinates")
	}

	w.Draw(1440, 900)
	x, y = settingsPoint(w, settingsTerminalButton)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	x, y = settingsPoint(w, settingsWindowAperture)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if w.windows.Border != windowBorderInstrument {
		t.Fatal("an invisible Windows card activated from another category")
	}
}

func TestEnvironmentSettingsPersistWithoutEnteringUndoHistory(t *testing.T) {
	for _, dimensions := range []struct{ width, height int }{{1440, 900}, {2880, 1800}} {
		w := desktop(t)
		w.Draw(dimensions.width, dimensions.height)
		beforeDocument, history, historyLength := w.Document(), w.historyPosition, len(w.history)
		beforeState, err := w.SaveState()
		if err != nil {
			t.Fatal(err)
		}
		w.openSettings()
		x, y := settingsPoint(w, settingsEnvironmentButton)
		pointer(w, experience.PointerDown, x, y)
		pointer(w, experience.PointerUp, x, y)
		for _, toggle := range []box{settingsDNAToggle, settingsCatToggle, settingsEyesToggle} {
			x, y = settingsPoint(w, toggle)
			if !pointer(w, experience.PointerDown, x, y) || !pointer(w, experience.PointerUp, x, y) {
				t.Fatal("Environment switch did not consume its completed click")
			}
		}
		if w.environment != (environmentSettings{}) {
			t.Fatalf("Environment switches did not disable every layer: %+v", w.environment)
		}
		if w.Document() != beforeDocument || w.historyPosition != history || len(w.history) != historyLength || w.CanUndo() {
			t.Fatal("Environment preferences entered the workspace document or Undo history")
		}
		afterState, err := w.SaveState()
		if err != nil || bytes.Equal(beforeState, afterState) {
			t.Fatal("Environment preferences did not change persisted desktop state")
		}
		loaded := desktop(t)
		if err := loaded.LoadState(afterState); err != nil || loaded.environment != (environmentSettings{}) {
			t.Fatalf("disabled Environment preferences did not round-trip: state=%+v err=%v", loaded.environment, err)
		}
	}
}

func TestEnvironmentSettingsRequireVisibleCompletedUnmovedClicks(t *testing.T) {
	w := desktop(t)
	w.Draw(1440, 900)
	w.openSettings()
	x, y := settingsPoint(w, settingsEnvironmentButton)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)

	x, y = settingsPoint(w, settingsDNAToggle)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerMove, x+12, y)
	pointer(w, experience.PointerMove, x, y)
	pointer(w, experience.PointerUp, x, y)
	if !w.environment.DNA {
		t.Fatal("dragging away and back activated the DNA switch")
	}
	x, y = settingsPoint(w, settingsCatToggle)
	pointer(w, experience.PointerDown, x, y)
	w.Handle(experience.Event{Kind: experience.PointerCancel})
	pointer(w, experience.PointerUp, x, y)
	if !w.environment.Cat {
		t.Fatal("a cancelled press activated the cat switch on a later release")
	}
	x, y = settingsPoint(w, settingsEyesToggle)
	pointer(w, experience.PointerDown, x, y)
	w.Draw(2880, 1800)
	pointer(w, experience.PointerUp, x*2, y*2)
	if !w.environment.Eyes {
		t.Fatal("a resized Settings press activated the eyes switch using stale coordinates")
	}

	w.Draw(1440, 900)
	x, y = settingsPoint(w, settingsTerminalButton)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	x, y = settingsPoint(w, settingsDNAToggle)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if !w.environment.DNA {
		t.Fatal("an invisible Environment switch activated from another category")
	}
}

func TestSettingsControlsRequireCompletedUnmovedClicks(t *testing.T) {
	w := desktop(t)
	w.Draw(1440, 900)
	w.openSettings()
	x, y := settingsPoint(w, settingsCloseButton)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerMove, x+12, y)
	pointer(w, experience.PointerMove, x, y)
	pointer(w, experience.PointerUp, x, y)
	if !w.settingsOpen {
		t.Fatal("dragging away and back activated the Settings close control")
	}
	pointer(w, experience.PointerDown, x, y)
	w.Handle(experience.Event{Kind: experience.PointerCancel})
	pointer(w, experience.PointerUp, x, y)
	if !w.settingsOpen {
		t.Fatal("a cancelled Settings press activated on a later release")
	}
	pointer(w, experience.PointerDown, x, y)
	w.Draw(2880, 1800)
	pointer(w, experience.PointerUp, x*2, y*2)
	if !w.settingsOpen {
		t.Fatal("a resized Settings press activated using stale coordinates")
	}
	w.Draw(1440, 900)
	x, y = settingsPoint(w, settingsCloseButton)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if w.settingsOpen || w.OwnsKeyboard() {
		t.Fatal("a completed close click did not dismiss Settings")
	}
}

func TestSettingsEscapeDrainsItsStrokeAndDoesNotRestoreFocus(t *testing.T) {
	w, apps := headerDesktopApplications(t, 1)
	w.Draw(1440, 900)
	x, y := visibleApplication(t, w, apps.surfaces[0])
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	w.openSettings()
	before, history := w.Document(), w.historyPosition
	escape := experience.Event{Kind: experience.KeyInput, Key: experience.KeyEscape, Keycode: 1, Pressed: true}
	if !w.Handle(escape) || w.settingsOpen {
		t.Fatal("Escape did not dismiss Settings")
	}
	escape.Repeat = true
	if !w.Handle(escape) {
		t.Fatal("held Escape escaped after dismissing Settings")
	}
	escape.Pressed, escape.Repeat = false, false
	if !w.Handle(escape) {
		t.Fatal("Escape release escaped after dismissing Settings")
	}
	if w.OwnsKeyboard() || w.applicationFocusedID != 0 || w.Document() != before || w.historyPosition != history {
		t.Fatal("dismissing Settings restored application focus or changed workspace state")
	}
	if w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyE, Keycode: 18, Pressed: true}) {
		t.Fatal("fresh desktop input remained trapped after the Settings stroke completed")
	}
}

func TestSettingsReleasesPreviouslyReservedCommandStrokes(t *testing.T) {
	for _, kind := range []string{"overview-enter", "terminal-enter", "overview-chord"} {
		t.Run(kind, func(t *testing.T) {
			w, apps := headerDesktopApplications(t, 1)
			launcher := terminalLauncher(t, apps)
			w.SetApplications(launcher)
			held := overviewKeyEvent(28, true)
			switch kind {
			case "overview-enter":
				command(t, w, Action{Kind: ToggleApplicationOverview})
			case "terminal-enter":
				held = terminalChord(28)
			case "overview-chord":
				held = helpKeyEvent(24, experience.KeyO, true)
				held.Modifiers = experience.ModControl | experience.ModAlt
			}
			w.Handle(held)
			before, launches := w.Document(), len(launcher.launched)
			w.openSettings()
			w.closeSettings()
			held.Repeat, held.Modifiers = true, 0
			if !w.Handle(held) || w.OwnsKeyboard() || w.Document() != before || len(launcher.launched) != launches {
				t.Fatal("previously reserved command resumed after Settings")
			}
			held.Pressed, held.Repeat = false, false
			if !w.Handle(held) || w.overviewNavigationKeys != 0 || w.terminalShortcutKeys != 0 || w.overviewShortcutHeld {
				t.Fatal("Settings release did not drain the earlier command tracker")
			}
			if kind == "overview-chord" {
				tapOverviewKey(t, w, 28)
			}
			tapOverviewKey(t, w, 28)
			if !w.OwnsKeyboard() {
				t.Fatal("fresh focus command stayed trapped by a stale shortcut tracker")
			}
		})
	}
}

func TestSettingsPreservesLatestCommandKeymap(t *testing.T) {
	w := desktop(t)
	w.openSettings()
	event := experience.Event{Kind: experience.KeymapChanged, Keymap: "fixture keymap"}
	if !w.Handle(event) || w.commandKeymap != event {
		t.Fatal("Settings swallowed the command palette's latest keymap")
	}
}
