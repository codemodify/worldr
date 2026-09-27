package workspace

import (
	"bytes"
	"encoding/json"
	"math"
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
	// Most workspace tests exercise deterministic document and input behavior.
	// Opt them out of autonomous ambient impacts; dedicated cat-collision tests
	// enable the production-default preference explicitly.
	w.environment.CatCollisions = false
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
	legacy := bytes.Replace(data, []byte(`"presentation": "cinematic",`), []byte("\"presentation\": \"adaptive\",\n  \"reduced_motion\": true,"), 1)
	if bytes.Equal(data, legacy) {
		t.Fatal("legacy desktop fixture did not add removed preferences")
	}
	legacyLoaded := desktop(t)
	if err := legacyLoaded.LoadState(legacy); err != nil {
		t.Fatal(err)
	}
	if legacyLoaded.Document() != w.Document() || legacyLoaded.State().Presentation != presentation.Cinematic || legacyLoaded.State().ReducedMotion {
		t.Fatal("legacy desktop preferences were not normalized to cinematic full motion")
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
	badDocument := before
	badDocument.View.Camera.TargetX = 101
	if err := badDocument.Validate(); err == nil {
		t.Fatal("accepted an out-of-bounds camera target")
	}
	badDocument = before
	badDocument.View.Camera.TargetDepth = float32(math.NaN())
	if err := badDocument.Validate(); err == nil {
		t.Fatal("accepted a non-finite camera target")
	}
	state["camera"].(map[string]any)["zoom"] = float64(before.View.Camera.Zoom)
	state["camera"].(map[string]any)["target_y"] = 1000
	badTarget, _ := json.Marshal(state)
	for _, invalid := range [][]byte{studyState, append(append([]byte(nil), data...), []byte(` {}`)...), badCamera, badTarget, []byte(`{"version":2}`)} {
		if err := w.LoadState(invalid); err == nil || w.Document() != before || w.historyPosition != history {
			t.Fatal("invalid or wrong-experience state changed the desktop")
		}
	}
}

func TestDesktopEnvironmentSettingsStateCompatibilityAndTransactions(t *testing.T) {
	raw, err := NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	if !raw.environment.CatCollisions {
		t.Fatal("desktop did not enable requested cat collision physics by default")
	}
	_ = raw.Close()

	w := desktop(t)
	w.environment = environmentSettings{}
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatal(err)
	}
	environment, ok := encoded["environment"].(map[string]any)
	if !ok || len(environment) != 4 {
		t.Fatalf("desktop state omitted its Environment settings: %#v", encoded["environment"])
	}
	for _, name := range []string{"dna", "cat", "cat_collisions", "eyes"} {
		if value, present := environment[name]; !present || value != false {
			t.Fatalf("desktop state did not encode %s=false explicitly: %#v", name, environment)
		}
	}

	for _, snapshot := range []struct {
		name string
		make func() ([]byte, error)
	}{
		{name: "save", make: w.SaveState},
		{name: "checkpoint", make: w.CheckpointState},
	} {
		t.Run(snapshot.name, func(t *testing.T) {
			state, err := snapshot.make()
			if err != nil {
				t.Fatal(err)
			}
			restored := desktop(t)
			if err := restored.LoadState(state); err != nil || restored.environment != (environmentSettings{}) {
				t.Fatalf("disabled Environment settings did not round-trip: state=%+v err=%v", restored.environment, err)
			}
		})
	}

	delete(encoded, "environment")
	legacy, err := json.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	legacyLoaded := desktop(t)
	if err := legacyLoaded.LoadState(legacy); err != nil || legacyLoaded.environment != defaultEnvironmentSettings() {
		t.Fatalf("legacy state did not receive enabled Environment defaults: state=%+v err=%v", legacyLoaded.environment, err)
	}

	// Documents written before paw physics have an Environment object but no
	// collision member. Decoding into the preinitialized defaults enables the new
	// requested behavior without changing the desktop document version.
	withLegacyEnvironment := make(map[string]any)
	if err := json.Unmarshal(data, &withLegacyEnvironment); err != nil {
		t.Fatal(err)
	}
	delete(withLegacyEnvironment["environment"].(map[string]any), "cat_collisions")
	legacyEnvironment, err := json.Marshal(withLegacyEnvironment)
	if err != nil {
		t.Fatal(err)
	}
	legacyLoaded = desktop(t)
	if err := legacyLoaded.LoadState(legacyEnvironment); err != nil || !legacyLoaded.environment.CatCollisions {
		t.Fatalf("legacy Environment object did not receive enabled cat collision default: state=%+v err=%v", legacyLoaded.environment, err)
	}

	invalid := make(map[string]any)
	if err := json.Unmarshal(data, &invalid); err != nil {
		t.Fatal(err)
	}
	invalid["environment"].(map[string]any)["unknown"] = true
	malformed, err := json.Marshal(invalid)
	if err != nil {
		t.Fatal(err)
	}
	beforeDocument, beforeEnvironment, history := w.Document(), w.environment, w.historyPosition
	if err := w.LoadState(malformed); err == nil || w.Document() != beforeDocument || w.environment != beforeEnvironment || w.historyPosition != history {
		t.Fatal("invalid Environment state changed the live desktop")
	}
}

