package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/mediaapp"
	"github.com/codemodify/worldr/internal/nativeapps"
	"github.com/codemodify/worldr/internal/noteapp"
	"github.com/codemodify/worldr/internal/photoapp"
	"github.com/codemodify/worldr/internal/projectapp"
	"github.com/codemodify/worldr/internal/researchapp"
	"github.com/codemodify/worldr/internal/workspace"
)

type manifestPersistenceFixture struct {
	persistenceFixture
	loads, saves, checkpoints int
}

func (f *manifestPersistenceFixture) LoadState(data []byte) error {
	f.loads++
	return f.persistenceFixture.LoadState(data)
}

func (f *manifestPersistenceFixture) SaveState() ([]byte, error) {
	f.saves++
	return f.persistenceFixture.SaveState()
}

func (f *manifestPersistenceFixture) CheckpointState() ([]byte, error) {
	f.checkpoints++
	return f.persistenceFixture.SaveState()
}

func completeSessionManifest() *sessionManifest {
	return &sessionManifest{Version: 1,
		Project:   &projectapp.SessionState{Root: "/work/project", Directory: "data/models", Selected: "assembly.json"},
		Terminals: []nativeapps.TerminalState{{Key: "native:terminal-2", Directory: "/work/project/data", Tasks: []nativeapps.TaskRecipe{{Name: "Tests", Command: "go test ./..."}}}, {Key: "native:terminal-7", Directory: "/work/other"}},
		Photo:     &photoapp.SessionState{Path: "/work/project/reference.png"},
		Media:     &mediaapp.SessionState{Path: "/work/project/experiment.mp4", Position: 42.75, Paused: true, Volume: 38, Muted: true},
		Research:  []researchapp.SessionState{{Key: "native:research-workbench-2", Source: "/work/project/signals.csv", View: researchapp.ViewState{X: 0, Y: 1, Z: 2, Selected: 4, Zoom: 1, Mode: "scatter"}}},
		Notes:     []noteapp.SessionState{{Key: "native:note-2", Text: "unsaved workspace note", Caret: 22, Anchor: 22, TopLine: 0, Dirty: true}},
		Axial:     &workspace.AxialApplicationState{Key: workspace.AxialApplicationKey, Time: 12.5, Yaw: -.4, Pitch: .2, Zoom: .1, Selected: 2, Exploded: true}}
}

