package workspace

import (
	"math"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
)

const (
	overviewLeft uint8 = 1 << iota
	overviewRight
	overviewUp
	overviewDown
	overviewEnter
	overviewKeypadEnter
)

// Layout and keyboard movement use the same live count and viewport geometry.
func applicationOverviewGrid(count int, viewport scene.Viewport) (columns, rows int) {
	if count == 0 {
		return 1, 0
	}
	aspect := float32(1)
	if viewport.Width > 0 && viewport.Height > 0 {
		aspect = viewport.Width / viewport.Height
	}
	columns = min(count, max(1, int(math.Ceil(math.Sqrt(float64(count)*float64(aspect)/1.4)))))
	return columns, (count + columns - 1) / columns
}

func overviewNavigationKey(event experience.Event) uint8 {
	switch event.Keycode {
	case 105:
		return overviewLeft
	case 106:
		return overviewRight
	case 103:
		return overviewUp
	case 108:
		return overviewDown
	case 28:
		return overviewEnter
	case 96:
		return overviewKeypadEnter
	case 0:
		// Native command sources may supply only a normalized semantic key.
		switch event.Key {
		case experience.KeyLeft:
			return overviewLeft
		case experience.KeyRight:
			return overviewRight
		}
	}
	return 0
}

// A stroke that began as a workspace command stays owned until release, even
// after leaving overview, changing modifiers or explicitly focusing a window.
// This check precedes other shortcuts so a held Enter cannot become a launcher.
func (w *Workspace) handleHeldOverviewKey(event experience.Event) bool {
	if event.Kind == experience.KeyboardCancel {
		w.overviewNavigationKeys = 0
	}
	if event.Kind != experience.KeyInput {
		return false
	}
	bit := overviewNavigationKey(event)
	if w.overviewNavigationKeys&bit == 0 {
		return false
	}
	if !event.Pressed {
		w.overviewNavigationKeys &^= bit
	}
	return true
}

func (w *Workspace) handleOverviewNavigation(event experience.Event) bool {
	if event.Kind != experience.KeyInput {
		return false
	}
	bit := overviewNavigationKey(event)
	if bit == 0 {
		return false
	}
	enter := bit == overviewEnter || bit == overviewKeypadEnter
	if !w.m.applicationState.Overview && (!enter || w.applicationKeyboard || w.application.ID == 0 || event.Modifiers != 0) {
		return false
	}
	if !event.Pressed {
		return w.m.applicationState.Overview
	}
	w.overviewNavigationKeys |= bit
	// Modified overview navigation is intentionally inert. Duplicate presses
	// and repeats are consumed until release; each fresh press makes one move.
	if event.Repeat || event.Modifiers != 0 {
		return true
	}
	w.cancelPointer()
	if enter {
		if w.m.applicationState.Overview {
			_ = w.Dispatch(Action{Kind: ToggleApplicationOverview})
		} else {
			w.readAndFocusSelectedApplication()
		}
		return true
	}
	surfaces := w.visibleApplications()
	count := len(surfaces)
	if count == 0 {
		return true
	}
	current := 0
	for i, surface := range surfaces {
		if surface.Key == w.m.applicationState.Active {
			current = i
			break
		}
	}
	w.layout(w.width, w.height)
	columns, rows := applicationOverviewGrid(count, w.viewport)
	next := current
	switch bit {
	case overviewLeft:
		if current%columns > 0 {
			next--
		}
	case overviewRight:
		if current%columns < columns-1 && current+1 < count {
			next++
		}
	case overviewUp:
		if current >= columns {
			next -= columns
		}
	case overviewDown:
		if current/columns < rows-1 {
			next = min(current+columns, count-1)
		}
	}
	if next != current {
		_ = w.Dispatch(Action{Kind: SelectApplication, ApplicationKey: surfaces[next].Key})
	}
	return true
}

// A fresh workspace Enter explicitly requests Read and, for interactive apps,
// typing. Passive draggable content keeps keyboard shortcuts in the workspace.
func (w *Workspace) readAndFocusSelectedApplication() {
	w.clearApplicationFocus()
	if !w.m.applicationReading {
		_ = w.Dispatch(Action{Kind: ToggleApplicationReading})
	}
	w.applicationRestoreKey = ""
	if w.application.DragContent {
		return
	}
	w.applications.Focus(w.application.ID)
	w.applicationFocusedID = w.application.ID
	w.applicationKeyboard = true
}
