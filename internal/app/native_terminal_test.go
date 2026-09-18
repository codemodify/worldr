//go:build linux && cgo

package app

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeapps"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/workspace"
)

func TestNativeShellLaunchesCompatibilityApplication(t *testing.T) {
	if os.Getenv("WORLDR_TEST_APPS") != "1" {
		t.Skip("set WORLDR_TEST_APPS=1 for native shell application launch")
	}
	var output bytes.Buffer
	controller, err := openApplicationController(&output)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.close()
	terminal, err := nativeapps.New(nativeapps.Options{Command: "/bin/sh", Args: []string{"-c", "foot --config=/dev/null --title=from-native-shell /bin/sh -c 'IFS= read -r line' & IFS= read -r done"}, Env: applicationEnvironment(os.Environ(), controller.socket)})
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := terminal.Poll(); err != nil {
			t.Fatal(err)
		}
		if err := controller.poll(); err != nil {
			t.Fatal(err)
		}
		if len(controller.Surfaces()) == 1 && controller.Surfaces()[0].Title == "from-native-shell" {
			hub := newApplicationHub(controller, terminal)
			if surfaces := hub.Surfaces(); len(surfaces) != 2 || surfaces[0].ID == surfaces[1].ID {
				t.Fatal("native and shell-launched application did not coexist")
			}
			return
		}
		time.Sleep(3 * time.Millisecond)
	}
	t.Fatalf("native shell child did not map into worldr: %s", output.String())
}

func TestWorkspaceCreatesAndReopensRealNativeTerminal(t *testing.T) {
	manager := nativeapps.NewManager(nativeapps.Options{Command: "/bin/sh", Args: []string{"-c", "IFS= read -r line"}})
	defer manager.Close()
	work, err := workspace.New()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Close()
	hub := newApplicationHub(manager)
	work.SetApplications(hub)
	defer work.SetApplications(nil)
	click := func(x, y float32) {
		t.Helper()
		work.Draw(1440, 900)
		for _, kind := range []experience.EventKind{experience.PointerDown, experience.PointerUp} {
			if !work.Handle(experience.Event{Kind: kind, X: x, Y: y, Button: experience.ButtonPrimary, ButtonCode: 272}) {
				t.Fatal("workspace control did not handle click")
			}
		}
	}
	click(550, 45) // New Terminal in the persistent header.
	if err := manager.Poll(); err != nil {
		t.Fatal(err)
	}
	first := append([]experience.ApplicationSurface(nil), hub.Surfaces()...)
	if len(first) != 1 || work.Document().View.Application.Active != first[0].Key || work.OwnsKeyboard() {
		t.Fatal("native launch did not select exactly one app with explicit keyboard focus")
	}
	click(550, 45)
	if err := manager.Poll(); err != nil {
		t.Fatal(err)
	}
	second := append([]experience.ApplicationSurface(nil), hub.Surfaces()...)
	if len(second) != 2 || second[0].ID == second[1].ID || second[0].Key == second[1].Key {
		t.Fatal("second native shell lost independent identity")
	}
	key, id := second[1].Key, second[1].ID
	click(100, 760) // Close the active second terminal, leaving the first alive.
	if err := manager.Poll(); err != nil {
		t.Fatal(err)
	}
	if survivors := hub.Surfaces(); len(survivors) != 1 || survivors[0].ID != first[0].ID {
		t.Fatal("selected close affected another native terminal")
	}
	if retired := manager.Retired(); len(retired) != 1 || retired[0] != second[1].Texture.ID() {
		t.Fatal("closed native terminal image was not retired")
	}
	click(550, 45)
	if err := manager.Poll(); err != nil {
		t.Fatal(err)
	}
	reopened := hub.Surfaces()
	if len(reopened) != 2 || reopened[1].ID == id || reopened[1].Key != key || reopened[0].ID != first[0].ID {
		t.Fatal("reopening lost layout key or reused stale runtime input identity")
	}
}

