package workspace

import (
	"reflect"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

type checkpointObservation struct {
	document        Document
	pointer         pointerCapture
	head            *windowThrow
	motions         []windowThrow
	history         []edit
	historyPosition int
	focused         uint64
	keyboard        bool
	clientEvents    int
	focusCalls      int
}

func observeCheckpoint(w *Workspace, apps *fakeApplications) checkpointObservation {
	result := checkpointObservation{document: w.Document(), pointer: w.pointer, head: w.windowThrow,
		history: append([]edit(nil), w.history...), historyPosition: w.historyPosition,
		focused: w.applicationFocusedID, keyboard: w.applicationKeyboard, clientEvents: len(apps.events), focusCalls: len(apps.focus)}
	for motion := w.windowThrow; motion != nil; motion = motion.next {
		result.motions = append(result.motions, *motion)
	}
	return result
}

func assertCheckpointUntouched(t *testing.T, before checkpointObservation, w *Workspace, apps *fakeApplications) {
	t.Helper()
	if after := observeCheckpoint(w, apps); !reflect.DeepEqual(before, after) {
		t.Fatal("checkpoint changed live document, capture, focus, motion, history, or client input")
	}
}

func TestCheckpointPreservesIndependentThrowsAndFocusedApplication(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 3)
	first, second, focused := apps.surfaces[0], apps.surfaces[1], apps.surfaces[2]
	startWindowThrow(t, w, first)
	w.Update(40 * time.Millisecond)
	startWindowThrow(t, w, second)
	w.Update(60 * time.Millisecond)
	x, y := visibleApplication(t, w, focused)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if !throwingSurface(w, first.ID) || !throwingSurface(w, second.ID) || w.applicationFocusedID != focused.ID || !w.applicationKeyboard {
		t.Fatal("fixture did not retain two throws and independent application focus")
	}
	before := observeCheckpoint(w, apps)
	data, err := w.CheckpointState()
	if err != nil {
		t.Fatal(err)
	}
	assertCheckpointUntouched(t, before, w, apps)
	restored := desktop(t)
	if err := restored.LoadState(data); err != nil {
		t.Fatal(err)
	}
	if restored.Document().View != before.document.View || restored.windowThrow != nil || restored.applicationKeyboard {
		t.Fatal("checkpoint did not restore current placements without replaying motion or acquiring focus")
	}
	restored.Update(time.Second)
	if restored.Document().View != before.document.View {
		t.Fatal("restored checkpoint resumed transient throw velocity")
	}
	w.Update(100 * time.Millisecond)
	after := w.Document().View.Application.Layouts
	if after[0] == before.document.View.Application.Layouts[0] || after[1] == before.document.View.Application.Layouts[1] || w.applicationFocusedID != focused.ID {
		t.Fatal("checkpoint stopped a live throw or changed the focused sibling")
	}
}

func TestCheckpointExcludesHeldDragWhileKeepingSiblingCoastCurrent(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 2)
	coasting, held := apps.surfaces[0], apps.surfaces[1]
	startWindowThrow(t, w, coasting)
	w.Update(40 * time.Millisecond)
	beforeDrag := w.Document()
	x, y := windowGripPoint(t, w, held)
	w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y, Time: 2000})
	w.Handle(experience.Event{Kind: experience.PointerMove, X: x + 40, Y: y + 20, Time: 2040})
	w.Update(120 * time.Millisecond)
	before := observeCheckpoint(w, apps)
	if !w.pointer.windowDrag || !throwingSurface(w, coasting.ID) || before.document.View.Application.Layouts[0] == beforeDrag.View.Application.Layouts[0] || before.document.View.Application.Layouts[1] == beforeDrag.View.Application.Layouts[1] {
		t.Fatal("fixture did not hold one changed drag while its sibling advanced")
	}
	data, err := w.CheckpointState()
	if err != nil {
		t.Fatal(err)
	}
	assertCheckpointUntouched(t, before, w, apps)
	restored := desktop(t)
	if err := restored.LoadState(data); err != nil {
		t.Fatal(err)
	}
	got := restored.Document().View.Application
	if got.Layouts[0] != before.document.View.Application.Layouts[0] || got.Layouts[1] != beforeDrag.View.Application.Layouts[1] || got.Active != beforeDrag.View.Application.Active || got.Selected != beforeDrag.View.Application.Selected {
		t.Fatal("checkpoint included the held preview or rewound its independently moving sibling")
	}
	if restored.pointer.kind != captureNone || restored.windowThrow != nil {
		t.Fatal("checkpoint restored transient input or motion")
	}
	w.Update(100 * time.Millisecond)
	if w.pointer != before.pointer || w.Document().View.Application.Layouts[0] == before.document.View.Application.Layouts[0] {
		t.Fatal("checkpoint broke the original drag capture or continuing sibling motion")
	}
	// The original gesture can still be cancelled after a checkpoint, without
	// reverting the unrelated coast that advanced while it was held.
	advanced := w.Document().View.Application.Layouts[0]
	w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyEscape, Keycode: 1, Pressed: true})
	if w.pointer.kind != captureNone || w.Document().View.Application.Layouts[1] != beforeDrag.View.Application.Layouts[1] || w.Document().View.Application.Layouts[0] != advanced {
		t.Fatal("checkpoint changed the held gesture's later cancellation semantics")
	}
}

func TestCheckpointExcludesUnfinishedOrbitWithoutRewindingStudyTime(t *testing.T) {
	w := study(t)
	start := w.Document()
	pointer(w, experience.PointerDown, 600, 400)
	pointer(w, experience.PointerMove, 660, 412)
	w.Update(100 * time.Millisecond)
	apps := &fakeApplications{}
	before := observeCheckpoint(w, apps)
	if w.pointer.kind != captureOrbit || before.document.View.Camera == start.View.Camera || before.document.Timeline == start.Timeline {
		t.Fatal("fixture did not preview an orbit while study time advanced")
	}
	data, err := w.CheckpointState()
	if err != nil {
		t.Fatal(err)
	}
	assertCheckpointUntouched(t, before, w, apps)
	restored := study(t)
	if err := restored.LoadState(data); err != nil {
		t.Fatal(err)
	}
	if got := restored.Document(); got.View.Camera != start.View.Camera || got.Timeline != before.document.Timeline {
		t.Fatal("study checkpoint retained an unfinished camera edit or rewound independent playback")
	}
	pointer(w, experience.PointerUp, 660, 412)
	if w.pointer.kind != captureNone || w.historyPosition != before.historyPosition+1 || w.Document().View.Camera != before.document.View.Camera {
		t.Fatal("checkpoint prevented the original orbit from committing normally")
	}
}
