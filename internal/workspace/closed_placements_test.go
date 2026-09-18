package workspace

import (
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

func clickForgetClosed(w *Workspace) {
	b := forgetClosedPlacementsButton
	x, y := w.ox+(b.x+12)*w.scale, w.oy+(b.y+12)*w.scale
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
}

func TestForgetClosedPlacementsRecoversFullOrphanedLayout(t *testing.T) {
	w, apps := multipleApplications(t, MaxApplicationLayouts)
	apps.surfaces = nil
	launcher := &rollbackLaunchingApplications{launchingApplications: terminalLauncher(t, apps)}
	w.SetApplications(launcher)
	before := w.Document()
	if w.closedPlacementMask() != ^uint32(0) || w.application.ID != 0 {
		t.Fatal("test did not retain 32 closed placements without live apps")
	}
	clickForgetClosed(w)
	if w.closedPlacementMask() != 0 || w.m.applicationState.Layouts != (ApplicationLayouts{}) ||
		w.m.applicationState.Active != "" || w.m.applicationState.Selected != 0 || !w.CanUndo() {
		t.Fatal("explicit cleanup did not free orphaned placements and repair selection")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("undo did not restore the complete remembered layout")
	}
	command(t, w, Action{Kind: Redo})
	if w.closedPlacementMask() != 0 {
		t.Fatal("redo re-added intentionally forgotten placements")
	}
	w.launchTerminal(launcher)
	if w.application.ID != launcher.surface.ID || len(launcher.closed) != 0 || w.applicationNotice != "" {
		t.Fatal("new terminal could not use explicitly freed capacity")
	}
	if err := w.Document().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestForgetClosedPlacementsPreservesFullProviderLiveKeys(t *testing.T) {
	w, apps := multipleApplications(t, 4)
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[0].Key})
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[2].Key, Additive: true})
	command(t, w, Action{Kind: GroupApplications})
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[0].Key, Additive: true})
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[0].Key, Additive: true})
	// The second provider surface remains live even while it has no image.
	// It is intentionally absent from the workspace's renderable surface list.
	apps.surfaces[2].Texture = nil
	apps.surfaces = []experience.ApplicationSurface{apps.surfaces[0], apps.surfaces[2]}
	w.Update(0)
	before, history := w.Document(), w.historyPosition
	if len(w.applicationSurfaces) != 1 || w.closedPlacementMask() != (1<<1|1<<3) {
		t.Fatal("cleanup classified an unrenderable but live provider key as closed")
	}
	// Caller-supplied masks cannot bypass the live-provider check in Dispatch.
	command(t, w, Action{Kind: ForgetClosedPlacements, ClosedPlacements: ^uint32(0)})
	v := w.Document().View.Application
	for _, i := range []int{0, 2} {
		if v.Layouts[i] != before.View.Application.Layouts[i] {
			t.Fatal("forget changed a live placement, size or group")
		}
	}
	if v.Layouts[1] != (ApplicationPlacement{}) || v.Layouts[3] != (ApplicationPlacement{}) ||
		v.Active != before.View.Application.Active || v.Selected != before.View.Application.Selected {
		t.Fatal("forget changed live selection or left closed keys")
	}
	if w.historyPosition != history+1 {
		t.Fatal("forget did not record exactly one edit")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("mixed live/closed undo did not restore the original document")
	}
}

func TestForgetKeepsFallbackKeyWhileLiveSurfaceHasNoImage(t *testing.T) {
	w, apps := applicationStudy(t)
	key := w.applicationKeys[777]
	if key == "" || apps.surfaces[0].Key != "" {
		t.Fatal("test requires an assigned fallback key")
	}
	apps.surfaces[0].Texture = nil
	w.Update(0)
	before := w.Document()
	if w.applicationKeys[777] != key || len(w.applicationSurfaces) != 0 || w.closedPlacementMask() != 0 {
		t.Fatal("a live surface without an image lost its fallback key")
	}
	command(t, w, Action{Kind: ForgetClosedPlacements})
	if w.Document() != before || w.CanUndo() {
		t.Fatal("cleanup forgot a key belonging to a live unrenderable surface")
	}
	apps.surfaces = nil
	w.Update(0)
	if w.applicationKeys[777] != "" || w.closedPlacementMask() == 0 {
		t.Fatal("actual withdrawal failed to release the fallback association")
	}
	command(t, w, Action{Kind: ForgetClosedPlacements})
	if w.m.applicationState.index(key) >= 0 {
		t.Fatal("withdrawn fallback placement could not be forgotten")
	}
}

