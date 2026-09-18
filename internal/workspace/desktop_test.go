package workspace

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/presentation"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

func desktop(t *testing.T) *Workspace {
	t.Helper()
	w, err := NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func desktopApplications(t *testing.T, w *Workspace) *fakeApplications {
	t.Helper()
	texture, err := render.NewTexture(160, 100, make([]byte, 160*100*4))
	if err != nil {
		t.Fatal(err)
	}
	apps := &fakeApplications{surfaces: []experience.ApplicationSurface{{ID: 41, Key: "native:project-browser", Title: "Project browser", Texture: texture}}}
	w.SetApplications(apps)
	w.Draw(1440, 900)
	return apps
}

func TestDesktopHasNoStudyResourcesBeforeOrAfterLastApplication(t *testing.T) {
	w := desktop(t)
	allowed := map[*render.Geometry]bool{w.scene.Node(w.stageNode).Mesh.Geometry(): true}
	var allowBackdrop func(scene.NodeID)
	allowBackdrop = func(parent scene.NodeID) {
		for _, id := range w.backgroundScene.Children(parent) {
			if mesh := w.backgroundScene.Node(id).Mesh; mesh != nil {
				allowed[mesh.Geometry()] = true
			}
			allowBackdrop(id)
		}
	}
	allowBackdrop(0)
	checkEmpty := func() {
		t.Helper()
		frame := w.Draw(1440, 900)
		for _, command := range frame.Commands {
			for _, draw := range command.Draws {
				if draw.Texture != nil || !allowed[draw.Geometry] {
					t.Fatal("empty desktop submitted study geometry or an instrument texture")
				}
			}
		}
		if w.panel != nil || w.panelNode != 0 || w.nodes != [3]scene.NodeID{} {
			t.Fatal("desktop allocated AXIAL study resources")
		}
	}
	checkEmpty()
	if w.Info().ID != "worldr.workspace" || w.Info().ID == study(t).Info().ID || w.panel != nil {
		t.Fatal("desktop retained the study's identity or instrument allocation")
	}
	apps := desktopApplications(t, w)
	command(t, w, Action{Kind: ToggleApplicationReading})
	w.Draw(1440, 900)
	apps.surfaces = nil
	w.Update(time.Second)
	checkEmpty()
	if w.State().Playing {
		t.Fatal("desktop started synthetic study playback")
	}
}

func TestDesktopIgnoresStudyControlsAndRejectsStudyActions(t *testing.T) {
	w := desktop(t)
	w.Draw(1440, 900)
	before := w.Document()
	for _, control := range []experience.Key{experience.KeySpace, experience.KeyE, experience.Key1, experience.Key2, experience.Key3, experience.KeyLeft, experience.KeyRight, experience.KeyB, experience.KeyF} {
		if key(w, control, 0) {
			t.Fatalf("empty desktop consumed study shortcut %q", control)
		}
	}
	for _, point := range [][2]float32{{80, 830}, {800, 824}, {100, 625}, {110, 330}} {
		pointer(w, experience.PointerDown, point[0], point[1])
		pointer(w, experience.PointerUp, point[0], point[1])
	}
	for _, kind := range []ActionKind{TogglePlayback, SetPlayback, ToggleExplode, SetExploded, ToggleFocus, SetFocused, TogglePanelDepth, SelectComponent, SeekTime, StepTime, ResetStudy} {
		if err := w.Dispatch(Action{Kind: kind}); err == nil {
			t.Fatalf("desktop accepted study action %q", kind)
		}
	}
	w.Demo(30 * time.Second)
	w.Update(time.Second)
	if w.Document() != before || w.CanUndo() {
		t.Fatal("study controls changed desktop state or history")
	}
}

func TestDesktopStateRoundTripAndStudyStateRejection(t *testing.T) {
	w := desktop(t)
	apps := desktopApplications(t, w)
	command(t, w, Action{Kind: MoveApplications, DeltaX: 1.2, DeltaY: -.7, DeltaDepth: -2})
	command(t, w, Action{Kind: OrbitCamera, DeltaX: 15, DeltaY: -20})
	command(t, w, Action{Kind: SetPresentation, Presentation: presentation.Adaptive})
	command(t, w, Action{Kind: SetReducedMotion, Enabled: true})
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range [][]byte{[]byte(`"selection"`), []byte(`"timeline"`), []byte(`"exploded"`), []byte(`"panel_behind"`)} {
		if bytes.Contains(data, forbidden) {
			t.Fatalf("desktop persisted unrelated study field %s", forbidden)
		}
	}
	loaded := desktop(t)
	if err := loaded.LoadState(data); err != nil {
		t.Fatal(err)
	}
	reconnected := &fakeApplications{surfaces: append([]experience.ApplicationSurface(nil), apps.surfaces...)}
	reconnected.surfaces[0].ID = 900
	loaded.SetApplications(reconnected)
	loaded.Draw(1440, 900)
	if loaded.Document() != w.Document() || loaded.application.ID != 900 || loaded.OwnsKeyboard() || loaded.CanUndo() {
		t.Fatal("desktop restore lost stable placement/preferences or restored transient input/history")
	}
	studyState, err := study(t).SaveState()
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["camera"].(map[string]any)["zoom"] = 4
	badCamera, _ := json.Marshal(state)
	before, history := w.Document(), w.historyPosition
	for _, invalid := range [][]byte{studyState, append(append([]byte(nil), data...), []byte(` {}`)...), badCamera, []byte(`{"version":2}`)} {
		if err := w.LoadState(invalid); err == nil || w.Document() != before || w.historyPosition != history {
			t.Fatal("invalid or wrong-experience state changed the desktop")
		}
	}
}

func TestDesktopResetViewPreservesApplicationPlacementAndPreferences(t *testing.T) {
	w := desktop(t)
	desktopApplications(t, w)
	command(t, w, Action{Kind: MoveApplications, DeltaX: .7, DeltaDepth: 1})
	command(t, w, Action{Kind: SetPresentation, Presentation: presentation.Adaptive})
	command(t, w, Action{Kind: OrbitCamera, DeltaX: 25, DeltaY: -10})
	before := w.Document()
	if !key(w, experience.KeyR, 0) {
		t.Fatal("desktop R did not reset the view")
	}
	after := w.Document()
	if after.View.Camera == before.View.Camera || after.View.Application.Layouts != before.View.Application.Layouts || after.View.Presentation != before.View.Presentation || after.Timeline != before.Timeline {
		t.Fatal("desktop camera reset affected placements, presentation or study state")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("desktop view reset could not be undone")
	}
}
