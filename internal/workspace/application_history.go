package workspace

// restoreApplicationHistory applies the edit's changed values to the current
// view rather than replacing a saved view wholesale. Windows can move under
// inertia or register new placements while an independent edit is in history.
// Stable keys identify both placement fields and the selection bits that were
// changed; slot numbers belong only to each individual snapshot.
func restoreApplicationHistory(current, before, after ApplicationViewState, undo bool) ApplicationViewState {
	source := after
	if undo {
		source = before
	}
	for index, placement := range current.Layouts {
		if placement.Key == "" {
			continue
		}
		previous, next := before.index(placement.Key), after.index(placement.Key)
		if previous < 0 || next < 0 {
			// Registration and removal are not ordinary placement edits. Keep
			// current keys intact; Forget Closed has its own restoration path.
			continue
		}
		a, b := before.Layouts[previous], after.Layouts[next]
		target := b
		if undo {
			target = a
		}
		p := &current.Layouts[index]
		if a.X != b.X {
			p.X = target.X
		}
		if a.Y != b.Y {
			p.Y = target.Y
		}
		if a.Depth != b.Depth {
			p.Depth = target.Depth
		}
		if a.Wide != b.Wide {
			p.Wide = target.Wide
		}
		if a.Group != b.Group {
			p.Group = target.Group
		}
		if a.Space != b.Space {
			p.Space = target.Space
		}
		selectedBefore, selectedAfter := before.Selected&(1<<previous) != 0, after.Selected&(1<<next) != 0
		if selectedBefore != selectedAfter {
			selected := selectedAfter
			if undo {
				selected = selectedBefore
			}
			if selected {
				current.Selected |= 1 << index
			} else {
				current.Selected &^= 1 << index
			}
		}
	}
	if before.Active != after.Active && (source.Active == "" || current.index(source.Active) >= 0) {
		current.Active = source.Active
	}
	if before.Space != after.Space {
		current.Space = source.Space
	}
	for i := range current.Spaces {
		if before.Spaces[i] != after.Spaces[i] {
			current.Spaces[i] = source.Spaces[i]
		}
	}
	if before.Reading != after.Reading {
		current.Reading = source.Reading
	}
	if before.Overview != after.Overview {
		current.Overview = source.Overview
	}
	if before.Placing != after.Placing {
		current.Placing = source.Placing
	}
	// Behind/Wide are also preferences before the first placement exists.
	// With an active placement they are derived from its restored live fields.
	if before.Behind != after.Behind {
		current.Behind = source.Behind
	}
	if before.Wide != after.Wide {
		current.Wide = source.Wide
	}
	current.aliases()
	return current
}