func TestNativeTerminalWorkspaceGPU(t *testing.T) {
	if os.Getenv("WORLDR_TEST_GPU") != "1" {
		t.Skip("set WORLDR_TEST_GPU=1 for native terminal GPU integration")
	}
	canary := filepath.Join(t.TempDir(), "typed")
	p, err := nativeapps.New(nativeapps.Options{Command: "/bin/sh", Args: []string{"-c", "printf '\\033]2;WORLDR / NATIVE SHELL\\007\\033[36mNative terminal / real PTY\\033[0m\\n\\nThis shell runs directly inside worldr.\\n'; IFS= read -r line; printf '%s' \"$line\" > \"$1\"; printf '\\nReceived: %s\\n' \"$line\"; IFS= read -r finish", "worldr-test", canary}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	work, err := workspace.New()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Close()
	hub := newApplicationHub(p)
	work.SetApplications(hub)
	defer work.SetApplications(nil)
	act := func(action workspace.Action) {
		t.Helper()
		if err := work.Dispatch(action); err != nil {
			t.Fatal(err)
		}
	}
	act(workspace.Action{Kind: workspace.SetPlayback, Enabled: false})
	const width, height = 1440, 900
	vk, err := native.OpenVK(false, width, height)
	if err != nil {
		t.Fatal(err)
	}
	defer vk.Close()
	if err := vk.SetSceneAtlas(work.Atlas()); err != nil {
		t.Fatal(err)
	}
	pollUntil := func(reason string, predicate func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if err := p.Poll(); err != nil {
				t.Fatal(err)
			}
			work.Update(0)
			if predicate() {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Fatal("timed out waiting for " + reason)
	}
	pollUntil("native shell output", func() bool {
		return len(p.Surfaces()) == 1 && p.Surfaces()[0].Title == "Native terminal / WORLDR / NATIVE SHELL"
	})
	texture := p.Surfaces()[0].Texture
	act(workspace.Action{Kind: workspace.ToggleApplicationReading})
	frame := work.Draw(width, height)
	x, y := applicationTextureCenter(t, frame, texture)
	dispatch := func(event experience.Event) {
		t.Helper()
		hub.seat(event)
		if quit, err := dispatchEvent(work, event, "", io.Discard); err != nil || quit {
			t.Fatalf("native terminal event caused exit: %t %v", quit, err)
		}
	}
	for _, kind := range []experience.EventKind{experience.PointerDown, experience.PointerUp} {
		dispatch(experience.Event{Kind: kind, X: x, Y: y, Button: experience.ButtonPrimary, ButtonCode: 272})
	}
	if !work.OwnsKeyboard() {
		t.Fatal("native shell did not gain keyboard focus")
	}
	for _, code := range []uint32{17, 24, 19, 38, 32, 19, 28} {
		for _, down := range []bool{true, false} {
			dispatch(experience.Event{Kind: experience.KeyInput, Keycode: code, Pressed: down})
		}
	}
	pollUntil("raw keys reaching native PTY", func() bool { data, err := os.ReadFile(canary); return err == nil && string(data) == "worldr" })
	pixels := make([]byte, width*height*4)
	renderScene := func(name string) {
		t.Helper()
		if err := vk.RenderFrame(work.Draw(width, height), [4]float32{.025, .034, .043, 1}, pixels); err != nil {
			t.Fatal(err)
		}
		if directory := os.Getenv("WORLDR_CAPTURE_DIR"); directory != "" {
			if err := savePNG(filepath.Join(directory, name+".png"), pixels, width, height); err != nil {
				t.Fatal(err)
			}
		}
	}
	renderScene("native-terminal-reading")
	oldWidth, oldHeight := texture.Size()
	act(workspace.Action{Kind: workspace.ToggleApplicationSize})
	pollUntil("native grid resize", func() bool { w, h := texture.Size(); return w > oldWidth && h > oldHeight })
	act(workspace.Action{Kind: workspace.ToggleApplicationReading})
	act(workspace.Action{Kind: workspace.MoveApplications, DeltaX: -2, DeltaDepth: -3})
	renderScene("native-terminal-spatial")
	act(workspace.Action{Kind: workspace.ToggleApplicationOverview})
	renderScene("native-terminal-overview")
	if work.OwnsKeyboard() {
		t.Fatal("overview retained native keyboard focus")
	}
}
