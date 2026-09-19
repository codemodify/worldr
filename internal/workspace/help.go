package workspace

import "github.com/codemodify/worldr/internal/experience"

var helpButton = box{950, 871, 132, 24}
var helpCloseButton = box{1038, 164, 124, 35}

type helpKey struct {
	code uint32
	key  experience.Key
}

func helpStroke(event experience.Event) helpKey {
	if event.Keycode != 0 {
		return helpKey{code: event.Keycode}
	}
	return helpKey{key: event.Key}
}

type helpPointerCapture struct {
	active, dragged bool
	target          box
	x, y            float32
	width, height   int
}

func (w *Workspace) cancelHelpPointer() {
	w.helpPointer = helpPointerCapture{}
}

func (w *Workspace) claimHelpStrokes() {
	// Cancelled app/workspace strokes must not resume on another target.
	// False map entries track ordinary held inputs; true entries are drained
	// by help even when the opening click is cancelled or the modal closes.
	for key := range w.helpKeys {
		w.helpKeys[key] = true
	}
	for button := range w.helpButtons {
		w.helpButtons[button] = true
	}
}

func (w *Workspace) openHelp() {
	w.finishWindowThrow()
	w.cancelPointer()
	w.clearApplicationFocus()
	w.cancelHelpPointer()
	w.claimHelpStrokes()
	w.portals.open = false
	w.helpOpen = true
}

// Help owns its complete input strokes. Dismissing it never replays input or
// grants application typing focus; keys/buttons held through dismissal drain
// before they can reach the workspace or a newly focused application.
func (w *Workspace) handleHelp(event experience.Event) bool {
	if event.Kind == experience.KeyboardCancel {
		w.helpKeys = nil
		w.helpButtons = nil
		w.helpPointer = helpPointerCapture{}
		// The normal cancellation path still releases application/workspace state.
		return false
	}
	if event.Kind == experience.PointerCancel {
		w.helpButtons = nil
		w.helpPointer = helpPointerCapture{}
		return false
	}
	if event.Kind == experience.KeyInput {
		stroke := helpStroke(event)
		owned := w.helpKeys[stroke]
		if !event.Pressed && (owned || w.helpOpen) {
			// These commands may have begun before help opened. Release their
			// suppression too, without admitting a new command through the modal.
			w.handleHeldOverviewKey(event)
			w.handleTerminalShortcut(event)
			w.handleOverviewShortcut(event)
		}
		if owned {
			if !event.Pressed {
				delete(w.helpKeys, stroke)
			}
			return true
		}
		if event.Pressed {
			if w.helpKeys == nil {
				w.helpKeys = make(map[helpKey]bool)
			}
			w.helpKeys[stroke] = false
		} else {
			delete(w.helpKeys, stroke)
		}
		f1 := event.Key == experience.KeyF1 || event.Keycode == 59
		escape := event.Key == experience.KeyEscape || event.Keycode == 1
		if w.helpOpen || w.helpPointer.active || f1 && !w.applicationKeyboard && event.Modifiers == 0 {
			if event.Pressed {
				w.helpKeys[stroke] = true
				if !event.Repeat && event.Modifiers == 0 {
					switch {
					case w.helpOpen && (f1 || escape):
						w.helpOpen = false
						w.cancelHelpPointer()
					case !w.helpOpen && f1:
						w.openHelp()
					case escape:
						w.cancelHelpPointer()
					}
				}
			}
			return true
		}
	}

	x, y := (event.X-w.ox)/w.scale, (event.Y-w.oy)/w.scale
	button := applicationButton(event)
	ownedButton := w.helpButtons[button]
	draining := false
	for _, owned := range w.helpButtons {
		draining = draining || owned
	}
	if event.Kind == experience.PointerDown && button != 0 {
		if w.helpButtons == nil {
			w.helpButtons = make(map[uint32]bool)
		}
		w.helpButtons[button] = w.helpOpen || w.helpPointer.active || draining
	}
	if event.Kind == experience.PointerUp {
		delete(w.helpButtons, button)
	}
	if w.helpPointer.active {
		if w.width != w.helpPointer.width || w.height != w.helpPointer.height {
			w.cancelHelpPointer()
			return true
		}
		if event.Kind == experience.PointerMove || event.Kind == experience.PointerUp {
			if abs(x-w.helpPointer.x)+abs(y-w.helpPointer.y) > 4 {
				w.helpPointer.dragged = true
			}
			if event.Kind == experience.PointerUp && button == 272 {
				activate := !w.helpPointer.dragged && w.helpPointer.target.contains(x, y)
				w.cancelHelpPointer()
				if activate {
					if w.helpOpen {
						w.helpOpen = false
					} else {
						w.openHelp()
					}
				}
			}
		}
		return true
	}
	if ownedButton && (event.Kind == experience.PointerDown || event.Kind == experience.PointerUp) ||
		draining && (event.Kind == experience.PointerDown || event.Kind == experience.PointerMove || event.Kind == experience.PointerScroll) {
		return true
	}
	if event.Kind == experience.PointerDown && button == 272 {
		target := helpButton
		if w.helpOpen {
			target = helpCloseButton
		}
		if target.contains(x, y) {
			w.cancelPointer()
			w.clearApplicationFocus()
			w.helpButtons[button] = true
			w.claimHelpStrokes()
			w.helpPointer = helpPointerCapture{active: true, target: target, x: x, y: y, width: w.width, height: w.height}
			return true
		}
	}
	return w.helpOpen
}

