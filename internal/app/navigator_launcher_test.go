package app

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise first launch, restoration and state overrides without opening a
// presenter or building the executable. The stub only records argv.
func TestNavigatorLauncherSeedsOnlyNewOrExplicitFreshSessions(t *testing.T) {
	source, err := os.ReadFile("../../scripts/run-navigator.sh")
	if err != nil {
		t.Fatal(err)
	}
	template, err := os.ReadFile("../../examples/navigator-desktop/workspace.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		existing   []string
		args       []string
		seed       bool
		state      string
		copied     bool
		pristine   bool
		buildFails bool
	}{
		{name: "first launch", seed: true, state: "dist/navigator/workspace.json", copied: true},
		{name: "saved empty session", existing: []string{"dist/navigator/workspace.json"}, state: "dist/navigator/workspace.json"},
		{name: "crash recovery only", existing: []string{"dist/navigator/workspace.json.autosave"}, state: "dist/navigator/workspace.json"},
		{name: "existing alternate session", existing: []string{"saved/session one.json"}, args: []string{"--state=saved/session one.json"}, state: "saved/session one.json"},
		{name: "alternate state argument", existing: []string{"saved/session one.json"}, args: []string{"--state", "saved/session one.json"}, state: "saved/session one.json"},
		{name: "new alternate ignores default session", existing: []string{"dist/navigator/workspace.json"}, args: []string{"--state=saved/new.json"}, seed: true, state: "saved/new.json", copied: true},
		{name: "new alternate spaced parent", args: []string{"--state", "saved/session one.json"}, seed: true, state: "saved/session one.json", copied: true},
		{name: "alternate recovery", existing: []string{"saved/other.json.autosave"}, args: []string{"-state=saved/other.json"}, state: "saved/other.json"},
		{name: "explicit fresh", existing: []string{"dist/navigator/workspace.json"}, args: []string{"--fresh"}, seed: true, state: "dist/navigator/workspace.json"},
		{name: "last fresh value wins", existing: []string{"dist/navigator/workspace.json"}, args: []string{"--fresh", "--fresh=false"}, state: "dist/navigator/workspace.json"},
		{name: "persistence disabled", existing: []string{"dist/navigator/workspace.json"}, args: []string{"--state="}, seed: true},
		{name: "pristine failed launch retry", existing: []string{"dist/navigator/workspace.json"}, seed: true, state: "dist/navigator/workspace.json", pristine: true},
		{name: "pristine retry with recovery", existing: []string{"dist/navigator/workspace.json", "dist/navigator/workspace.json.autosave"}, state: "dist/navigator/workspace.json", pristine: true},
		{name: "build failure creates no state", state: "dist/navigator/workspace.json", buildFails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write := func(name string, data []byte, mode os.FileMode) {
				t.Helper()
				path := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, mode); err != nil {
					t.Fatal(err)
				}
			}
			write("scripts/run-navigator.sh", source, 0755)
			write("examples/navigator-desktop/workspace.json", template, 0644)
			buildStub := "#!/bin/sh\nexit 0\n"
			if tc.buildFails {
				buildStub = "#!/bin/sh\nexit 1\n"
			}
			write("tools/go", []byte(buildStub), 0755)
			write("bin/worldr-shell", []byte("#!/usr/bin/env bash\nprintf '%s\\0' \"$@\" > \"$WORLDR_NAVIGATOR_TEST_ARGS\"\n"), 0755)
			existingBytes := make(map[string][]byte)
			for _, name := range tc.existing {
				data := []byte(`{"version":2,"session":{"version":1}}`)
				if tc.pristine && name == tc.state {
					data = template
				}
				write(name, data, 0600)
				existingBytes[name] = data
			}
			capture := filepath.Join(root, "arguments")
			cmd := exec.Command("bash", append([]string{filepath.Join(root, "scripts/run-navigator.sh")}, tc.args...)...)
			cmd.Env = append(os.Environ(), "PATH="+filepath.Join(root, "tools")+string(os.PathListSeparator)+os.Getenv("PATH"), "WORLDR_NAVIGATOR_TEST_ARGS="+capture)
			out, err := cmd.CombinedOutput()
			if tc.buildFails {
				if err == nil {
					t.Fatal("failed build unexpectedly launched")
				}
				for _, name := range []string{tc.state, "arguments"} {
					if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
						t.Fatalf("failed build created %q", name)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("launcher failed: %v\n%s", err, out)
			}
			for name, want := range existingBytes {
				got, err := os.ReadFile(filepath.Join(root, name))
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("launcher changed existing session %q", name)
				}
			}
			if tc.copied {
				path := filepath.Join(root, tc.state)
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, template) {
					t.Fatalf("initial project template was not copied to %q: %v", tc.state, err)
				}
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("initial session does not have private permissions")
				}
				assertNavigatorStarterGroups(t, got)
			} else if tc.state != "" && existingBytes[tc.state] == nil {
				if _, err := os.Stat(filepath.Join(root, tc.state)); !os.IsNotExist(err) {
					t.Fatal("recovery-only launch created a replacement main session")
				}
			}
			data, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Split(string(bytes.TrimSuffix(data, []byte{0})), "\x00")
			has := func(want string) bool {
				for _, arg := range args {
					if arg == want {
						return true
					}
				}
				return false
			}
			for _, want := range []string{"--experience=navigator", "--backend=nested", "--width=1280", "--height=820", "--state=dist/navigator/workspace.json"} {
				if !has(want) {
					t.Fatalf("missing navigator launch option %q: %q", want, args)
				}
			}
			for _, starter := range []string{"--project=" + root, "--research=examples/data/orbit-signals.csv", "--model=examples/models/mount.obj", "--axial"} {
				if has(starter) != tc.seed {
					t.Fatalf("starter %q presence=%v, want %v: %q", starter, has(starter), tc.seed, args)
				}
			}
			if got := args[len(args)-len(tc.args):]; strings.Join(got, "\x00") != strings.Join(tc.args, "\x00") {
				t.Fatalf("launcher changed caller overrides: %q", args)
			}
		})
	}
}

func assertNavigatorStarterGroups(t *testing.T, data []byte) {
	t.Helper()
	var envelope struct {
		Version int `json:"version"`
		State   struct {
			Applications struct {
				Spaces  []struct{ Name string } `json:"spaces"`
				Layouts []struct {
					Key   string
					Space int
				} `json:"layouts"`
			} `json:"applications"`
		} `json:"state"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	applications := envelope.State.Applications
	if envelope.Version != 1 || len(applications.Spaces) != 3 || len(applications.Layouts) != 4 {
		t.Fatal("starter workspace does not define three groups and four applications")
	}
	for i, want := range []string{"Code", "Research", "Design"} {
		if applications.Spaces[i].Name != want {
			t.Fatalf("starter group %d = %q, want %q", i, applications.Spaces[i].Name, want)
		}
	}
	placements := map[string]int{"native:project-browser": 0, "native:research-workbench": 1, "native:model": 2, "native:axial-07": 2}
	for _, layout := range applications.Layouts {
		space, ok := placements[layout.Key]
		if !ok || layout.Space != space {
			t.Fatalf("unexpected initial assignment %+v", layout)
		}
		delete(placements, layout.Key)
	}
	if len(placements) != 0 {
		t.Fatal("starter application assignment missing")
	}
}
