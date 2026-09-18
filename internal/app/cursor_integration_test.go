//go:build linux && cgo

package app

import (
	"bytes"
	"io"
	"math"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/workspace"
)

func TestFootCursorWorkspaceGPU(t *testing.T) {
	if os.Getenv("WORLDR_TEST_APPS") != "1" || os.Getenv("WORLDR_TEST_GPU") != "1" {
		t.Skip("set WORLDR_TEST_APPS=1 and WORLDR_TEST_GPU=1 for real foot cursor integration")
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
	if err := work.Dispatch(workspace.Action{Kind: workspace.SetPlayback, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	controller, err := launchApplication(Options{Application: "foot", ApplicationArgs: []string{"--config=/dev/null", bounded, "15s", "/bin/sh", "-c", "printf 'Cursor integration\\n'; IFS= read -r line"}}, &output)
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
	hub := newApplicationHub(controller)
	work.SetApplications(hub)
	deadline := time.Now().Add(8 * time.Second)
	pollUntil := func(reason string, predicate func() bool) {
		t.Helper()
		for time.Now().Before(deadline) {
			if err := controller.poll(); err != nil {
				t.Fatal(err)
			}
			work.Update(0)
			if predicate() {
				return
			}
			if controller.exited {
				t.Fatalf("foot exited before %s: %s", reason, output.String())
			}
			time.Sleep(3 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s: %s", reason, output.String())
	}
	pollUntil("mapped terminal", func() bool { return len(controller.Surfaces()) == 1 })
	if err := work.Dispatch(workspace.Action{Kind: workspace.ToggleApplicationReading}); err != nil {
		t.Fatal(err)
	}
	const width, height = 960, 600
	frame := work.Draw(width, height)
	var x, y float32
	for _, command := range frame.Commands {
		if command.Kind == render.SceneCommand {
			v := command.View.Viewport
			x, y = v[0]+v[2]/2, v[1]+v[3]/2
			break
		}
	}
	if x == 0 || y == 0 {
		t.Fatal("no application camera viewport")
	}
	dispatch := func(event experience.Event) {
		t.Helper()
		hub.seat(event)
		if quit, err := dispatchEvent(work, event, "", io.Discard); err != nil || quit {
			t.Fatalf("cursor input failed: quit=%v err=%v", quit, err)
		}
	}
	dispatch(experience.Event{Kind: experience.PointerMove, X: x, Y: y, Time: 1})
	var requested experience.ApplicationCursor
	pollUntil("client cursor request", func() bool {
		cursor, ok := work.Cursor()
		if ok && !cursor.Hidden && cursor.Texture != nil {
			requested = cursor
			return true
		}
		return false
	})
	if work.OwnsKeyboard() {
		t.Fatal("hovering for a cursor granted keyboard focus")
	}
	cursorRevision := requested.Texture.Revision()
	vk, err := native.OpenVK(false, width, height)
	if err != nil {
		t.Fatal(err)
	}
	defer vk.Close()
	if err := vk.SetSceneAtlas(work.Atlas()); err != nil {
		t.Fatal(err)
	}
	frame = work.Draw(width, height)
	baseline := make([]byte, width*height*4)
	withCursor := make([]byte, len(baseline))
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, baseline); err != nil {
		t.Fatal(err)
	}
	var overlay cursorOverlay
	composed := overlay.appendFor(frame, x, y, work.Atlas(), work)
	last := composed.Commands[len(composed.Commands)-1]
	if last.Kind != render.ImageCommand || last.Image.Texture != requested.Texture {
		t.Fatal("real client cursor did not become a retained GPU overlay")
	}
	if err := vk.RenderFrame(composed, [4]float32{0, 0, 0, 1}, withCursor); err != nil {
		t.Fatal(err)
	}
	r := last.Image.Bounds
	minX, minY := int(math.Floor(float64(r[0])))-1, int(math.Floor(float64(r[1])))-1
	maxX, maxY := int(math.Ceil(float64(r[0]+r[2])))+1, int(math.Ceil(float64(r[1]+r[3])))+1
	changed := 0
	for py := 0; py < height; py++ {
		for px := 0; px < width; px++ {
			at := (py*width + px) * 4
			if !bytes.Equal(baseline[at:at+4], withCursor[at:at+4]) {
				changed++
				if px < minX || px >= maxX || py < minY || py >= maxY {
					t.Fatalf("cursor changed unrelated frame pixel %d,%d outside %v", px, py, r)
				}
			}
		}
	}
	if changed == 0 {
		t.Fatal("real cursor image changed no GPU pixels")
	}
	if after, ok := work.Cursor(); !ok || after.Texture != requested.Texture || after.Texture.Revision() != cursorRevision {
		t.Fatal("cursor lookup replaced unchanged retained pixels")
	}
	// Capture preserves the client choice outside the app, but final release
	// must restore the workspace arrow without waiting for another motion.
	dispatch(experience.Event{Kind: experience.PointerDown, X: x, Y: y, ButtonCode: 272, Button: experience.ButtonPrimary, Time: 2})
	dispatch(experience.Event{Kind: experience.PointerMove, X: 10, Y: 10, Time: 3})
	if _, ok := work.Cursor(); !ok {
		t.Fatal("captured real cursor was lost outside app bounds")
	}
	dispatch(experience.Event{Kind: experience.PointerUp, X: 10, Y: 10, ButtonCode: 272, Button: experience.ButtonPrimary, Time: 4})
	if _, ok := work.Cursor(); ok {
		t.Fatal("real client cursor survived release over workspace chrome")
	}
	fallback := overlay.appendFor(frame, 10, 10, work.Atlas(), work)
	if fallback.Commands[len(fallback.Commands)-1].Kind != render.OverlayCommand {
		t.Fatal("workspace arrow was not restored")
	}
}