func TestForgetUndoRetainsNewlyRegisteredLiveWindow(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	closedKey := apps.surfaces[1].Key
	closedPlacement := w.m.applicationState.Layouts[1]
	apps.surfaces = apps.surfaces[:1]
	w.Update(0)
	command(t, w, Action{Kind: ForgetClosedPlacements})
	launcher := terminalLauncher(t, apps)
	w.SetApplications(launcher)
	w.launchTerminal(launcher)
	newIndex := w.m.applicationState.index(launcher.surface.Key)
	if newIndex != 1 {
		t.Fatal("new live window did not reuse the freed slot")
	}
	newPlacement := w.m.applicationState.Layouts[newIndex]
	command(t, w, Action{Kind: Undo})
	v := w.m.applicationState
	if i := v.index(closedKey); i < 0 || v.Layouts[i] != closedPlacement {
		t.Fatal("undo did not restore the forgotten placement into remaining capacity")
	}
	if i := v.index(launcher.surface.Key); i < 0 || v.Layouts[i] != newPlacement || v.Selected&(1<<i) == 0 || v.Active != launcher.surface.Key {
		t.Fatal("undo lost a later registered live window or its selection")
	}
	command(t, w, Action{Kind: Redo})
	v = w.m.applicationState
	if v.index(closedKey) >= 0 || v.index(launcher.surface.Key) < 0 || v.Active != launcher.surface.Key {
		t.Fatal("redo removed the new window instead of the forgotten key")
	}
	if err := w.Document().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestForgetUndoCapacityConflictIsAtomic(t *testing.T) {
	w, apps := multipleApplications(t, MaxApplicationLayouts)
	apps.surfaces = nil
	launcher := terminalLauncher(t, apps)
	w.SetApplications(launcher)
	command(t, w, Action{Kind: ForgetClosedPlacements})
	w.launchTerminal(launcher)
	before, history := w.Document(), w.historyPosition
	if err := w.Dispatch(Action{Kind: Undo}); err == nil || !strings.Contains(err.Error(), "32-window limit") {
		t.Fatalf("undo should report a capacity conflict: %v", err)
	}
	if w.Document() != before || w.historyPosition != history || w.application.ID != launcher.surface.ID || !w.CanUndo() {
		t.Fatal("refused undo changed history, dropped a live window or partly restored layouts")
	}
	if !strings.Contains(w.applicationNotice, "cannot undo") {
		t.Fatal("undo capacity refusal did not produce visible feedback")
	}
	assertApplicationNoticeVisible(t, w)
}

func TestForgetRedoPreservesReopenedKeysAndTheirLiveState(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	reopened := apps.surfaces[1]
	apps.surfaces = apps.surfaces[:1]
	w.Update(0)
	command(t, w, Action{Kind: ForgetClosedPlacements})
	command(t, w, Action{Kind: Undo})
	apps.surfaces = append(apps.surfaces, reopened)
	w.Update(0)
	// Input focus/selection can change without adding a document edit. Model
	// the current live selection and a provider-associated restored placement.
	d := w.Document()
	i := d.View.Application.index(reopened.Key)
	d.View.Application.Active = reopened.Key
	d.View.Application.Selected = 1 << i
	d.View.Application.Layouts[i].Depth = 2.75
	d.View.Application.Layouts[i].Group = 7
	w.install(d, false)
	before := w.Document()
	command(t, w, Action{Kind: Redo})
	if w.Document() != before || w.application.ID != reopened.ID {
		t.Fatal("redo forgot a reopened live key or changed its position/group/selection")
	}
}

func TestForgetCancelsInFlightGestureBeforeRecordingRemoval(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	apps.surfaces = apps.surfaces[:1]
	w.Update(0)
	command(t, w, Action{Kind: ToggleApplicationPlacement})
	w.Draw(1440, 900)
	x, y := visibleApplication(t, w, apps.surfaces[0])
	before, history := w.Document(), w.historyPosition
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerMove, x+24, y+12)
	if w.Document() == before || w.pointer.kind != captureApplicationPlacement {
		t.Fatal("test did not start a live placement preview")
	}
	command(t, w, Action{Kind: ForgetClosedPlacements})
	if w.pointer.kind != captureNone || w.m.applicationState.Layouts[0] != before.View.Application.Layouts[0] || w.historyPosition != history+1 {
		t.Fatal("forget committed an unfinished placement or retained capture")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("undo mixed the cancelled preview into the forgotten layout edit")
	}
}

func TestForgetClickCancellationResizeAndLiveReappearance(t *testing.T) {
	for _, cancel := range []string{"release-outside", "pointer-cancel", "keyboard-cancel", "resize", "reappeared"} {
		t.Run(cancel, func(t *testing.T) {
			w, apps := multipleApplications(t, 1)
			surface := apps.surfaces[0]
			apps.surfaces = nil
			w.Update(0)
			before := w.Document()
			b := forgetClosedPlacementsButton
			x, y := b.x+12, b.y+12
			if !pointer(w, experience.PointerDown, x, y) || w.Document() != before {
				t.Fatal("forget acted before the completed click")
			}
			switch cancel {
			case "release-outside":
				x -= b.w
			case "pointer-cancel":
				w.Handle(experience.Event{Kind: experience.PointerCancel})
			case "keyboard-cancel":
				w.Handle(experience.Event{Kind: experience.KeyboardCancel})
			case "resize":
				// The host cancels input before changing its coordinate extent.
				w.Handle(experience.Event{Kind: experience.PointerCancel})
				w.Draw(2880, 1800)
				x, y = x*2, y*2
			case "reappeared":
				apps.surfaces = []experience.ApplicationSurface{surface}
			}
			pointer(w, experience.PointerUp, x, y)
			if w.Document() != before || w.CanUndo() {
				t.Fatal("cancelled click or live reappearance forgot a placement")
			}
			if cancel == "resize" {
				clickForgetClosed(w)
				if w.closedPlacementMask() != 0 || !w.CanUndo() {
					t.Fatal("scaled cleanup control did not accept a new completed click")
				}
			}
		})
	}
}

func TestForgetControlAndNoticeDoNotOverlap(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	apps.surfaces = nil
	w.Update(0)
	w.showApplicationNotice(strings.Repeat("W", 120))
	frame := w.Draw(1440, 900)
	color := w.color(amber, 1)
	for _, vertex := range frame.Vertices {
		if vertex.Y >= 106 && vertex.Y < 125 && vertex.R == color.R && vertex.G == color.G && vertex.B == color.B && vertex.A == 1 && vertex.X >= forgetClosedPlacementsButton.x {
			t.Fatal("wide error text covered the cleanup button")
		}
	}
	clickForgetClosed(w)
	before, history := w.Document(), w.historyPosition
	clickForgetClosed(w)
	if w.Document() != before || w.historyPosition != history || w.pointer.kind != captureNone {
		t.Fatal("hidden cleanup control acted without closed placements")
	}
}
