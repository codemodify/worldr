package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/workspace"
)

func TestFuturePanelsStarterLayoutLoadsAndKeepsRealApplicationKeys(t *testing.T) {
	data, err := os.ReadFile("../../examples/future-panels/workspace.json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Version      int             `json:"version"`
		ExperienceID string          `json:"experience_id"`
		State        json.RawMessage `json:"state"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Version != 1 || envelope.ExperienceID != "worldr.workspace" {
		t.Fatal("preset does not belong to Spatial Workspace")
	}
	w, err := workspace.NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.LoadState(envelope.State); err != nil {
		t.Fatal("starter layout is not a valid native workspace", err)
	}
	view := w.Document().View.Application
	if view.Reading || view.Overview || view.Active != "native:terminal" || view.Selected != 2 {
		t.Fatalf("preset did not open in the intended free spatial view: %+v", view)
	}
	keys := map[string]bool{"native:project-browser": true, "native:terminal": true, "native:research-workbench": true, "native:model": true, "native:axial-07": true}
	depths := map[float32]bool{}
	for _, placement := range view.Layouts {
		if placement.Key == "" {
			continue
		}
		if !keys[placement.Key] || placement.Width < 720 || placement.Height < 440 {
			t.Fatalf("invalid real application placement: %+v", placement)
		}
		delete(keys, placement.Key)
		depths[placement.Depth] = true
	}
	if len(keys) != 0 || len(depths) < 3 {
		t.Fatal("preset does not contain all five applications across multiple depths")
	}
}

func TestFuturePanelsCatalogSeedsFiveApplicationsWithoutReplacingSessions(t *testing.T) {
	template, err := os.ReadFile("../../examples/future-panels/workspace.json")
	if err != nil {
		t.Fatal(err)
	}
	d := experimentDefinitionForTest(t, "future-panels")
	for _, state := range []string{"new", "saved empty", "recovery only", "pristine", "pristine with recovery"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			write := func(path string, content []byte) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, content, 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(root, d.template), template)
			statePath := filepath.Join(root, d.state)
			existing := map[string][]byte{}
			if state == "saved empty" {
				existing[statePath] = []byte(`{"session":{}}`)
			}
			if strings.HasPrefix(state, "pristine") {
				existing[statePath] = template
			}
			if strings.Contains(state, "recovery") {
				existing[recoveryPath(statePath)] = []byte(`{"session":{}}`)
			}
			for path, data := range existing {
				write(path, data)
			}
			o, err := prepareExperiment(context.Background(), Options{Backend: "headless", FPS: 60}, d.ID, root, Options{}, false)
			if err != nil {
				t.Fatal(err)
			}
			seeded := o.Project == root && o.Terminal && o.Axial && len(o.Research) == 1 && len(o.Models) == 1
			if seeded != (state == "new" || state == "pristine") || currentExperiment(o) != d.ID || o.Experience != "workspace" || o.Skin != d.skin || len(o.NativeApplications) != 0 {
				t.Fatalf("incorrect starter application launch: %+v", o)
			}
			for path, want := range existing {
				actual, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(actual, want) {
					t.Fatalf("catalog changed existing session %q", path)
				}
			}
			if state == "recovery only" {
				if _, err := os.Stat(statePath); !os.IsNotExist(err) {
					t.Fatal("recovery replaced with a primary seed")
				}
			}
		})
	}
}

func TestFuturePanelsLauncherPreservesRestorationAndCallerOverrides(t *testing.T) {
	source, err := os.ReadFile("../../scripts/run-future-panels.sh")
	if err != nil {
		t.Fatal(err)
	}
	template, err := os.ReadFile("../../examples/future-panels/workspace.json")
	if err != nil {
		t.Fatal(err)
	}
	const defaultState = "dist/future-panels/workspace.json"
	for _, tc := range []struct {
		name, state                string
		existing, args             []string
		seed, pristine, buildFails bool
	}{
		{name: "first launch", state: defaultState, seed: true},
		{name: "saved empty", state: defaultState, existing: []string{defaultState}},
		{name: "recovery only", state: defaultState, existing: []string{defaultState + ".autosave"}},
		{name: "explicit fresh", state: defaultState, existing: []string{defaultState}, args: []string{"--fresh"}, seed: true},
		{name: "cancel fresh", state: defaultState, existing: []string{defaultState}, args: []string{"--fresh", "--fresh=false"}},
		{name: "new alternate", state: "saved/new session.json", existing: []string{defaultState}, args: []string{"--state=saved/new session.json"}, seed: true},
		{name: "existing alternate", state: "saved/session one.json", existing: []string{"saved/session one.json"}, args: []string{"--state", "saved/session one.json"}},
		{name: "state aliases", state: "saved/session one.json", existing: []string{"saved/session one.json"}, args: []string{"-state=saved/session one.json"}},
		{name: "last override wins", state: defaultState, existing: []string{defaultState}, args: []string{"--state=unused.json", "--state", defaultState, "--width=1600", "--terminal=false"}},
		{name: "disabled persistence", args: []string{"--state="}, seed: true},
		{name: "pristine retry", state: defaultState, existing: []string{defaultState}, seed: true, pristine: true},
		{name: "pristine recovery", state: defaultState, existing: []string{defaultState, defaultState + ".autosave"}, pristine: true},
		{name: "build failure", state: defaultState, buildFails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write := func(path string, data []byte, mode os.FileMode) {
				t.Helper()
				full := filepath.Join(root, path)
				if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, data, mode); err != nil {
					t.Fatal(err)
				}
			}
			write("scripts/run-future-panels.sh", source, 0755)
			write("examples/future-panels/workspace.json", template, 0644)
			build := "#!/bin/sh\nexit 0\n"
			if tc.buildFails {
				build = "#!/bin/sh\nexit 1\n"
			}
			write("tools/go", []byte(build), 0755)
			write("bin/worldr-desktop", []byte("#!/usr/bin/env bash\nprintf '%s\\0' \"$@\" > \"$WORLDR_PANELS_TEST_ARGS\"\n"), 0755)
			existing := map[string][]byte{}
			for _, path := range tc.existing {
				data := []byte(`{"version":2,"session":{"version":1}}`)
				if tc.pristine && path == tc.state {
					data = template
				}
				existing[path] = data
				write(path, data, 0600)
			}
			capture := filepath.Join(root, "arguments")
			cmd := exec.Command("bash", append([]string{filepath.Join(root, "scripts/run-future-panels.sh")}, tc.args...)...)
			cmd.Env = append(os.Environ(), "PATH="+filepath.Join(root, "tools")+string(os.PathListSeparator)+os.Getenv("PATH"), "WORLDR_PANELS_TEST_ARGS="+capture)
			out, err := cmd.CombinedOutput()
			if tc.buildFails {
				if err == nil {
					t.Fatal("failed build launched desktop")
				}
				for _, path := range []string{tc.state, "arguments"} {
					if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
						t.Fatalf("failed build created %q", path)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("launcher failed: %v\n%s", err, out)
			}
			for path, want := range existing {
				actual, err := os.ReadFile(filepath.Join(root, path))
				if err != nil || !bytes.Equal(actual, want) {
					t.Fatalf("launcher changed existing %q", path)
				}
			}
			if tc.state != "" && existing[tc.state] == nil {
				path := filepath.Join(root, tc.state)
				actual, err := os.ReadFile(path)
				if existing[tc.state+".autosave"] != nil {
					if !os.IsNotExist(err) {
						t.Fatal("recovery-only session received a replacement primary seed")
					}
				} else {
					if err != nil || !bytes.Equal(actual, template) {
						t.Fatal("new session did not receive the authored layout", err)
					}
					if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
						t.Fatal("new session is not private", err)
					}
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
			for _, want := range []string{"--experience=workspace", "--backend=nested", "--width=1440", "--height=900", "--skin=future-panels", "--state=" + defaultState} {
				if !has(want) {
					t.Fatalf("missing desktop option %q", want)
				}
			}
			for _, starter := range []string{"--project=" + root, "--terminal", "--research=examples/data/orbit-signals.csv", "--model=examples/models/mount.obj", "--axial"} {
				if has(starter) != tc.seed {
					t.Fatalf("starter %q presence=%v, expected=%v: %q", starter, has(starter), tc.seed, args)
				}
			}
			if strings.Join(args[len(args)-len(tc.args):], "\x00") != strings.Join(tc.args, "\x00") {
				t.Fatal("caller overrides did not remain last in argv")
			}
		})
	}
}