func TestSessionManifestInvalidDataNeverReachesExperienceLoad(t *testing.T) {
	tooMany := &sessionManifest{Version: 1, Project: &projectapp.SessionState{Root: "/work/project"}}
	for i := 0; i < 32; i++ {
		key := "native:terminal"
		if i > 0 {
			key = fmt.Sprintf("native:terminal-%d", i+1)
		}
		tooMany.Terminals = append(tooMany.Terminals, nativeapps.TerminalState{Key: key, Directory: "/tmp"})
	}
	oversized, err := json.Marshal(tooMany)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, raw string }{
		{"null", `null`},
		{"array", `[]`},
		{"missing version", `{}`},
		{"unknown version", `{"version":2}`},
		{"unknown field", `{"version":1,"command":"must-not-run"}`},
		{"duplicate version", `{"version":1,"version":1}`},
		{"duplicate terminal key field", `{"version":1,"terminals":[{"key":"native:terminal","key":"native:terminal-2","directory":"/tmp"}]}`},
		{"duplicate terminal identities", `{"version":1,"terminals":[{"key":"native:terminal","directory":"/tmp"},{"key":"native:terminal","directory":"/elsewhere"}]}`},
		{"duplicate research identities", `{"version":1,"research":[{"key":"native:research-workbench","source":"/tmp/a.csv","view":{"x":0,"y":1,"z":2,"selected":0,"zoom":1,"mode":"line"}},{"key":"native:research-workbench","source":"/tmp/b.csv","view":{"x":0,"y":1,"z":2,"selected":0,"zoom":1,"mode":"line"}}]}`},
		{"invalid research view", `{"version":1,"research":[{"key":"native:research-workbench","source":"/tmp/a.csv","view":{"x":0,"y":1,"z":2,"selected":0,"zoom":9,"mode":"line"}}]}`},
		{"invalid hosted axial key", `{"version":1,"axial":{"key":"native:other","time":2,"yaw":0,"pitch":0,"zoom":1,"selected":0}}`},
		{"invalid hosted axial view", `{"version":1,"axial":{"key":"native:axial-07","time":31,"yaw":0,"pitch":0,"zoom":1,"selected":0}}`},
		{"duplicate note identities", `{"version":1,"notes":[{"key":"native:note","text":"one","caret":3,"anchor":3},{"key":"native:note","text":"two","caret":3,"anchor":3}]}`},
		{"note selection splits grapheme", `{"version":1,"notes":[{"key":"native:note","text":"e\u0301","caret":1,"anchor":0}]}`},
		{"command in terminal", `{"version":1,"terminals":[{"key":"native:terminal","directory":"/tmp","args":["-c","must-not-run"]}]}`},
		{"multiline task command", `{"version":1,"terminals":[{"key":"native:terminal","directory":"/tmp","tasks":[{"name":"bad","command":"true\nfalse"}]}]}`},
		{"noncanonical terminal key", `{"version":1,"terminals":[{"key":"native:terminal-01","directory":"/tmp"}]}`},
		{"null terminal", `{"version":1,"terminals":[null]}`},
		{"relative terminal directory", `{"version":1,"terminals":[{"key":"native:terminal","directory":"tmp"}]}`},
		{"relative photo", `{"version":1,"photo":{"path":"../image.png"}}`},
		{"null required path", `{"version":1,"photo":{"path":null}}`},
		{"duplicate photo path", `{"version":1,"photo":{"path":"/one","path":"/two"}}`},
		{"unknown photo field", `{"version":1,"photo":{"path":"/one","execute":true}}`},
		{"project traversal", `{"version":1,"project":{"root":"/tmp","directory":"../outside"}}`},
		{"project absolute child", `{"version":1,"project":{"root":"/tmp","directory":"/outside"}}`},
		{"project selection traversal", `{"version":1,"project":{"root":"/tmp","selected":"../outside"}}`},
		{"negative media position", `{"version":1,"media":{"path":"/movie","position":-1,"volume":70}}`},
		{"excessive media volume", `{"version":1,"media":{"path":"/movie","position":0,"volume":101}}`},
		{"nonfinite media number", `{"version":1,"media":{"path":"/movie","position":1e999,"volume":70}}`},
		{"nul path", `{"version":1,"photo":{"path":"/bad\u0000path"}}`},
		{"invalid UTF-8 path", "{\"version\":1,\"photo\":{\"path\":\"/bad\xffpath\"}}"},
		{"too many windows", string(oversized)},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			data := []byte(`{"version":2,"experience_id":"test.experience","state":{"value":20},"session":` + test.raw + `}`)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			fixture := &manifestPersistenceFixture{persistenceFixture: persistenceFixture{value: 7}}
			if _, err := loadSessionState(path, fixture); err == nil {
				t.Fatalf("accepted invalid manifest: %s", test.raw)
			}
			if fixture.loads != 0 || fixture.value != 7 {
				t.Fatal("invalid manifest reached the experience loader or changed its document")
			}
		})
	}
}

func TestSessionEnvelopeMismatchAndOversizeNeverReachExperienceLoad(t *testing.T) {
	for _, data := range []string{
		`{"version":1,"experience_id":"test.experience","state":{"value":20},"session":{"version":1}}`,
		`{"version":2,"experience_id":"test.experience","state":{"value":20}}`,
		`{"version":2,"experience_id":"other.experience","state":{"value":20},"session":{"version":1}}`,
		`{"version":2,"experience_id":"test.experience","state":{"value":20},"session":{"version":1},"session":{"version":1}}`,
		`{"version":2,"experience_id":"test.experience","state":{"value":20},"session":{"version":1}} {}`,
		string(bytes.Repeat([]byte(" "), maxStateBytes+1)),
	} {
		path := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		fixture := &manifestPersistenceFixture{persistenceFixture: persistenceFixture{value: 7}}
		if _, err := loadSessionState(path, fixture); err == nil || fixture.loads != 0 || fixture.value != 7 {
			t.Fatalf("invalid envelope reached experience state: calls=%d value=%d err=%v", fixture.loads, fixture.value, err)
		}
	}
}

