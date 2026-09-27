package workspace

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
)

func navigationPersistenceWorkspace(t *testing.T) *Workspace {
	t.Helper()
	w, err := NewNavigator()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func TestNavigatorPreferencesSaveAndCheckpointRoundTrip(t *testing.T) {
	w := navigationPersistenceWorkspace(t)
	w.navigation.home, w.navigation.motionEnabled = false, false
	w.navigation.page, w.navigation.returnFocus = 31, "native:research/disconnected"
	w.navigation.held[helpKey{key: experience.KeyH}] = true
	w.navigation.pressed, w.navigation.toolsOpen, w.navigation.help = "home", true, true
	w.navigation.motion.elapsed = 17
	w.m.applicationState.Spaces[1] = SpaceState{Name: "Research"}
	w.m.applicationState.Layouts[3] = ApplicationPlacement{Key: "native:research/disconnected", Space: 1, X: .3, Y: -.5, Width: 960, Height: 600}
	w.pointer = pointerCapture{kind: captureButton, pressX: 30, pressY: 40}
	want := *w.savedNavigationPreferences()

	for _, snapshot := range []struct {
		name string
		make func() ([]byte, error)
	}{{"save", w.SaveState}, {"checkpoint", w.CheckpointState}} {
		t.Run(snapshot.name, func(t *testing.T) {
			beforeNavigation := *w.navigation
			beforeHeld := make(map[helpKey]bool)
			for key, owned := range w.navigation.held {
				beforeHeld[key] = owned
			}
			beforePointer, beforeDocument := w.pointer, w.Document()
			data, err := snapshot.make()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(beforeNavigation, *w.navigation) || !reflect.DeepEqual(beforeHeld, w.navigation.held) || w.pointer != beforePointer || w.Document() != beforeDocument {
				t.Fatal("saving preferences changed live navigation, capture or document")
			}
			var encoded desktopDocument
			if err := json.Unmarshal(data, &encoded); err != nil {
				t.Fatal(err)
			}
			if encoded.Version != 1 || encoded.Navigation == nil || *encoded.Navigation != want {
				t.Fatalf("Navigator preferences did not persist in v1: %+v", encoded.Navigation)
			}
			for _, explicit := range []string{`"home": false`, `"motion_enabled": false`, `"page": 31`} {
				if !bytes.Contains(data, []byte(explicit)) {
					t.Fatalf("false/boundary preference omitted: %s", explicit)
				}
			}
			for _, transient := range []string{`"held"`, `"pressed"`, `"tools_open"`, `"elapsed"`} {
				if bytes.Contains(data, []byte(transient)) {
					t.Fatalf("transient Navigator state persisted: %s", transient)
				}
			}
			restored := navigationPersistenceWorkspace(t)
			restored.navigation.held[helpKey{key: experience.KeyJ}] = true
			restored.navigation.pressed, restored.navigation.toolsOpen, restored.navigation.help = "next", true, true
			if err := restored.LoadState(data); err != nil {
				t.Fatal(err)
			}
			if *restored.savedNavigationPreferences() != want {
				t.Fatalf("saved preference did not roundtrip: %+v", restored.savedNavigationPreferences())
			}
			if restored.navigation.pressed != "" || len(restored.navigation.held) != 0 || restored.navigation.toolsOpen || restored.navigation.help || restored.navigation.motion.initialized || restored.applicationKeyboard || restored.pointer.kind != captureNone {
				t.Fatal("restore replayed transient navigation/input or granted application focus")
			}
			if restored.Document().View.Application.Spaces != encoded.Applications.Spaces || restored.Document().View.Application.Layouts != encoded.Applications.Layouts {
				t.Fatal("Navigator preferences disrupted saved projects or window slots")
			}
		})
	}
}

func TestNavigatorPreferencesRestoreCurrentPageMemory(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		home, reading         bool
		overview              bool
		wantHome, wantProject int
	}{
		{name: "home", home: true, wantHome: 2},
		{name: "project", overview: true, wantProject: 2},
		{name: "focused app sidebar", reading: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := navigationPersistenceWorkspace(t)
			w.navigation.home, w.navigation.page = tc.home, 2
			w.m.applicationState.Space = 3
			w.m.applicationState.Spaces[3] = SpaceState{Name: "Restored project"}
			w.m.applicationState.Reading = tc.reading
			w.m.applicationState.Overview = tc.overview
			data, err := w.SaveState()
			if err != nil {
				t.Fatal(err)
			}
			restored := navigationPersistenceWorkspace(t)
			if err := restored.LoadState(data); err != nil {
				t.Fatal(err)
			}
			if restored.navigation.homePage != tc.wantHome || restored.navigation.projectPages[3] != tc.wantProject || restored.navigation.projectPage != tc.wantProject {
				t.Fatalf("restored current page memory is wrong: home=%d projects=%v", restored.navigation.homePage, restored.navigation.projectPages)
			}
			for i, page := range restored.navigation.projectPages {
				if i != 3 && page != 0 {
					t.Fatalf("restore assigned current page to unrelated project %d", i)
				}
			}
		})
	}
}

