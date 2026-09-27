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
	// The provider has already launched here, so inspect its full surface list,
	// including a new surface excluded by the live-window limit.
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
		w.showApplicationNotice("Terminal could not open: workspace already has 32 live or opening windows.")
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
// resources until their slot is needed by a genuinely new live window.
func (w *Workspace) SavedApplicationKeys() []string {
	var keys []string
	for _, placement := range w.m.applicationState.Layouts {
		if placement.Key != "" {
			keys = append(keys, placement.Key)
		}
	}
	return keys
}
