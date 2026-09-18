package app

import (
	"reflect"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/projectapp"
)

func TestFilesLauncherCatalogIncludesNativeFileBackedApps(t *testing.T) {
	f := &filesProvider{}
	want := []experience.ApplicationLaunch{
		{Kind: "files", Title: "Open Files"},
		{Kind: "photo", Title: "Open photo"},
		{Kind: "media", Title: "Open video"},
		{Kind: "model", Title: "Inspect 3D model"},
		{Kind: "research", Title: "Open research data"},
	}
	if got := f.ApplicationLaunches(); !reflect.DeepEqual(got, want) {
		t.Fatalf("launch catalog = %#v, want %#v", got, want)
	}
	for _, test := range []struct {
		kind   string
		intent projectapp.OpenIntent
	}{
		{kind: "files", intent: projectapp.OpenAny},
		{kind: "photo", intent: projectapp.OpenPhoto},
		{kind: "media", intent: projectapp.OpenVideo},
		{kind: "model", intent: projectapp.OpenModel},
		{kind: "research", intent: projectapp.OpenDataset},
	} {
		if intent, ok := filesLaunchIntent(test.kind); !ok || intent != test.intent {
			t.Fatalf("launch kind %q mapped to %v, %v", test.kind, intent, ok)
		}
	}
	if _, ok := filesLaunchIntent("unknown"); ok {
		t.Fatal("Files claimed an unknown launcher kind")
	}
}

func TestFilesLauncherReopensWithFreshIdentityAndRetiresClosedTexture(t *testing.T) {
	provider, err := projectapp.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &filesProvider{current: provider, root: t.TempDir(), next: 1}
	defer f.Close()
	surface := f.Surfaces()[0]
	old := surface.Texture.ID()
	f.Focus(1)
	f.Seat(experience.Event{Kind: experience.KeyboardCancel})
	f.CloseApplication(1)
	if len(f.Surfaces()) != 0 {
		t.Fatal("closed browser remained registered")
	}
	var opened *projectapp.Provider
	f.onOpen = func(p *projectapp.Provider) { opened = p }
	key, err := f.LaunchApplication("files")
	if err != nil {
		t.Fatal(err)
	}
	next := f.Surfaces()[0]
	if key != "native:project-browser" || next.ID == surface.ID || opened != f.current {
		t.Fatal("reopen did not update provider routing")
	}
	retired := f.RetiredTextures()
	if len(retired) != 1 || retired[0] != old || len(f.RetiredTextures()) != 0 {
		t.Fatal("retirement was lost or repeated", retired)
	}
	f.CloseApplication(surface.ID)
	if len(f.Surfaces()) != 1 {
		t.Fatal("stale close destroyed reopened Files")
	}
	if _, err = f.LaunchApplication("files"); err != nil || f.Surfaces()[0].ID != next.ID {
		t.Fatal("launching live Files duplicated window", err)
	}
}

func TestFilesPickerAliasReusesLiveSurfaceAtWorkspaceCapacity(t *testing.T) {
	provider, err := projectapp.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &filesProvider{current: provider, root: t.TempDir(), next: 1}
	filler := &hubProvider{surfaces: make([]experience.ApplicationSurface, 31)}
	for index := range filler.surfaces {
		filler.surfaces[index].ID = uint64(index + 1)
	}
	hub := newApplicationHub(f, filler)
	defer hub.Close()
	if surfaces := hub.Surfaces(); len(surfaces) != 32 {
		t.Fatalf("test workspace has %d surfaces, want 32", len(surfaces))
	}
	if f.ApplicationLaunchAddsSurface("photo") {
		t.Fatal("photo chooser claimed it would duplicate the live Files surface")
	}
	key, err := hub.LaunchApplication("photo")
	if err != nil || key != "native:project-browser" || len(hub.Surfaces()) != 32 {
		t.Fatal("photo chooser could not reuse Files at workspace capacity", key, err)
	}
	if !f.ApplicationLaunchAddsSurface("unknown") {
		t.Fatal("an unsupported launch was incorrectly treated as reusable")
	}
	f.CloseApplication(1)
	if !f.ApplicationLaunchAddsSurface("photo") {
		t.Fatal("a closed Files surface was incorrectly treated as reusable")
	}
}