func (w *Workspace) drawHelpButton() {
	w.line(helpButton.x, helpButton.y+helpButton.h, helpButton.x+helpButton.w, helpButton.y+helpButton.h, 1, muted, .25)
	label := "HELP  /  F1"
	if w.applicationKeyboard {
		label = "WORKSPACE HELP"
	}
	w.text(helpButton.x+8, helpButton.y+5, 10, label, teal, .9)
}

type helpEntry struct{ control, description string }

func (w *Workspace) drawHelp() {
	if !w.helpOpen {
		return
	}
	w.rect(0, 0, 1440, 900, bg, .88)
	w.rect(250, 140, 940, 622, 0x101e2d, 1)
	w.line(250, 140, 1190, 140, 2, teal, .75)
	w.text(282, 168, 26, "YOUR WORKSPACE", ink, 1)
	w.text(283, 210, 13, "A quick guide to moving through space and getting work done.", muted, 1)
	w.button(helpCloseButton, "CLOSE  ESC", true)
	columns := []struct {
		x     float32
		title string
		rows  []helpEntry
	}{
		{282, "SPACE AND PRESENTATION", []helpEntry{
			{"Drag scene / scroll scene", "Orbit / zoom; app scrolling stays with the app."},
			{"P / Shift+P", "Switch presentation / pause ambient motion and throws."},
			{"1–3 / E / F", "Inspect a part / explode assembly / focus view."},
			{"Space / left and right arrows", "Play or pause / step the study timeline."},
			{"Ctrl+Z / Ctrl+Shift+Z", "Undo / redo workspace edits."},
			{"Escape / R", "Cancel a gesture or reset view / reset study."},
			{"Ctrl+S", "Save when started with a document path."},
			{"Ctrl+Alt+Q", "Quit worldr, including from a focused app."},
		}},
		{742, "APPLICATIONS AND NATIVE TERMINALS", []helpEntry{
			{"Ctrl+Alt+Enter / New Terminal", "Open an independent native shell."},
			{"Ctrl+Alt+O / Overview", "Find windows, including those behind objects."},
			{"Overview: arrows, then Enter", "Select a window and return to its workspace."},
			{"Fresh Enter / click application", "Start typing; workspace Enter also opens Read."},
			{"Top grip / Super+drag", "Move or throw windows; grab again to stop. Scroll for depth."},
			{"Place / Group", "Shift+click selects several; group to move together."},
			{"Native terminal: Ctrl+Shift+C / V", "Copy selected text / paste from the clipboard."},
			{"Ctrl+Alt+G / Ctrl+Alt+← →", "Open portals / travel directly between window groups."},
		}},
	}
	if w.desktop {
		columns[0].rows[0] = helpEntry{"Super+drag space / rotation pad", "Pan the workspace / orbit from the bottom-right pad."}
		columns[0].rows[2] = helpEntry{"Read Selected / F", "Enlarge the active window, then return to space."}
		columns[0].rows[3] = helpEntry{"Super+wheel / depth controls", "Move the hovered / selected windows through depth."}
		columns[0].rows[5] = helpEntry{"Escape / R", "Cancel a gesture / reset the camera view."}
		columns[1].rows[4] = helpEntry{"Top grip / Super+primary", "Move or throw a window; grab it again to stop."}
		columns[1].rows[5] = helpEntry{"Corner grip / Super+secondary", "Resize a window freely; Escape cancels the gesture."}
		columns[1].rows[6] = helpEntry{"Right APPS rail / Ctrl+Alt+Space", "Launch tools directly / search every tool and window."}
		columns[1].rows = append(columns[1].rows, helpEntry{"Top-grip  −  □  ×", "Minimize to a strip / maximize or restore / close."})
	}
	for _, column := range columns {
		w.text(column.x, 256, 11, column.title, muted, 1)
		for i, row := range column.rows {
			y := float32(286 + i*46)
			w.text(column.x, y, 13, row.control, teal, 1)
			w.text(column.x, y+20, 11, row.description, ink, .9)
		}
	}
	w.line(282, 683, 1158, 683, 1, muted, .25)
	w.text(282, 703, 12, "Ordinary shortcuts belong to the focused app. Click workspace controls to return.", ink, 1)
	w.text(282, 730, 11, "Closing this guide leaves typing focus with the workspace.", muted, 1)
}
