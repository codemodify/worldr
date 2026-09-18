package app

import (
	"context"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/workspace"
)

func TestDisplayChoiceProtectsExistingSession(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv("DISPLAY", ":0")
	t.Setenv("XDG_SESSION_TYPE", "wayland")
	if b := chooseBackend(Options{Backend: "auto"}); b != "nested" {
		t.Fatalf("got %s", b)
	}
	for _, backend := range []string{"vk-display", "drm"} {
		if err := validateDirect(Options{}, backend); err == nil {
			t.Fatalf("%s must refuse takeover", backend)
		}
	}
	t.Setenv("WAYLAND_DISPLAY", "")
	if b := chooseBackend(Options{Backend: "auto"}); b != "headless" {
		t.Fatalf("X11 host: %s", b)
	}
	t.Setenv("DISPLAY", "")
	if b := chooseBackend(Options{Backend: "auto"}); b != "headless" {
		t.Fatalf("graphical session without a socket: %s", b)
	}
}

func TestInvalidOptionsFailBeforeDeviceCreation(t *testing.T) {
	for _, args := range [][]string{{"--fps=0"}, {"--frames=-1"}, {"--duration=-1s"}, {"--autosave=-1s"}, {"--width=100000"}, {"--backend=unknown"}, {"--output=0"}, {"--output=abc"}, {"--backend=nested", "--output=9"}, {"--backend=drm", "--output=9", "--output=10"}, {"unexpected"}} {
		if _, err := Parse(args, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	o, err := Parse([]string{"--backend=wayland-client", "--frames=4"}, io.Discard)
	if err != nil || o.Backend != "nested" || o.Frames != 4 {
		t.Fatalf("alias: %+v, %v", o, err)
	}
	o, err = Parse([]string{"--backend=vk-display", "--output=508", "--output=517", "--list-outputs"}, io.Discard)
	if err != nil || !o.ListOutputs || len(o.Outputs) != 2 || o.Outputs[0] != 508 || o.Outputs[1] != 517 {
		t.Fatalf("direct output selection: %+v, %v", o, err)
	}
}

func TestSnapshotPreservesChannelsAndOrientation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frame.png")
	// BGRA red at the top and blue at the bottom.
	if err := savePNG(path, []byte{0, 0, 255, 255, 255, 0, 0, 255}, 1, 2); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	im, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	r, _, b, _ := im.At(0, 0).RGBA()
	if r != 65535 || b != 0 {
		t.Fatal("top row/channels changed")
	}
	r, _, b, _ = im.At(0, 1).RGBA()
	if r != 0 || b != 65535 {
		t.Fatal("bottom row/channels changed")
	}
}

// CI enables this with a software Vulkan driver; development can use a real GPU.
// Failure is never converted to a skip when the integration check is requested.
func TestHeadlessWorkspaceSnapshot(t *testing.T) {
	if os.Getenv("WORLDR_TEST_GPU") != "1" {
		t.Skip("set WORLDR_TEST_GPU=1 with a Vulkan device to exercise the full renderer")
	}
	path := filepath.Join(t.TempDir(), "workspace.png")
	statePath := filepath.Join(t.TempDir(), "study.json")
	seed, err := workspace.New()
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []workspace.Action{{Kind: workspace.SetPlayback, Enabled: false}, {Kind: workspace.SelectComponent, Component: workspace.Shaft}, {Kind: workspace.SetExploded, Enabled: true}, {Kind: workspace.SeekTime, Seconds: 12.5}} {
		if err := seed.Dispatch(action); err != nil {
			seed.Close()
			t.Fatal(err)
		}
	}
	wantDocument := seed.Document()
	if err := saveState(statePath, seed); err != nil {
		seed.Close()
		t.Fatal(err)
	}
	seed.Close()
	o, err := Parse([]string{"--backend=headless", "--width=960", "--height=600", "--frames=2", "--snapshot=" + path, "--state=" + statePath, "--demo"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), io.Discard, o, func() (experience.Experience, error) { return workspace.New() }); err != nil {
		t.Fatal(err)
	}
	reloaded, err := workspace.New()
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	if err := loadState(statePath, reloaded); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Document(); got != wantDocument {
		t.Fatalf("real application save/load changed paused document: got %+v, want %+v", got, wantDocument)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	im, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if im.Bounds().Dx() != 960 || im.Bounds().Dy() != 600 {
		t.Fatal(im.Bounds())
	}
	colors := map[uint32]bool{}
	for y := 0; y < 600; y += 7 {
		for x := 0; x < 960; x += 7 {
			r, g, b, _ := im.At(x, y).RGBA()
			colors[(r>>8)<<16|(g>>8)<<8|(b>>8)] = true
		}
	}
	if len(colors) < 30 {
		t.Fatalf("rendered image lacks scene content: %d colors", len(colors))
	}
}

func TestCursorDoesNotMutateBorrowedFrameCapacity(t *testing.T) {
	vertices := make([]render.Vertex, 12)
	for i := range vertices {
		vertices[i].X = 99
	}
	commands := make([]render.Command, 4)
	for i := range commands {
		commands[i].First = 777
	}
	commands[0] = render.Command{Kind: render.OverlayCommand, First: 0, Count: 3}
	frame := render.Frame{Vertices: vertices[:3], Commands: commands[:1]}
	var cursor cursorOverlay
	withCursor := cursor.append(frame, 20, 30, render.Atlas{Width: 16, Height: 16})
	for _, vertex := range vertices {
		if vertex.X != 99 {
			t.Fatal("cursor overwrote experience-owned vertex capacity")
		}
	}
	for _, command := range commands[1:] {
		if command.First != 777 {
			t.Fatal("cursor overwrote experience-owned command capacity")
		}
	}
	if len(withCursor.Vertices) != 9 || len(withCursor.Commands) != 2 || withCursor.Commands[1].First != 3 || withCursor.Commands[1].Count != 6 {
		t.Fatal("cursor overlay range is incorrect")
	}
}