func TestCompleteSessionAtomicRoundTripAndLegacyState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.json")
	fixture := &manifestPersistenceFixture{persistenceFixture: persistenceFixture{value: 42}}
	want := completeSessionManifest()
	if err := saveSessionState(path, fixture, want); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	oldInfo, err := old.Stat()
	if err != nil {
		t.Fatal(err)
	}
	fixture.value = 0
	got, err := loadSessionState(path, fixture)
	if err != nil || fixture.value != 42 || !reflect.DeepEqual(got, want) {
		t.Fatalf("complete session did not round-trip: state=%d session=%+v err=%v", fixture.value, got, err)
	}
	fixture.value = 81
	want.Media.Position = 96.5
	if err := saveSessionState(path, fixture, want); err != nil {
		t.Fatal(err)
	}
	newInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if newInfo.Mode().Perm() != 0600 || os.SameFile(oldInfo, newInfo) {
		t.Fatal("session replacement was not a private atomic file replacement")
	}
	stillOld, err := io.ReadAll(old)
	if err != nil || !bytes.Equal(stillOld, first) {
		t.Fatal("atomic session replacement modified the original open inode", err)
	}
	fixture.value = 0
	got, err = loadSessionState(path, fixture)
	if err != nil || fixture.value != 81 || !reflect.DeepEqual(got, want) {
		t.Fatal("replacement session lost application state", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	invalid := completeSessionManifest()
	invalid.Terminals[0].Key = "native:terminal-1"
	if err := saveSessionState(path, fixture, invalid); err == nil {
		t.Fatal("invalid session replaced a valid document")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed session save changed the previous document", err)
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(files) != 1 {
		t.Fatal("session writes leaked temporary files", err)
	}
	// Existing version1 documents remain readable and simply have no content
	// manifest. Saving one explicitly must not invent process restoration.
	fixture.value = 17
	if err := saveState(path, fixture); err != nil {
		t.Fatal(err)
	}
	fixture.value = 0
	if legacy, err := loadSessionState(path, fixture); err != nil || legacy != nil || fixture.value != 17 {
		t.Fatalf("legacy document lost compatibility: value=%d session=%+v err=%v", fixture.value, legacy, err)
	}
}

func TestSessionCheckpointEncodingUsesNonDisruptiveContract(t *testing.T) {
	fixture := &manifestPersistenceFixture{persistenceFixture: persistenceFixture{value: 42}}
	data, err := encodeState(fixture, completeSessionManifest(), true)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.checkpoints != 1 || fixture.saves != 0 || fixture.loads != 0 {
		t.Fatal("checkpoint encoder called the mutating save/load contract")
	}
	envelope, err := decodeStateEnvelope(data)
	if err != nil || envelope.Version != sessionEnvelopeVersion || !reflect.DeepEqual(envelope.Session, completeSessionManifest()) {
		t.Fatal("checkpoint omitted its session envelope", err)
	}
	if _, err := encodeState(&persistenceFixture{}, completeSessionManifest(), true); err == nil {
		t.Fatal("checkpoint silently fell back to mutating SaveState")
	}
}

func TestSessionRestorePlacementCountsSavedAndUnattachedLiveKeys(t *testing.T) {
	for _, savedCount := range []int{31, 32} {
		t.Run(fmt.Sprintf("%d remembered placements", savedCount), func(t *testing.T) {
			work, err := workspace.NewDesktop()
			if err != nil {
				t.Fatal(err)
			}
			defer work.Close()
			view := work.Document().View
			for i := 0; i < savedCount; i++ {
				view.Application.Layouts[i] = workspace.ApplicationPlacement{Key: fmt.Sprintf("remembered:%d", i)}
			}
			view.Application.Active, view.Application.Selected = "remembered:0", 1
			state, err := json.Marshal(map[string]any{"version": 1, "camera": view.Camera, "presentation": view.Presentation, "applications": view.Application})
			if err != nil {
				t.Fatal(err)
			}
			if err := work.LoadState(state); err != nil {
				t.Fatal(err)
			}
			provider := &hubProvider{}
			hub := newApplicationHub(provider)
			before := work.Document()
			if err := checkSessionPlacement(work, hub, "remembered:0"); err != nil {
				t.Fatalf("existing saved key could not reuse its slot: %v", err)
			}
			if savedCount == 31 {
				if err := checkSessionPlacement(work, hub, "native:terminal-4"); err != nil {
					t.Fatal("the final free slot was rejected before restoration", err)
				}
				// A previous restore publishes its loading surface before the host
				// attaches the hub to the workspace. It already needs that slot.
				provider.surfaces = []experience.ApplicationSurface{{ID: 1, Key: "native:terminal-4"}}
				if err := checkSessionPlacement(work, hub, "native:terminal-4"); err != nil {
					t.Fatal("published key was double-counted against its existing slot", err)
				}
			}
			if err := checkSessionPlacement(work, hub, "native:terminal-7"); err == nil {
				t.Fatal("restore overbooked saved placements and previously published surfaces")
			}
			if work.Document() != before || work.OwnsKeyboard() || work.CanUndo() || provider.focused != 0 || provider.sent != 0 || provider.resized != 0 {
				t.Fatal("capacity preflight attached/reconciled the workspace or changed focus/history")
			}
		})
	}
}