func TestNavigatorPreferencesLegacyDefaultsAndOptIn(t *testing.T) {
	legacy := desktop(t)
	for _, snapshot := range []func() ([]byte, error){legacy.SaveState, legacy.CheckpointState} {
		data, err := snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(`"navigation"`)) {
			t.Fatal("ordinary desktop opted into Navigator persistence")
		}
		for _, null := range []bool{false, true} {
			if null {
				data = append([]byte(`{"navigation":null,`), data[1:]...)
			}
			w := navigationPersistenceWorkspace(t)
			w.navigation.home, w.navigation.motionEnabled, w.navigation.page, w.navigation.returnFocus = false, false, 8, "old:focus"
			if err := w.LoadState(data); err != nil {
				t.Fatal(err)
			}
			if got := *w.savedNavigationPreferences(); got != (navigationPreferences{Home: true, MotionEnabled: true}) {
				t.Fatalf("legacy document did not reset Navigator defaults: %+v", got)
			}
		}
	}
	navigator := navigationPersistenceWorkspace(t)
	data, err := navigator.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(`"return_focus"`)) {
		t.Fatal("empty optional return focus was persisted")
	}
	if err := legacy.LoadState(data); err != nil {
		t.Fatal(err)
	}
	if legacy.navigation != nil {
		t.Fatal("loading Navigator preferences changed the selected experience")
	}
	data, err = legacy.SaveState()
	if err != nil || bytes.Contains(data, []byte(`"navigation"`)) {
		t.Fatal("ordinary desktop retained opt-in Navigator preferences")
	}
}

func TestNavigatorPreferencesRejectInvalidLoadTransactionally(t *testing.T) {
	w := navigationPersistenceWorkspace(t)
	w.navigation.home, w.navigation.page, w.navigation.returnFocus = false, 2, "native:notes"
	w.navigation.held[helpKey{key: experience.KeyK}] = true
	w.navigation.pressed = "next"
	w.pointer = pointerCapture{kind: captureButton, pressX: 41}
	w.history, w.historyPosition = []edit{{}}, 1
	data, err := w.CheckpointState()
	if err != nil {
		t.Fatal(err)
	}
	beforeDocument, beforePointer, beforeNavigation := w.Document(), w.pointer, w.navigation
	beforeState := *w.navigation
	beforeHeld := map[helpKey]bool{helpKey{key: experience.KeyK}: true}
	var encoded map[string]json.RawMessage
	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatal(err)
	}
	for _, preferences := range []string{
		`{"home":true,"motion_enabled":true,"page":-1}`,
		`{"home":true,"motion_enabled":true,"page":32}`,
		`{"home":true,"motion_enabled":true,"page":1.5}`,
		`{"home":true,"motion_enabled":true,"page":0,"return_focus":"bad\nkey"}`,
		`{"home":true,"motion_enabled":true,"page":0,"return_focus":"` + strings.Repeat("x", 257) + `"}`,
		`{"home":"yes","motion_enabled":true,"page":0}`,
		`{"home":true,"motion_enabled":"no","page":0}`,
		`{"home":true,"motion_enabled":true,"page":0,"unknown":true}`,
	} {
		encoded["navigation"] = json.RawMessage(preferences)
		invalid, err := json.Marshal(encoded)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.LoadState(invalid); err == nil {
			t.Fatalf("invalid preferences accepted: %s", preferences)
		}
		if w.Document() != beforeDocument || w.pointer != beforePointer || w.navigation != beforeNavigation || !reflect.DeepEqual(*w.navigation, beforeState) || !reflect.DeepEqual(w.navigation.held, beforeHeld) || w.historyPosition != 1 || len(w.history) != 1 {
			t.Fatal("invalid Navigator load changed document, preferences, capture or history")
		}
	}
	// Valid preferences must also wait for validation of the containing document.
	encoded["navigation"] = json.RawMessage(`{"home":true,"motion_enabled":true,"page":0}`)
	encoded["camera"] = json.RawMessage(`{"yaw":0,"pitch":99}`)
	invalid, err := json.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.LoadState(invalid); err == nil || w.navigation != beforeNavigation || !reflect.DeepEqual(*w.navigation, beforeState) || w.Document() != beforeDocument {
		t.Fatal("invalid desktop document partially installed valid Navigator preferences")
	}
}

