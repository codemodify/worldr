package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/projectapp"
	"github.com/codemodify/worldr/internal/researchapp"
	"github.com/codemodify/worldr/internal/workspace"
)

func TestFilesOpensDatasetIntoLinkedNativeResearchWorkbench(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "experiment.csv")
	if err := os.WriteFile(path, []byte("time,signal,control\n0,2,3\n1,5,8\n2,7,13\n"), 0600); err != nil {
		t.Fatal(err)
	}
	browser, err := projectapp.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	research := researchapp.NewManager()
	hub := newApplicationHub(browser, research)
	work, err := workspace.NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Close()
	defer hub.Close()
	work.SetApplications(hub)
	defer work.SetApplications(nil)
	connectFileBrowserWithResearch(browser, nil, nil, research, hub, work)
	deadline := time.Now().Add(5 * time.Second)
	selected := false
	for time.Now().Before(deadline) {
		if err := browser.Poll(); err != nil {
			t.Fatal(err)
		}
		if state, ok := browser.SessionState(); ok && state.Selected == filepath.Base(path) {
			selected = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !selected {
		t.Fatal("Files did not finish loading its dataset entry")
	}
	work.Draw(1440, 900)
	browser.Focus(1)
	browser.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: true})
	for time.Now().Before(deadline) {
		if err := hub.Poll(); err != nil {
			t.Fatal(err)
		}
		if surfaces := research.Surfaces(); len(surfaces) == 1 && surfaces[0].Spatial != nil && len(surfaces[0].Spatial.Objects) >= 4 {
			if surfaces[0].Key != "native:research-workbench" || surfaces[0].AppID != "worldr.research-workbench" {
				t.Fatal("research surface identity changed", surfaces[0])
			}
			if hub.focused != 0 {
				t.Fatal("opening a dataset stole application focus")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("Files did not open the dataset in the research workbench")
}
