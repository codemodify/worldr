//go:build linux

package app

import (
	"bytes"
	"context"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/projectapp"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/workspace"
)

func TestProjectBrowserDesktopFocusResizeClipboardAndClose(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "canary λ.txt")
	const contents = "Read-only project fixture.\n"
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	project, err := projectapp.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer project.Close()
	work, err := workspace.NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Close()
	texture, err := render.NewTexture(2, 2, bytes.Repeat([]byte{20, 30, 40, 255}, 4))
	if err != nil {
		t.Fatal(err)
	}
	sibling := &hubProvider{surfaces: []experience.ApplicationSurface{{ID: 1, Key: "native:terminal", Texture: texture}}}
	hub := newApplicationHub(sibling, project)
	work.SetApplications(hub)
	defer work.SetApplications(nil)
	if work.Info().ID != "worldr.workspace" {
		t.Fatal("project browser opened in the study experience")
	}
	surfaces := append([]experience.ApplicationSurface(nil), hub.Surfaces()...)
	if len(surfaces) != 2 || surfaces[0].ID == surfaces[1].ID || surfaces[1].Key != "native:project-browser" {
		t.Fatalf("project and terminal identities collided: %+v", surfaces)
	}
	browser := surfaces[1]
	act := func(action workspace.Action) {
		t.Helper()
		if err := work.Dispatch(action); err != nil {
			t.Fatal(err)
		}
	}
	send := func(event experience.Event) {
		t.Helper()
		hub.seat(event)
		if quit, err := dispatchEvent(work, event, "", io.Discard); err != nil || quit {
			t.Fatalf("project input caused host exit: quit=%t err=%v", quit, err)
		}
	}
	stroke := func(code uint32, modifiers experience.Modifiers) {
		t.Helper()
		for _, pressed := range []bool{true, false} {
			send(experience.Event{Kind: experience.KeyInput, Keycode: code, Modifiers: modifiers, Pressed: pressed})
		}
	}
	focus := func(key string) {
		t.Helper()
		act(workspace.Action{Kind: workspace.SelectApplication, ApplicationKey: key})
		work.Draw(1440, 900)
		stroke(28, 0) // Explicit fresh Enter grants typing focus after selection.
		if !work.OwnsKeyboard() {
			t.Fatal("explicit activation did not grant application keyboard ownership")
		}
	}
	focus(browser.Key)
	if hub.focused != browser.ID || sibling.focused != 0 {
		t.Fatal("browser focus left a sibling terminal focused")
	}
	// Readiness is observed through an actual Copy Path request, without
	// inspecting provider internals or depending on rendered text glyphs.
	deadline := time.Now().Add(3 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		if err := project.Poll(); err != nil {
			t.Fatal(err)
		}
		work.Update(0)
		stroke(46, experience.ModControl|experience.ModShift)
		if copied, ok := project.TakeCopy(); ok && copied == path {
			ready = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !ready || sibling.sent != 0 {
		t.Fatalf("focused browser failed to copy its selected file, or sent keys to its sibling: ready=%t sibling=%d", ready, sibling.sent)
	}
	initialWidth, initialHeight := browser.Texture.Size()
	act(workspace.Action{Kind: workspace.ToggleApplicationSize})
	if err := project.Poll(); err != nil {
		t.Fatal(err)
	}
	work.Draw(1440, 900)
	width, height := browser.Texture.Size()
	if width <= initialWidth || height <= initialHeight || project.Surfaces()[0].Texture != browser.Texture {
		t.Fatal("workspace resize failed to update the browser's retained image")
	}
	copyEndpoint := newCopyOnlyClipboard(project)
	defer copyEndpoint.close()
	terminalClient := &clipboardClient{}
	terminalEndpoint := newNativeClipboard(terminalClient)
	defer terminalEndpoint.close()
	broker := newClipboardBroker(terminalEndpoint, copyEndpoint)
	stroke(46, experience.ModControl|experience.ModShift)
	broker.sync()
	if terminalClient.pasted != "" {
		t.Fatal("copying a browser path pasted into the terminal without a request")
	}
	focus(surfaces[0].Key)
	if sibling.focused != 1 || hub.focused != surfaces[0].ID {
		t.Fatal("terminal did not take exclusive focus from the browser")
	}
	stroke(46, experience.ModControl|experience.ModShift)
	if _, copied := project.TakeCopy(); copied || sibling.sent != 1 {
		t.Fatal("terminal key stroke leaked back into the browser")
	}
	terminalClient.requested = true
	deadline = time.Now().Add(time.Second)
	for terminalClient.pasted == "" && time.Now().Before(deadline) {
		broker.sync()
		time.Sleep(time.Millisecond)
	}
	if terminalClient.pasted != path {
		t.Fatalf("cross-provider paste changed selected path: got %q want %q", terminalClient.pasted, path)
	}
	focus(browser.Key)
	hub.CloseApplication(browser.ID)
	if err := project.Poll(); err != nil {
		t.Fatal(err)
	}
	work.Update(0)
	frame := work.Draw(1440, 900)
	if work.OwnsKeyboard() || len(hub.Surfaces()) != 1 || hub.Surfaces()[0].ID != surfaces[0].ID || sibling.closed != 0 {
		t.Fatal("closing browser retained focus or closed its sibling")
	}
	for _, command := range frame.Commands {
		for _, draw := range command.Draws {
			if draw.Texture == browser.Texture {
				t.Fatal("closed browser remained in the submitted scene")
			}
		}
	}
	if retired := project.RetiredTextures(); len(retired) != 1 || retired[0] != browser.Texture.ID() {
		t.Fatalf("closed browser image did not retire exactly once: %v", retired)
	}
	hub.CloseApplication(browser.ID) // A removed hub route must remain harmless.
	project.Close()
	if retired := project.RetiredTextures(); len(retired) != 0 {
		t.Fatal("repeated browser close retired its image twice")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != contents {
		t.Fatal("browser focus/copy/resize/close changed its project file", err)
	}
}

func TestInvalidProjectFailsBeforePresenterOrProcessLaunch(t *testing.T) {
	file := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(file, []byte("canary"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{file, filepath.Join(t.TempDir(), "missing-root")} {
		o, err := Parse([]string{"--project=" + path, "--backend=nested", "--app=worldr-must-not-launch-this"}, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		err = Run(context.Background(), io.Discard, o, func() (experience.Experience, error) { return workspace.NewDesktop() })
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "project") {
			t.Fatalf("invalid project reached display/process startup: %v", err)
		}
	}
}

func TestProjectBrowserHeadlessStartupSnapshotGPU(t *testing.T) {
	if os.Getenv("WORLDR_TEST_GPU") != "1" {
		t.Skip("set WORLDR_TEST_GPU=1 for project browser host integration")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.txt"), []byte("Native project browser integration.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	exports := t.TempDir()
	snapshot, state := filepath.Join(exports, "desktop.png"), filepath.Join(exports, "desktop.json")
	o, err := Parse([]string{"--project=" + root, "--backend=headless", "--width=960", "--height=600", "--frames=3", "--snapshot=" + snapshot, "--state=" + state}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), io.Discard, o, func() (experience.Experience, error) { return workspace.NewDesktop() }); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"worldr.workspace"`)) || !bytes.Contains(data, []byte(`"native:project-browser"`)) {
		t.Fatalf("startup browser placement was not saved in a desktop document: %s", data)
	}
	f, err := os.Open(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	im, err := png.Decode(f)
	if err != nil || im.Bounds().Dx() != 960 || im.Bounds().Dy() != 600 {
		t.Fatal("project desktop snapshot did not preserve output dimensions", err)
	}
	colors := make(map[[3]uint32]bool)
	for y := 0; y < 600; y += 7 {
		for x := 0; x < 960; x += 7 {
			r, g, b, _ := im.At(x, y).RGBA()
			colors[[3]uint32{r, g, b}] = true
		}
	}
	if len(colors) < 30 {
		t.Fatalf("project desktop snapshot lacks rendered content: %d colors", len(colors))
	}
}
