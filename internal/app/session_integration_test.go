//go:build linux && cgo

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeapps"
	"github.com/codemodify/worldr/internal/photoapp"
	"github.com/codemodify/worldr/internal/projectapp"
	"github.com/codemodify/worldr/internal/workspace"
)

type sessionIntegrationSeed struct {
	path, root, terminalDirectory, photo string
	view                                 workspace.ViewState
	manifest                             *sessionManifest
}

func sessionShellOptions(marker string) nativeapps.Options {
	return nativeapps.Options{Command: "/bin/sh", Args: []string{"-c", `printf '\033]2;%s / %s\007' "$1" "$PWD"; IFS= read -r value`, "session-fixture", marker}}
}

func sessionSurface(hub *applicationHub, key string) experience.ApplicationSurface {
	for _, surface := range hub.Surfaces() {
		if surface.Key == key {
			return surface
		}
	}
	return experience.ApplicationSurface{}
}

func waitSession(t *testing.T, work *workspace.Workspace, hub *applicationHub, description string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := hub.Poll(); err != nil {
			t.Fatal(err)
		}
		work.Update(0)
		if ready() {
			return
		}
		time.Sleep(3 * time.Millisecond)
	}
	t.Fatalf("session did not reach %s; surfaces=%+v", description, hub.Surfaces())
}