func TestDesktopWindowSettingsStateCompatibilityAndTransactions(t *testing.T) {
	w := desktop(t)
	w.windows.Border = windowBorderTelemetry
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatal(err)
	}
	windows, ok := encoded["windows"].(map[string]any)
	if !ok || len(windows) != 1 || windows["border"] != string(windowBorderTelemetry) {
		t.Fatalf("desktop state omitted its window settings: %#v", encoded["windows"])
	}

	for _, snapshot := range []struct {
		name string
		make func() ([]byte, error)
	}{
		{name: "save", make: w.SaveState},
		{name: "checkpoint", make: w.CheckpointState},
	} {
		t.Run(snapshot.name, func(t *testing.T) {
			state, err := snapshot.make()
			if err != nil {
				t.Fatal(err)
			}
			restored := desktop(t)
			if err := restored.LoadState(state); err != nil || restored.windows.Border != windowBorderTelemetry {
				t.Fatalf("window settings did not round-trip: state=%+v err=%v", restored.windows, err)
			}
		})
	}

	delete(encoded, "windows")
	legacy, err := json.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	legacyLoaded := desktop(t)
	if err := legacyLoaded.LoadState(legacy); err != nil || legacyLoaded.windows != defaultWindowSettings() {
		t.Fatalf("legacy state did not receive default window settings: state=%+v err=%v", legacyLoaded.windows, err)
	}

	invalid := make(map[string]any)
	if err := json.Unmarshal(data, &invalid); err != nil {
		t.Fatal(err)
	}
	invalid["windows"].(map[string]any)["border"] = "unknown"
	malformed, err := json.Marshal(invalid)
	if err != nil {
		t.Fatal(err)
	}
	beforeDocument, beforeEnvironment, beforeWindows, history := w.Document(), w.environment, w.windows, w.historyPosition
	if err := w.LoadState(malformed); err == nil || w.Document() != beforeDocument || w.environment != beforeEnvironment || w.windows != beforeWindows || w.historyPosition != history {
		t.Fatal("invalid window settings changed the live desktop")
	}

	w.windows.Border = "invalid-runtime-value"
	if _, err := w.SaveState(); err == nil {
		t.Fatal("desktop save accepted an invalid live window border")
	}
}

func TestDesktopResetViewPreservesApplicationPlacementAndPreferences(t *testing.T) {
	w := desktop(t)
	desktopApplications(t, w)
	command(t, w, Action{Kind: MoveApplications, DeltaX: .7, DeltaDepth: 1})
	command(t, w, Action{Kind: OrbitCamera, DeltaX: 25, DeltaY: -10})
	before := w.Document()
	if !key(w, experience.KeyR, 0) {
		t.Fatal("desktop R did not reset the view")
	}
	after := w.Document()
	if after.View.Camera == before.View.Camera || after.View.Application.Layouts != before.View.Application.Layouts || after.View.Presentation != presentation.Cinematic || after.View.ReducedMotion || after.Timeline != before.Timeline {
		t.Fatal("desktop camera reset affected placements, presentation, motion, or study state")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("desktop view reset could not be undone")
	}
}
