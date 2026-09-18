package app

import (
	"io"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/workspace"
)

func TestHostedAxialLaunchPlacementAndSessionRestore(t *testing.T) {
	work, err := workspace.NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Close()
	provider, err := workspace.NewAxialApplicationManager()
	if err != nil {
		t.Fatal(err)
	}
	hub := newApplicationHub(provider)
	defer hub.Close()
	work.SetApplications(hub)
	defer work.SetApplications(nil)
	key, err := hub.LaunchApplication("axial")
	if err != nil || key != workspace.AxialApplicationKey {
		t.Fatalf("launch: %q %v", key, err)
	}
	work.PlaceNewApplication(key)
	surface := hub.Surfaces()[0]
	hub.Focus(surface.ID)
	hub.Send(surface.ID, experience.Event{Kind: experience.KeyInput, Key: experience.KeyE, Pressed: true})

	manifest := (&workspaceSession{work: work, axial: provider}).snapshot()
	if manifest.Axial == nil || !manifest.Axial.Exploded {
		t.Fatalf("hosted state missing from manifest: %+v", manifest)
	}

	restoredWork, err := workspace.NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	defer restoredWork.Close()
	state, err := work.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	if err := restoredWork.LoadState(state); err != nil {
		t.Fatal(err)
	}
	restoredProvider, err := workspace.NewAxialApplicationManager()
	if err != nil {
		t.Fatal(err)
	}
	restoredHub := newApplicationHub(restoredProvider)
	defer restoredHub.Close()
	restored := &workspaceSession{work: restoredWork, axial: restoredProvider}
	if failures := restored.restore(manifest, restoredHub, io.Discard); len(failures) != 0 {
		t.Fatalf("restore: %v", failures)
	}
	if surfaces := restoredHub.Surfaces(); len(surfaces) != 1 || surfaces[0].Key != workspace.AxialApplicationKey || surfaces[0].Spatial == nil {
		t.Fatalf("restored hosted surface: %+v", surfaces)
	}
	if got := restored.snapshot().Axial; got == nil || *got != *manifest.Axial {
		t.Fatalf("state did not round-trip: got=%+v want=%+v", got, manifest.Axial)
	}
}