func makeSessionIntegrationSeed(t *testing.T) sessionIntegrationSeed {
	t.Helper()
	root, photoPath := workspacePhotoFixture(t)
	subfolder, shellFolder := filepath.Join(root, "samples λ"), filepath.Join(root, "shell working directory")
	for _, path := range []string{subfolder, shellFolder} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(subfolder, "selected.txt"), []byte("Saved file selection.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	navigation := projectapp.SessionState{Root: root, Directory: filepath.Base(subfolder), Selected: "selected.txt"}
	project, err := projectapp.NewSession(navigation)
	if err != nil {
		t.Fatal(err)
	}
	terminals := nativeapps.NewManager(sessionShellOptions("original"))
	photos := photoapp.NewManager()
	hub := newApplicationHub(project, terminals, photos)
	defer func() {
		if err := hub.Close(); err != nil {
			t.Error(err)
		}
	}()
	work, err := workspace.NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Close()
	for _, folder := range []string{shellFolder, root, root} {
		directory, err := os.Open(folder)
		if err != nil {
			t.Fatal(err)
		}
		_, err = terminals.LaunchTerminalInDirectory(directory)
		_ = directory.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	photo, err := os.Open(photoPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := photos.OpenFile(photo, "display label.png"); err != nil {
		_ = photo.Close()
		t.Fatal(err)
	}
	work.SetApplications(hub)
	defer work.SetApplications(nil)
	work.Draw(960, 600)
	waitSession(t, work, hub, "seed photo, Files navigation and shell output", func() bool {
		state, ok := project.SessionState()
		return ok && state == navigation && sessionSurface(hub, "native:terminal").Title == "Native terminal / original / "+shellFolder &&
			sessionSurface(hub, "native:terminal-3").Title == "Native terminal / original / "+root && decodedPhotoPixels(photos.Surfaces()[0].Texture)
	})
	// Register and then close slot 2. Its saved placement remains, while live
	// terminal keys 1 and 3 must never be compacted across the missing slot.
	hub.CloseApplication(sessionSurface(hub, "native:terminal-2").ID)
	if err := hub.Poll(); err != nil {
		t.Fatal(err)
	}
	work.Update(0)
	for _, action := range []workspace.Action{
		{Kind: workspace.SelectApplication, ApplicationKey: "native:terminal"},
		{Kind: workspace.MoveApplications, DeltaX: 1.4, DeltaY: -.3, DeltaDepth: -.7},
		{Kind: workspace.ToggleApplicationSize},
		{Kind: workspace.SelectApplication, ApplicationKey: "native:photo-viewer"},
		{Kind: workspace.MoveApplications, DeltaX: -1.1, DeltaY: .6, DeltaDepth: -1.2},
		{Kind: workspace.SelectApplication, ApplicationKey: "native:terminal-3", Additive: true},
		{Kind: workspace.GroupApplications},
		{Kind: workspace.MoveApplications, DeltaX: .3, DeltaY: .2},
		{Kind: workspace.OrbitCamera, DeltaX: 12, DeltaY: -7},
		{Kind: workspace.ZoomCamera, DeltaZoom: .15},
		{Kind: workspace.ToggleApplicationReading},
	} {
		if err := work.Dispatch(action); err != nil {
			t.Fatal(err)
		}
	}
	work.Draw(960, 600)
	session := &workspaceSession{work: work, project: project, terminals: terminals, photos: photos}
	statePath := filepath.Join(t.TempDir(), "workspace.json")
	if err := session.save(statePath, nil); err != nil {
		t.Fatal(err)
	}
	manifest := session.snapshot()
	if len(manifest.Terminals) != 2 || manifest.Terminals[0].Key != "native:terminal" || manifest.Terminals[1].Key != "native:terminal-3" || manifest.Photo == nil || manifest.Photo.Path != photoPath || manifest.Project == nil || *manifest.Project != navigation {
		t.Fatalf("seed lost its resource manifest: %+v", manifest)
	}
	return sessionIntegrationSeed{path: statePath, root: root, terminalDirectory: shellFolder, photo: photoPath, view: work.Document().View, manifest: manifest}
}

func restoreSessionIntegrationSeed(t *testing.T, seed sessionIntegrationSeed) (*workspaceSession, *workspace.Workspace, *applicationHub, []error, string) {
	t.Helper()
	work, err := workspace.NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := loadSessionState(seed.path, work)
	if err != nil {
		_ = work.Close()
		t.Fatal(err)
	}
	project, err := openSessionProject("", saved.Project)
	if err != nil {
		_ = work.Close()
		t.Fatal(err)
	}
	terminals := nativeapps.NewManager(sessionShellOptions("fresh configured shell"))
	photos := photoapp.NewManager()
	hub := newApplicationHub(project, terminals, photos)
	t.Cleanup(func() {
		work.SetApplications(nil)
		_ = work.Close()
		if err := hub.Close(); err != nil {
			t.Error(err)
		}
	})
	session := &workspaceSession{work: work, project: project, terminals: terminals, photos: photos}
	var output bytes.Buffer
	// Publish every restored/loading surface before workspace reconciliation,
	// otherwise an absent early provider can replace the saved active key.
	failures := session.restore(saved, hub, &output)
	work.SetApplications(hub)
	work.Draw(960, 600)
	return session, work, hub, failures, output.String()
}

func TestWorkspaceSessionRoundTripRestoresResourcesKeysAndReadLayout(t *testing.T) {
	seed := makeSessionIntegrationSeed(t)
	data, err := os.ReadFile(seed.path)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := decodeStateEnvelope(data)
	if err != nil || envelope.Version != sessionEnvelopeVersion || envelope.Session == nil {
		t.Fatalf("full state did not save a session envelope: %v", err)
	}
	manifestJSON, err := json.Marshal(envelope.Session)
	if err != nil {
		t.Fatal(err)
	}
	// A saved terminal may contain a bounded inert task recipe, but a session
	// never serializes process launch arguments, environments or identities.
	for _, forbidden := range []string{`"args"`, `"env"`, `"pid"`, "original", "/bin/sh"} {
		if bytes.Contains(manifestJSON, []byte(forbidden)) {
			t.Fatalf("session persisted process execution data %q", forbidden)
		}
	}
	session, work, hub, failures, output := restoreSessionIntegrationSeed(t, seed)
	if len(failures) != 0 {
		t.Fatalf("restore failed: %v\n%s", failures, output)
	}
	if work.Document().View != seed.view || work.OwnsKeyboard() || hub.focused != 0 {
		t.Fatal("initial provider reconciliation lost saved Read/selection/layout/camera or granted keyboard focus")
	}
	waitSession(t, work, hub, "restored resources", func() bool {
		navigation, ok := session.project.SessionState()
		photo := sessionSurface(hub, "native:photo-viewer")
		return ok && navigation == *seed.manifest.Project && photo.ID != 0 && decodedPhotoPixels(photo.Texture) &&
			sessionSurface(hub, "native:terminal").Title == "Native terminal / fresh configured shell / "+seed.terminalDirectory &&
			sessionSurface(hub, "native:terminal-3").Title == "Native terminal / fresh configured shell / "+seed.root
	})
	if len(hub.Surfaces()) != 4 || sessionSurface(hub, "native:terminal-2").ID != 0 || work.Document().View != seed.view {
		t.Fatal("asynchronous restore compacted terminal gaps, added windows, or changed saved view")
	}
	if actual := session.snapshot(); !reflect.DeepEqual(actual, seed.manifest) {
		t.Fatalf("restored resources did not round-trip manifest:\n got %+v\nwant %+v", actual, seed.manifest)
	}
	if err := session.save(seed.path, nil); err != nil {
		t.Fatal(err)
	}
	second, secondWork, secondHub, failures, output := restoreSessionIntegrationSeed(t, seed)
	if len(failures) != 0 || len(secondHub.Surfaces()) != 4 || secondWork.Document().View != seed.view {
		t.Fatalf("second clean restore duplicated windows or lost saved view: %v %s", failures, output)
	}
	if len(second.terminals.SessionTerminals()) != 2 {
		t.Fatal("second clean session multiplied terminals")
	}
}

func TestWorkspaceSessionMissingResourcesRetainRecordsUntilForgotten(t *testing.T) {
	seed := makeSessionIntegrationSeed(t)
	if err := os.Remove(seed.terminalDirectory); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(seed.photo); err != nil {
		t.Fatal(err)
	}
	session, work, hub, failures, output := restoreSessionIntegrationSeed(t, seed)
	if len(failures) != 2 || !strings.Contains(output, "terminal folder") || !strings.Contains(output, "photo") {
		t.Fatalf("missing resources were not independently reported: %v / %s", failures, output)
	}
	waitSession(t, work, hub, "available siblings despite missing resources", func() bool {
		navigation, ok := session.project.SessionState()
		return ok && navigation == *seed.manifest.Project && sessionSurface(hub, "native:terminal-3").Title == "Native terminal / fresh configured shell / "+seed.root
	})
	if len(hub.Surfaces()) != 2 || sessionSurface(hub, "native:terminal").ID != 0 || sessionSurface(hub, "native:photo-viewer").ID != 0 {
		t.Fatal("missing resources created placeholder applications or stopped live siblings")
	}
	pending := session.snapshot()
	if pending.Photo == nil || *pending.Photo != *seed.manifest.Photo || len(pending.Terminals) != 2 {
		t.Fatalf("unavailable resource records were dropped from snapshot: %+v", pending)
	}
	if err := session.save(seed.path, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(seed.path)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := decodeStateEnvelope(data)
	if err != nil || envelope.Session.Photo == nil || len(envelope.Session.Terminals) != 2 {
		t.Fatalf("ordinary save erased pending resource associations: %v", err)
	}
	// Opening a replacement explicitly resolves the missing photo association.
	// A later close must not revive the unavailable old photo merely because
	// its placement remains in the workspace's saved layout.
	_, replacementPath := workspacePhotoFixture(t)
	replacementFile, err := os.Open(replacementPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.photos.OpenFile(replacementFile, "replacement.png"); err != nil {
		_ = replacementFile.Close()
		t.Fatal(err)
	}
	waitSession(t, work, hub, "replacement for unavailable photo", func() bool {
		photo := sessionSurface(hub, "native:photo-viewer")
		return photo.ID != 0 && decodedPhotoPixels(photo.Texture)
	})
	session.reconcile()
	if state := session.snapshot(); state.Photo == nil || state.Photo.Path != replacementPath {
		t.Fatal("replacement did not supersede the unavailable photo record")
	}
	hub.CloseApplication(sessionSurface(hub, "native:photo-viewer").ID)
	if err := hub.Poll(); err != nil {
		t.Fatal(err)
	}
	work.Update(0)
	session.reconcile()
	if state := session.snapshot(); state.Photo != nil || len(state.Terminals) != 2 {
		t.Fatal("closing replacement revived old missing photo or erased an unrelated pending terminal")
	}
	photoPlacementRetained := false
	for _, key := range work.SavedApplicationKeys() {
		photoPlacementRetained = photoPlacementRetained || key == "native:photo-viewer"
	}
	if !photoPlacementRetained {
		t.Fatal("replacement close unexpectedly forgot its saved layout")
	}
	if err := work.Dispatch(workspace.Action{Kind: workspace.ForgetClosedPlacements}); err != nil {
		t.Fatal(err)
	}
	for _, key := range work.SavedApplicationKeys() {
		if key == "native:terminal" || key == "native:terminal-2" || key == "native:photo-viewer" {
			t.Fatalf("explicit forget retained unavailable placement %q", key)
		}
	}
	forgotten := session.snapshot()
	if forgotten.Photo != nil || len(forgotten.Terminals) != 1 || forgotten.Terminals[0].Key != "native:terminal-3" || forgotten.Project == nil {
		t.Fatalf("forget did not release pending records while retaining live siblings: %+v", forgotten)
	}
	if err := session.save(seed.path, nil); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(seed.path)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err = decodeStateEnvelope(data)
	if err != nil || envelope.Session.Photo != nil || len(envelope.Session.Terminals) != 1 {
		t.Fatalf("forgotten resources returned in saved manifest: %v", err)
	}
}

type sessionRunProbe struct {
	*workspace.Workspace
	applications experience.Applications
	seen         map[string]bool
	decodedPhoto bool
	readyShells  map[string]bool
}

func (p *sessionRunProbe) SetApplications(applications experience.Applications) {
	p.applications = applications
	p.Workspace.SetApplications(applications)
}

func (p *sessionRunProbe) Update(dt time.Duration) {
	p.Workspace.Update(dt)
	if p.applications == nil {
		return
	}
	for _, surface := range p.applications.Surfaces() {
		p.seen[surface.Key] = true
		if surface.Key == "native:photo-viewer" {
			p.decodedPhoto = p.decodedPhoto || decodedPhotoPixels(surface.Texture)
		}
		if surface.AppID == "worldr.native-terminal" && strings.HasPrefix(surface.Title, "Native terminal / run / ") {
			p.readyShells[surface.Key] = true
		}
	}
}

func TestRunStateOnlyRestoresNativeContentWithoutMultiplyingTerminalsGPU(t *testing.T) {
	if os.Getenv("WORLDR_TEST_GPU") != "1" {
		t.Skip("set WORLDR_TEST_GPU=1 for full state-only native startup")
	}
	seed := makeSessionIntegrationSeed(t)
	shell := filepath.Join(t.TempDir(), "fresh-session-shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf '\\033]2;run / %s\\007' \"$PWD\"\nIFS= read -r value\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)
	for launch := 0; launch < 2; launch++ {
		options, err := Parse([]string{"--backend=headless", "--width=960", "--height=600", "--frames=8", "--autosave=0", "--state=" + seed.path}, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if options.Terminal || options.Project != "" {
			t.Fatal("state-only fixture accidentally requested explicit startup applications")
		}
		var probe *sessionRunProbe
		var output bytes.Buffer
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = Run(ctx, &output, options, func() (experience.Experience, error) {
			work, err := workspace.NewDesktop()
			if err != nil {
				return nil, err
			}
			probe = &sessionRunProbe{Workspace: work, seen: map[string]bool{}, readyShells: map[string]bool{}}
			return probe, nil
		})
		cancel()
		if err != nil {
			t.Fatalf("state-only launch %d: %v\n%s", launch+1, err, output.String())
		}
		if len(probe.seen) != 4 || !probe.seen["native:project-browser"] || !probe.seen["native:photo-viewer"] || len(probe.readyShells) != 2 || !probe.readyShells["native:terminal"] || !probe.readyShells["native:terminal-3"] || !probe.decodedPhoto {
			t.Fatalf("state-only launch %d did not restore exactly its native content: seen=%v shells=%v photo=%v\n%s", launch+1, probe.seen, probe.readyShells, probe.decodedPhoto, output.String())
		}
		data, err := os.ReadFile(seed.path)
		if err != nil {
			t.Fatal(err)
		}
		envelope, err := decodeStateEnvelope(data)
		if err != nil || envelope.Session == nil || len(envelope.Session.Terminals) != 2 || envelope.Session.Photo == nil || envelope.Session.Project == nil {
			t.Fatalf("state-only launch %d lost its native session on clean save: %v", launch+1, err)
		}
	}
}
