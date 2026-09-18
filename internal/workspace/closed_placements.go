package workspace

import "fmt"

var forgetClosedPlacementsButton = box{1110, 100, 288, 25}

// Consult the complete provider list, including live surfaces excluded by the
// layout limit or waiting for an image. Hidden is not the same as closed.
func (w *Workspace) liveApplicationKeys() map[string]bool {
	live := make(map[string]bool)
	if w.applications != nil {
		for _, surface := range w.applications.Surfaces() {
			key := surface.Key
			if key == "" {
				key = w.applicationKeys[surface.ID]
			}
			if key != "" {
				live[key] = true
			}
		}
	}
	return live
}

func (w *Workspace) closedPlacementMask() uint32 {
	live := w.liveApplicationKeys()
	var closed uint32
	for i, placement := range w.m.applicationState.Layouts {
		if placement.Key != "" && !live[placement.Key] {
			closed |= 1 << i
		}
	}
	return closed
}

func (v *ApplicationViewState) repairActive() {
	if i := v.index(v.Active); i >= 0 && v.Layouts[i].Space == v.Space {
		return
	}
	v.Active = ""
	for i, placement := range v.Layouts {
		if placement.Key != "" && placement.Space == v.Space && (v.Active == "" || v.Selected&(1<<i) != 0) {
			v.Active = placement.Key
			if v.Selected&(1<<i) != 0 {
				break
			}
		}
	}
}

func (w *Workspace) forgetClosedPlacements() error {
	w.cancelPointer()
	mask := w.closedPlacementMask()
	if mask == 0 {
		return nil
	}
	before := w.Document()
	after, err := reduce(before, Action{Kind: ForgetClosedPlacements, ClosedPlacements: mask})
	if err != nil {
		return err
	}
	w.install(after, false)
	w.record(before, after, allFields)
	w.history[len(w.history)-1].forgetClosed = true
	if w.m.applicationState.index(w.applicationRestoreKey) < 0 {
		w.applicationRestoreKey, w.applicationRestoreSelection = "", 0
	}
	w.applicationNotice, w.applicationNoticeRemaining = "", 0
	// Register any waiting live windows only after the user's removal edit.
	// Undo must retain these independently registered placements.
	w.syncApplications()
	return nil
}

func (w *Workspace) restoreEdit(entry edit, undo bool) (Document, error) {
	source := entry.after
	if undo {
		source = entry.before
	}
	if !entry.forgetClosed {
		next := w.Document()
		mask := entry.fields
		if mask&fieldApplicationLayout != 0 {
			next.View.Application = restoreApplicationHistory(next.View.Application, entry.before.View.Application, entry.after.View.Application, undo)
			mask &^= fieldApplicationLayout | fieldApplicationDepth | fieldApplicationReading | fieldApplicationSize
		}
		next = merge(next, source, mask)
		return next, next.Validate()
	}
	next := w.Document()
	v := &next.View.Application
	live := w.liveApplicationKeys()
	for original, placement := range entry.before.View.Application.Layouts {
		if placement.Key == "" || entry.after.View.Application.index(placement.Key) >= 0 {
			continue
		}
		index := v.index(placement.Key)
		if undo {
			if index >= 0 {
				continue // Reopened keys retain their current position and selection.
			}
			index = original
			if v.Layouts[index].Key != "" {
				index = -1
				for i, slot := range v.Layouts {
					if slot.Key == "" {
						index = i
						break
					}
				}
			}
			if index < 0 {
				err := fmt.Errorf("cannot undo: restoring saved placements would exceed the 32-window limit")
				w.showApplicationNotice(err.Error())
				return Document{}, err
			}
			v.Layouts[index] = placement
			if entry.before.View.Application.Selected&(1<<original) != 0 {
				v.Selected |= 1 << index
			}
		} else if index >= 0 && !live[placement.Key] {
			v.Layouts[index] = ApplicationPlacement{}
			v.Selected &^= 1 << index
		}
	}
	if undo && v.Active == "" {
		v.Active = entry.before.View.Application.Active
	}
	v.repairActive()
	v.aliases()
	return next, next.Validate()
}

func (w *Workspace) drawApplicationNotice() {
	closed := w.closedPlacementMask() != 0
	width := float32(1135)
	if closed {
		width = forgetClosedPlacementsButton.x - 253 - 12
		b := forgetClosedPlacementsButton
		w.rect(b.x, b.y, b.w, b.h, teal, .10)
		w.text(b.x+12, b.y+6, 11, "FORGET CLOSED PLACEMENTS", teal, 1)
	}
	notice := w.applicationNotice
	if notice == "" && w.applicationLayoutFull {
		notice = "Workspace limit reached: at most 32 application windows or saved placements."
	}
	if notice == "" {
		return
	}
	// Error details share the row with the explicit cleanup control. Fit actual
	// glyph widths so even wide characters cannot cover its label or hit target.
	text := []rune(notice)
	if w.canvas.MeasureText(12, notice) > width-26 {
		for len(text) > 0 && w.canvas.MeasureText(12, string(text)+"…") > width-26 {
			text = text[:len(text)-1]
		}
		notice = string(text) + "…"
	}
	w.rect(253, 100, width, 25, amber, .12)
	w.text(266, 106, 12, notice, amber, 1)
}
