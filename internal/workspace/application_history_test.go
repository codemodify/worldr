package workspace

import "testing"

func TestApplicationSelectionHistoryPreservesIndependentMovement(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	a, b := apps.surfaces[0].Key, apps.surfaces[1].Key
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: b})
	// Model A's independent coast after the user selected B. This movement
	// has not yet become its own completed history entry.
	ai := w.m.applicationState.index(a)
	w.m.applicationState.Layouts[ai].X += 3
	w.m.applicationState.Layouts[ai].Y -= .8
	w.m.applicationState.Layouts[ai].Depth = -.7
	coasted := w.m.applicationState.Layouts[ai]
	for _, action := range []ActionKind{Undo, Redo} {
		command(t, w, Action{Kind: action})
		v := w.m.applicationState
		active := a
		if action == Redo {
			active = b
		}
		if v.Active != active || v.Selected != 1<<v.index(active) {
			t.Fatalf("%s failed to restore application selection by key", action)
		}
		if v.Layouts[v.index(a)] != coasted {
			t.Fatalf("%s of selecting B rewound A's unrelated movement", action)
		}
		if v.Behind != (v.Layouts[v.index(active)].Depth < 0) {
			t.Fatal("selection history restored a stale depth alias")
		}
	}
}

func TestApplicationMoveHistoryRestoresOnlyChangedPlacementFields(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	a, b := apps.surfaces[0].Key, apps.surfaces[1].Key
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: b})
	ai, bi := w.m.applicationState.index(a), w.m.applicationState.index(b)
	before := w.m.applicationState.Layouts[bi]
	command(t, w, Action{Kind: MoveApplications, DeltaX: 1.5, DeltaDepth: .6})
	after := w.m.applicationState.Layouts[bi]
	w.m.applicationState.Layouts[ai].X += 2
	w.m.applicationState.Layouts[ai].Wide = true
	w.m.applicationState.Layouts[bi].Y += 1.2
	coasted, independentY := w.m.applicationState.Layouts[ai], w.m.applicationState.Layouts[bi].Y
	for _, action := range []ActionKind{Undo, Redo} {
		command(t, w, Action{Kind: action})
		v, target := w.m.applicationState, before
		if action == Redo {
			target = after
		}
		p := v.Layouts[v.index(b)]
		if p.X != target.X || p.Depth != target.Depth || p.Y != independentY || v.Layouts[v.index(a)] != coasted {
			t.Fatalf("%s changed fields outside B's recorded X/depth movement", action)
		}
	}
}

func TestApplicationHistoryUsesStableKeysAcrossSlotChanges(t *testing.T) {
	before := ApplicationViewState{Active: "a", Selected: 1}
	before.Layouts[0] = ApplicationPlacement{Key: "a", X: 1, Depth: -2}
	before.Layouts[1] = ApplicationPlacement{Key: "b", X: 2, Y: 3}
	before.aliases()
	after := before
	after.Active, after.Selected = "b", 1<<1
	after.Layouts[1].X, after.Layouts[1].Wide, after.Layouts[1].Group = 8, true, 7
	after.aliases()
	current := ApplicationViewState{Active: "b", Selected: 1<<5 | 1, Overview: true}
	current.Layouts[7], current.Layouts[5] = after.Layouts[0], after.Layouts[1]
	current.Layouts[7].Y = 12
	current.Layouts[5].Y = 9
	current.Layouts[0] = ApplicationPlacement{Key: "new", X: 17, Y: 4, Wide: true, Group: 3}
	registered := current.Layouts[0]
	current.aliases()
	undone := restoreApplicationHistory(current, before, after, true)
	if undone.Active != "a" || undone.Selected != 1<<7|1 || !undone.Overview {
		t.Fatal("history confused selection slot numbers or replaced an unrelated view mode")
	}
	if undone.Layouts[5].X != 2 || undone.Layouts[5].Y != 9 || undone.Layouts[5].Wide || undone.Layouts[5].Group != 0 || undone.Layouts[7].Y != 12 || undone.Layouts[0] != registered {
		t.Fatal("history did not preserve current slots and independent placement fields")
	}
	if err := undone.validate(); err != nil {
		t.Fatal(err)
	}
	redone := restoreApplicationHistory(undone, before, after, false)
	if redone != current {
		t.Fatal("stable-key redo failed to recover the edit without replacing later state")
	}
}

func TestApplicationHistoryDoesNotResurrectOrRemoveRegisteredKeys(t *testing.T) {
	before := ApplicationViewState{Active: "a", Selected: 1}
	before.Layouts[0] = ApplicationPlacement{Key: "a", X: 2}
	after := before
	after.Layouts[0].X = 4
	after.Layouts[1] = ApplicationPlacement{Key: "registered-during-edit", X: 8}
	current := after
	current.Layouts[1].X = 10
	current.Selected |= 1 << 1
	undone := restoreApplicationHistory(current, before, after, true)
	if undone.Layouts[0].X != 2 || undone.Layouts[1] != current.Layouts[1] || undone.Selected != current.Selected {
		t.Fatal("undo removed or rewound a placement registered during the edit")
	}
	// A missing historic key must not reclaim an occupied slot belonging to
	// a different live registration. Explicit cleanup owns resurrection.
	current.Layouts[0] = ApplicationPlacement{Key: "replacement", X: 19}
	current.Active = "replacement"
	undone = restoreApplicationHistory(current, before, after, true)
	if undone != current {
		t.Fatal("ordinary history resurrected a missing key or changed its replacement")
	}
}
