package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

func TestExperimentCatalogOrderAndIsolation(t *testing.T) {
	var ids []string
	states := map[string]bool{}
	for _, entry := range experimentCatalog() {
		ids = append(ids, entry.ID)
		if entry.state == "" || states[entry.state] {
			t.Fatalf("shared or absent state: %s", entry.ID)
		}
		states[entry.state] = true
	}
	want := []string{"axial", "workspace", "skin-studio", "merrick", "advanced", "hologram", "plasma", "future-panels", "navigator"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("experiment order: %v", ids)
	}
	if got := currentExperiment(Options{Experience: "workspace", NativeApplications: []string{"/a checkout/bin/advanced-desktop"}}); got != "advanced" {
		t.Fatal(got)
	}
}

func TestExperimentPreparationDoesNotCarryUnrelatedAppsOrExports(t *testing.T) {
	base, err := Parse([]string{"--experience=navigator", "--backend=headless", "--project=.", "--skin=merrick", "--fresh", "--snapshot=old.png", "--frames=9", "--terminal"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	next, err := prepareExperiment(context.Background(), base, "plasma", root, Options{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if next.Experience != "plasma" || next.State != filepath.Join(root, "dist/plasma/playground.json") || next.Backend != base.Backend || next.GPUMemoryMiB != base.GPUMemoryMiB {
		t.Fatalf("incorrect launch: %+v", next)
	}
	if next.Project != "" || next.Skin != "" || next.Fresh || next.Terminal || next.Snapshot != "" || next.Frames != 0 {
		t.Fatalf("leaked settings: %+v", next)
	}
	if _, err := prepareExperiment(context.Background(), base, "missing", root, Options{}, false); err == nil {
		t.Fatal("unknown experiment accepted")
	}
	if _, err := prepareExperiment(context.Background(), base, "plasma", "", Options{}, false); err == nil {
		t.Fatal("missing checkout accepted")
	}
}

func TestExperimentNavigatorSeedPreservesSavedAndRecoverySessions(t *testing.T) {
	for _, state := range []string{"new", "saved empty", "recovery only", "pristine"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			d := experimentDefinitionForTest(t, "navigator")
			template := []byte(`{"version":1,"seed":true}`)
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
			o := Options{State: filepath.Join(root, d.state)}
			switch state {
			case "saved empty":
				write(o.State, []byte(`{"session":{}}`))
			case "recovery only":
				write(recoveryPath(o.State), []byte(`{"session":{}}`))
			case "pristine":
				write(o.State, template)
			}
			if err := seedExperiment(root, d, &o); err != nil {
				t.Fatal(err)
			}
			seeded := o.Project == root && o.Axial && len(o.Research) == 1 && len(o.Models) == 1
			if seeded != (state == "new" || state == "pristine") {
				t.Fatalf("unexpected seeds: %+v", o)
			}
			if state == "recovery only" {
				if _, err := os.Stat(o.State); !os.IsNotExist(err) {
					t.Fatal("recovery overwritten with a primary seed")
				}
			}
		})
	}
}

func experimentDefinitionForTest(t *testing.T, id string) experimentDefinition {
	t.Helper()
	for _, d := range experimentCatalog() {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("experiment %q is missing", id)
	return experimentDefinition{}
}

func TestFuturePanelsIdentitySurvivesCustomStateAndAppearance(t *testing.T) {
	initial := Options{Experience: "workspace", Skin: "future-panels", State: filepath.Join(t.TempDir(), "custom filename.json"), Project: "project", Terminal: true, Axial: true}
	if currentExperiment(initial) != "future-panels" {
		t.Fatal("built-in skin did not identify Future Panels")
	}
	resume := resumeExperiment(initial)
	if currentExperiment(resume) != "future-panels" || resume.Skin != "" || resume.Project != "" || resume.Terminal || resume.Axial {
		t.Fatalf("resume lost experiment identity or replayed launch overrides: %+v", resume)
	}
	resume.Skin = "merrick"
	if currentExperiment(resume) != "future-panels" {
		t.Fatal("changing appearance changed the experiment identity")
	}
	next, err := prepareExperiment(context.Background(), Options{}, "future-panels", t.TempDir(), resumeExperiment(resume), true)
	if err != nil || next.State != initial.State || next.Skin != "" || currentExperiment(next) != "future-panels" {
		t.Fatalf("return did not retain custom Future Panels session: %+v, %v", next, err)
	}
	if currentExperiment(Options{Experience: "workspace", State: "arbitrary/future-panels.json"}) != "workspace" {
		t.Fatal("unrelated state path was interpreted as an experiment selection")
	}
	if currentExperiment(Options{Experience: "navigator", Skin: "future-panels"}) != "navigator" {
		t.Fatal("appearance overrode explicit Navigator experience")
	}
}

func TestExperimentReturnKeepsCustomStateWithoutReopeningClosedApps(t *testing.T) {
	old := Options{Experience: "navigator", State: filepath.Join(t.TempDir(), "my-session.json"), Skin: "merrick", Project: "old-project", Models: []string{"old-model"}, Axial: true, Terminal: true, Fresh: true}
	old.Application, old.Applications, old.Launches = "closed-app", []string{"closed-app"}, []ApplicationLaunch{{Command: "closed-app"}}
	resume := resumeExperiment(old)
	next, err := prepareExperiment(context.Background(), Options{}, "navigator", t.TempDir(), resume, true)
	if err != nil {
		t.Fatal(err)
	}
	if next.State != old.State || next.Skin != "" || next.Project != "" || next.Models != nil || next.Axial || next.Terminal || next.Fresh {
		t.Fatalf("incorrect return: %+v", next)
	}
	if next.Application != "" || len(next.Applications) != 0 || len(next.Launches) != 0 {
		t.Fatal("return replayed original application commands")
	}
}

func TestExperimentWithoutPersistenceUsesPrivateTemporarySession(t *testing.T) {
	var state string
	want := errors.New("stop before presenter")
	err := RunExperiments(context.Background(), io.Discard, Options{Backend: "headless", Experience: "navigator"}, func(o Options) (experience.Experience, error) {
		state = o.State
		if state == "" || o.Autosave != 0 {
			t.Fatal("missing session-local state")
		}
		info, err := os.Stat(filepath.Dir(state))
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatal("temporary session is not private", err)
		}
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(state)); !os.IsNotExist(err) {
		t.Fatal("temporary session survived exit", err)
	}
}

func TestExperimentSwitchWaitsForPreparationAndSuccessfulSave(t *testing.T) {
	for _, failure := range []string{"prepare", "save", "none"} {
		t.Run(failure, func(t *testing.T) {
			result := make(chan experimentResult, 1)
			s := &experimentSession{menu: &experimentMenu{open: true, busy: true}, pending: result, cancel: func() {}}
			saved := 0
			save := func() error {
				saved++
				if failure == "save" {
					return errors.New("disk full")
				}
				return nil
			}
			if s.poll(save) || saved != 0 {
				t.Fatal("left before preparation finished")
			}
			r := experimentResult{options: Options{Experience: "plasma"}}
			if failure == "prepare" {
				r.err = errors.New("build failed")
			}
			result <- r
			switched := s.poll(save)
			if switched != (failure == "none") || (s.next != nil) != switched {
				t.Fatal("switched despite failure")
			}
			if failure == "prepare" && saved != 0 {
				t.Fatal("saved before build succeeded")
			}
			if failure != "prepare" && saved != 1 {
				t.Fatal("missing save")
			}
			if failure != "none" && (!s.menu.open || s.menu.busy || !strings.Contains(s.menu.status, "Could not switch")) {
				t.Fatal("failed switch did not remain usable")
			}
		})
	}
}