func TestNavigatorPreferencesRejectInvalidSave(t *testing.T) {
	w := navigationPersistenceWorkspace(t)
	for _, invalid := range []navigationPreferences{
		{Page: -1}, {Page: 32}, {ReturnFocus: "bad\x00key"}, {ReturnFocus: string([]byte{0xff})},
	} {
		w.navigation.home, w.navigation.motionEnabled = invalid.Home, invalid.MotionEnabled
		w.navigation.page, w.navigation.returnFocus = invalid.Page, invalid.ReturnFocus
		for _, snapshot := range []func() ([]byte, error){w.SaveState, w.CheckpointState} {
			if _, err := snapshot(); err == nil {
				t.Fatalf("invalid preferences exported: %+v", invalid)
			}
		}
	}
}

func TestNavigatorStarterWorkspaceTemplate(t *testing.T) {
	data, err := os.ReadFile("../../examples/navigator-desktop/workspace.json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Version      int             `json:"version"`
		ExperienceID string          `json:"experience_id"`
		State        json.RawMessage `json:"state"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatal("starter template has trailing JSON")
	}
	w := navigationPersistenceWorkspace(t)
	if envelope.Version != 1 || envelope.ExperienceID != w.Info().ID {
		t.Fatal("starter template has an incompatible state envelope")
	}
	if err := w.LoadState(envelope.State); err != nil {
		t.Fatal(err)
	}
	if got := *w.savedNavigationPreferences(); got != (navigationPreferences{Home: true, MotionEnabled: true}) {
		t.Fatalf("starter template does not open Home with motion: %+v", got)
	}
	apps := &fakeApplications{}
	texture, err := render.NewTexture(160, 100, make([]byte, 160*100*4))
	if err != nil {
		t.Fatal(err)
	}
	for i, entry := range []struct {
		key   string
		space uint8
	}{
		{"native:project-browser", 0},
		{"native:research-workbench", 1},
		{"native:model", 2},
		{AxialApplicationKey, 2},
	} {
		view := w.Document().View.Application
		if slot := view.index(entry.key); slot < 0 || view.Layouts[slot].Space != entry.space {
			t.Fatalf("starter %q has no placement in project %d", entry.key, entry.space)
		}
		apps.surfaces = append(apps.surfaces, experience.ApplicationSurface{ID: uint64(500 + i), Key: entry.key, Title: entry.key, Texture: texture})
	}
	w.SetApplications(apps)
	w.Draw(1280, 820) // CPU scene recording only; no renderer or GPU session.
	if len(w.navigation.layout.projects) != 3 {
		t.Fatal("starter template does not expose three Home project cards")
	}
	for i, project := range w.navigation.layout.projects {
		wantName := []string{"Code", "Research", "Design"}[i]
		wantCount := []int{1, 1, 2}[i]
		if project.name != wantName || project.count != wantCount {
			t.Fatalf("project %d = %q/%d apps, want %q/%d", i, project.name, project.count, wantName, wantCount)
		}
	}
	if w.applicationKeyboard {
		t.Fatal("loading starter groups granted application keyboard focus")
	}
}
