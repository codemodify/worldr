package workspace

import (
	"strings"
	"time"
	"unicode"

	"github.com/codemodify/worldr/internal/experience"
)

// The global terminal chord is handled before application keyboard routing.
// Retain each physical Enter until release so modifier changes or a new focus
// cannot send a repeat or unmatched key-up into an application.
func (w *Workspace) handleTerminalShortcut(event experience.Event) bool {
	if event.Kind == experience.KeyboardCancel {
		w.terminalShortcutKeys = 0
	}
	if event.Kind != experience.KeyInput {
		return false
	}
	var bit uint8
	switch event.Keycode {
	case 28: // Linux evdev KEY_ENTER.
		bit = 1
	case 96: // Linux evdev KEY_KPENTER.
		bit = 2
	default:
		return false
	}
	if w.terminalShortcutKeys&bit != 0 {
		if !event.Pressed {
			w.terminalShortcutKeys &^= bit
		}
		return true
	}
	if !event.Pressed || event.Modifiers != experience.ModControl|experience.ModAlt {
		return false
	}
	launcher, supported := w.applications.(experience.ApplicationLauncher)
	if !supported {
		return false
	}
	w.terminalShortcutKeys |= bit
	if !event.Repeat {
		w.cancelPointer()
		w.launchTerminal(launcher)
	}
	return true
}

// Launching and closing are live application requests, not undoable document
// edits. Keep a closing surface until its provider withdraws it: a client may
// ask about unsaved work or decline to close. Capture the selected ID at press
// so a changing selection cannot redirect close to a different application.
func (w *Workspace) handleApplicationControl(event experience.Event) bool {
	closer, supported := w.applications.(experience.ApplicationCloser)
	launcher, launchSupported := w.applications.(experience.ApplicationLauncher)
	if w.pointer.kind == captureApplicationClose || w.pointer.kind == captureApplicationLaunch || w.pointer.kind == captureForgetClosedPlacements {
		switch event.Kind {
		case experience.PointerMove:
			w.movePointer(event.X, event.Y)
			return true
		case experience.PointerDown:
			return true
		case experience.PointerUp:
			if applicationButton(event) != 272 {
				return true
			}
			w.movePointer(event.X, event.Y)
			p := w.pointer
			w.pointer = pointerCapture{}
			if p.kind == captureApplicationClose && supported && !p.dragged && applicationCloseButton.contains(p.lastX, p.lastY) && w.application.ID == p.applicationID {
				w.clearApplicationFocus()
				closer.CloseApplication(p.applicationID)
			}
			if p.kind == captureApplicationLaunch && launchSupported && !p.dragged && applicationLaunchButton.contains(p.lastX, p.lastY) {
				w.launchTerminal(launcher)
			}
			if p.kind == captureForgetClosedPlacements && !p.dragged && forgetClosedPlacementsButton.contains(p.lastX, p.lastY) {
				_ = w.Dispatch(Action{Kind: ForgetClosedPlacements})
			}
			return true
		case experience.PointerCancel, experience.KeyboardCancel:
			return w.cancelPointer()
		}
	}
	if event.Kind != experience.PointerDown || applicationButton(event) != 272 {
		return false
	}
	x, y := (event.X-w.ox)/w.scale, (event.Y-w.oy)/w.scale
	kind := captureNone
	if launchSupported && applicationLaunchButton.contains(x, y) {
		kind = captureApplicationLaunch
	} else if supported && w.application.ID != 0 && applicationCloseButton.contains(x, y) {
		kind = captureApplicationClose
	} else if forgetClosedPlacementsButton.contains(x, y) && w.closedPlacementMask() != 0 {
		kind = captureForgetClosedPlacements
	}
	if kind == captureNone {
		return false
	}
	w.clearApplicationFocus()
	w.cancelPointer()
	w.pointer = pointerCapture{
		kind: kind, start: w.Document(), applicationID: w.application.ID,
		pressX: x, pressY: y, lastX: x, lastY: y, scale: w.scale, ox: w.ox, oy: w.oy,
	}
	return true
}

func (w *Workspace) launchTerminal(launcher experience.ApplicationLauncher) {
	w.clearApplicationFocus()
	before := w.Document()
	existing := make(map[uint64]bool)
	for _, surface := range w.applications.Surfaces() {
		existing[surface.ID] = true
	}
	key, err := launcher.LaunchApplication("terminal")
	if err != nil {
		w.showApplicationNotice("Terminal could not start: " + err.Error())
		return
	}
	w.PlaceNewApplication(key)
	next, err := reduce(w.Document(), Action{Kind: SelectApplication, ApplicationKey: key})
	visible := false
	for _, surface := range w.applicationSurfaces {
		visible = visible || surface.Key == key
	}
	if err == nil && visible {
		w.applicationRestoreKey = ""
		w.applicationNotice, w.applicationNoticeRemaining = "", 0
		w.install(next, false)
		return
	}
	// Saved placements can fill every slot even with no live windows. The
	// provider has already launched here, so inspect its full surface list,
	// including the new surface excluded from the workspace by that limit.
	if closer, ok := w.applications.(experience.ApplicationCloser); ok {
		for _, surface := range w.applications.Surfaces() {
			if surface.ID != 0 && surface.Key == key && !existing[surface.ID] {
				closer.CloseApplication(surface.ID)
				break
			}
		}
	}
	w.install(before, false)
	if w.applicationLayoutFull {
		w.showApplicationNotice("Terminal could not open: saved layout has reached its 32-window limit.")
	} else {
		w.showApplicationNotice("Terminal could not open: its window is unavailable.")
	}
}

func (w *Workspace) showApplicationNotice(message string) {
	message = strings.Map(func(r rune) rune {
		if !unicode.IsPrint(r) {
			return ' '
		}
		return r
	}, message)
	text := []rune(strings.Join(strings.Fields(message), " "))
	if len(text) > 120 {
		text = append(text[:119], '…')
	}
	w.applicationNotice = string(text)
	w.applicationNoticeRemaining = 10 * time.Second
}

func (w *Workspace) Notify(message string) { w.showApplicationNotice(message) }

// SavedApplicationKeys lets the host retain temporarily unavailable session
// resources until the user explicitly forgets their saved placements.
func (w *Workspace) SavedApplicationKeys() []string {
	var keys []string
	for _, placement := range w.m.applicationState.Layouts {
		if placement.Key != "" {
			keys = append(keys, placement.Key)
		}
	}
	return keys
}
