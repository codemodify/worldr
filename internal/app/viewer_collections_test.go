//go:build linux

package app

import (
	"github.com/codemodify/worldr/internal/mediaapp"
	"github.com/codemodify/worldr/internal/photoapp"
	"github.com/codemodify/worldr/internal/workspace"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMultiplePhotosResumeExactSlotsAndNamedSpaces(t *testing.T) {
	_, path := workspacePhotoFixture(t)
	w, err := workspace.NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	photos := photoapp.NewCollection()
	hub := newApplicationHub(photos)
	defer hub.Close()
	for i := 0; i < 3; i++ {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = photos.OpenFile(file, filepath.Base(path)); err != nil {
			file.Close()
			t.Fatal(err)
		}
	}
	w.SetApplications(hub)
	w.Draw(1440, 900)
	for _, a := range []workspace.Action{{Kind: workspace.CreateSpace, SpaceName: "Imaging"}, {Kind: workspace.SelectApplication, ApplicationKey: "native:photo-viewer-3"}, {Kind: workspace.MoveToSpace, Space: 1}} {
		if err = w.Dispatch(a); err != nil {
			t.Fatal(err)
		}
	}
	for _, surface := range hub.Surfaces() {
		if surface.Key == "native:photo-viewer-2" {
			hub.CloseApplication(surface.ID)
			break
		}
	}
	w.Update(0)
	session := &workspaceSession{work: w, photos: photos}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err = hub.Poll(); err != nil {
			t.Fatal(err)
		}
		if decodedPhotoPixels(photos.Surfaces()[1].Texture) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	saved := session.snapshot()
	if saved.Photo == nil || len(saved.Photos) != 1 || saved.Photos[0].Key != "native:photo-viewer-3" {
		t.Fatalf("independent photo slots not saved: %+v", saved)
	}
	statePath := filepath.Join(t.TempDir(), "workspace.json")
	if err = session.save(statePath, nil); err != nil {
		t.Fatal(err)
	}
	fresh, err := workspace.NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	manifest, err := loadSessionState(statePath, fresh)
	if err != nil {
		t.Fatal(err)
	}
	restoredPhotos := photoapp.NewCollection()
	restoredHub := newApplicationHub(restoredPhotos)
	defer restoredHub.Close()
	restored := &workspaceSession{work: fresh, photos: restoredPhotos}
	if failures := restored.restore(manifest, restoredHub, io.Discard); len(failures) != 0 {
		t.Fatal(failures)
	}
	fresh.SetApplications(restoredHub)
	fresh.Draw(1440, 900)
	surfaces := restoredPhotos.Surfaces()
	if len(surfaces) != 2 || surfaces[0].Key != "native:photo-viewer" || surfaces[1].Key != "native:photo-viewer-3" {
		t.Fatal("restore compacted slots", surfaces)
	}
	if fresh.Document() != w.Document() || fresh.OwnsKeyboard() {
		t.Fatal("restore changed independent space layout or stole focus")
	}
}
func TestViewerManifestRejectsDuplicatesAndOverCapacity(t *testing.T) {
	s := &sessionManifest{Version: 1, Photo: &photoapp.SessionState{Path: "/tmp/a.png"}, Photos: []photoapp.WindowState{{Key: "native:photo-viewer", SessionState: photoapp.SessionState{Path: "/tmp/b.png"}}}}
	if s.validate() == nil {
		t.Fatal("legacy slot duplicates were accepted")
	}
	s = &sessionManifest{Version: 1, Videos: []mediaapp.WindowState{{Key: "native:media-player-5", SessionState: mediaapp.SessionState{Path: "/tmp/a.mp4", Volume: 70}}}}
	if s.validate() == nil {
		t.Fatal("out of budget video slot accepted")
	}
}
