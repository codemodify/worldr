//go:build linux && cgo

package app

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/workspace"
)

func TestMultipleFootSpatialWorkflowGPU(t *testing.T) {
	if os.Getenv("WORLDR_TEST_APPS") != "1" || os.Getenv("WORLDR_TEST_GPU") != "1" {
		t.Skip("set WORLDR_TEST_APPS=1 and WORLDR_TEST_GPU=1 for the multiple foot workflow")
	}
	bounded, err := exec.LookPath("timeout")
	if err != nil {
		t.Fatal(err)
	}
	work, err := workspace.New()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Close()
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
	canaries := t.TempDir()
	var output bytes.Buffer
	controller, err := launchApplication(Options{
		Applications: []string{"foot", "foot"},
		ApplicationArgs: []string{"--config=/dev/null", "--title=worldr spatial terminal", bounded, "30s", "/bin/sh", "-c",
			"printf 'WORLDR / LIVE SPATIAL TERMINAL\\n\\nIndependent shell. Shared workspace.\\n'; IFS= read -r line; printf '%s' \"$line\" > \"$1/$line\"; IFS= read -r finish", "worldr-test", canaries},
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		work.SetApplications(nil)
		controller.close()
		if t.Failed() {
			t.Log(output.String())
		}
	}()
	work.SetApplications(controller)
	pollUntil := func(reason string, predicate func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if err := controller.poll(); err != nil {
				t.Fatal(err)
			}
			work.Update(0)
			if predicate() {
				return
			}
			if controller.exited {
				t.Fatalf("clients exited while waiting for %s: %s", reason, output.String())
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s: %s", reason, output.String())
	}
	pollUntil("two mapped applications", func() bool { return len(controller.Surfaces()) == 2 })
	surfaces := append([]experience.ApplicationSurface(nil), controller.Surfaces()...)
	if surfaces[0].Key == surfaces[1].Key || surfaces[0].Key == "" || surfaces[1].Key == "" {
		t.Fatal("launch slots lost independent layout keys")
	}
	pixels := make([]byte, width*height*4)
	renderScene := func(name string) render.Frame {
		t.Helper()
		frame := work.Draw(width, height)
		if err := vk.RenderFrame(frame, [4]float32{.025, .034, .043, 1}, pixels); err != nil {
			t.Fatal(err)
		}
		if directory := os.Getenv("WORLDR_CAPTURE_DIR"); directory != "" {
			if err := savePNG(filepath.Join(directory, name+".png"), pixels, width, height); err != nil {
				t.Fatal(err)
			}
		}
		return frame
	}
	dispatch := func(event experience.Event) {
		t.Helper()
		controller.seat(event)
		if quit, err := dispatchEvent(work, event, "", io.Discard); err != nil || quit {
			t.Fatalf("unexpected workspace exit: %t %v", quit, err)
		}
	}
	click := func(texture *render.Texture) {
		t.Helper()
		x, y := applicationTextureCenter(t, work.Draw(width, height), texture)
		for _, kind := range []experience.EventKind{experience.PointerDown, experience.PointerUp} {
			dispatch(experience.Event{Kind: kind, X: x, Y: y, Button: experience.ButtonPrimary, ButtonCode: 272, Time: 1})
		}
	}
	renderScene("multi-spatial")
	for i, item := range []struct {
		name string
		keys []uint32
	}{{"one", []uint32{24, 49, 18, 28}}, {"two", []uint32{20, 17, 24, 28}}} {
		act(workspace.Action{Kind: workspace.SelectApplication, ApplicationKey: surfaces[i].Key})
		act(workspace.Action{Kind: workspace.ToggleApplicationReading})
		click(surfaces[i].Texture)
		if !work.OwnsKeyboard() {
			t.Fatal("read view click did not focus the selected application")
		}
		for _, code := range item.keys {
			for _, down := range []bool{true, false} {
				dispatch(experience.Event{Kind: experience.KeyInput, Keycode: code, Pressed: down, Time: 2})
			}
		}
		pollUntil("independent shell input", func() bool {
			data, err := os.ReadFile(filepath.Join(canaries, item.name))
			return err == nil && string(data) == item.name
		})
		act(workspace.Action{Kind: workspace.ToggleApplicationReading})
	}
	act(workspace.Action{Kind: workspace.SelectApplication, ApplicationKey: surfaces[0].Key})
	act(workspace.Action{Kind: workspace.SelectApplication, ApplicationKey: surfaces[1].Key, Additive: true})
	act(workspace.Action{Kind: workspace.GroupApplications})
	before := work.Document().View.Application.Layouts
	act(workspace.Action{Kind: workspace.MoveApplications, DeltaX: 1.25, DeltaY: -.5, DeltaDepth: -2})
	after := work.Document().View.Application.Layouts
	for i, p := range before {
		if p.Key == "" {
			continue
		}
		if after[i].X != p.X+1.25 || after[i].Y != p.Y-.5 || after[i].Depth != p.Depth-2 || after[i].Group == 0 {
			t.Fatal("group movement lost relative placement")
		}
	}
	act(workspace.Action{Kind: workspace.Undo})
	if work.Document().View.Application.Layouts != before {
		t.Fatal("group movement did not undo in one step")
	}
	act(workspace.Action{Kind: workspace.Redo})
	act(workspace.Action{Kind: workspace.ToggleApplicationOverview})
	frame := renderScene("multi-overview")
	for _, surface := range surfaces {
		applicationTextureCenter(t, frame, surface.Texture)
	}
	click(surfaces[0].Texture)
	if work.Document().View.Application.Overview || work.Document().View.Application.Active != surfaces[0].Key || work.OwnsKeyboard() {
		t.Fatal("overview retrieval changed keyboard focus or lost selection")
	}
	state := filepath.Join(t.TempDir(), "workspace.json")
	if err := saveState(state, work); err != nil {
		t.Fatal(err)
	}
	saved := work.Document()
	act(workspace.Action{Kind: workspace.MoveApplications, DeltaX: 2})
	if err := loadState(state, work); err != nil {
		t.Fatal(err)
	}
	if work.Document() != saved || work.OwnsKeyboard() {
		t.Fatal("saved placement/group restore changed layout or acquired keyboard")
	}
	renderScene("multi-restored")
	// Closing one client must preserve the other's real process and GPU surface.
	act(workspace.Action{Kind: workspace.ToggleApplicationReading})
	click(surfaces[0].Texture)
	for _, down := range []bool{true, false} {
		dispatch(experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: down, Time: 3})
	}
	pollUntil("one application exits", func() bool { return len(controller.Surfaces()) == 1 })
	if controller.Surfaces()[0].Key != surfaces[1].Key || work.OwnsKeyboard() {
		t.Fatal("closing one application affected its sibling or retained input focus")
	}
	renderScene("multi-survivor")
}
