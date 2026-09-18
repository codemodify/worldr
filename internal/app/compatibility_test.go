//go:build linux && cgo

package app

import (
	"bytes"
	"io"
	"math"
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

// This workflow crosses the real client, protocol, workspace input policy and
// GPU renderer. Ordinary tests do not require an installed terminal or GPU.
func TestFootWorkspaceCompatibilityGPU(t *testing.T) {
	if os.Getenv("WORLDR_TEST_APPS") != "1" || os.Getenv("WORLDR_TEST_GPU") != "1" {
		t.Skip("set WORLDR_TEST_APPS=1 and WORLDR_TEST_GPU=1 for the real foot workspace workflow")
	}
	if _, err := exec.LookPath("foot"); err != nil {
		t.Fatal(err)
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
	const width, height = 1440, 900
	vk, err := native.OpenVK(false, width, height)
	if err != nil {
		t.Fatal(err)
	}
	defer vk.Close()
	if err := vk.SetSceneAtlas(work.Atlas()); err != nil {
		t.Fatal(err)
	}
	clear := [4]float32{0, 0, 0, 1}
	baseline := make([]byte, width*height*4)
	if err := vk.RenderFrame(work.Draw(width, height), clear, baseline); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(t.TempDir(), "typed-line")
	var output bytes.Buffer
	controller, err := launchApplication(Options{
		Application: "foot",
		ApplicationArgs: []string{"--config=/dev/null", "--title=worldr integration", bounded, "20s", "/bin/sh", "-c",
			"printf 'WORLD RENDERER INTEGRATION\\n'; IFS= read -r line; printf '%s' \"$line\" > \"$1\"; IFS= read -r finish", "worldr-test", canary},
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
	deadline := time.Now().Add(15 * time.Second)
	expectingExit := false
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
			if controller.exited && !expectingExit {
				t.Fatalf("foot exited while waiting for %s: %s", reason, output.String())
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s: %s", reason, output.String())
	}
	pollUntil("mapped compact application", func() bool {
		if len(controller.Surfaces()) != 1 {
			return false
		}
		w, h := controller.Surfaces()[0].Texture.Size()
		return w >= 920 && w <= 960 && h >= 560 && h <= 600
	})
	surface := controller.Surfaces()[0]
	if surface.Title != "worldr integration" {
		t.Fatalf("unexpected application title %q", surface.Title)
	}
	if err := work.Dispatch(workspace.Action{Kind: workspace.ToggleApplicationReading}); err != nil {
		t.Fatal(err)
	}
	frame := work.Draw(width, height)
	var clickX, clickY float32
	found := false
	for _, command := range frame.Commands {
		if command.Kind != render.SceneCommand {
			continue
		}
		for _, draw := range command.Draws {
			if draw.Texture != surface.Texture {
				continue
			}
			// Project the actual scene instance's local origin, rather than
			// assuming where the workspace happened to place its app panel.
			p, m := command.View.Projection, draw.Model
			cx := p[0]*m[12] + p[4]*m[13] + p[8]*m[14] + p[12]*m[15]
			cy := p[1]*m[12] + p[5]*m[13] + p[9]*m[14] + p[13]*m[15]
			cw := p[3]*m[12] + p[7]*m[13] + p[11]*m[14] + p[15]*m[15]
			if cw <= 0 {
				t.Fatal("application surface lies behind the camera")
			}
			vp := command.View.Viewport
			clickX, clickY = vp[0]+(cx/cw+1)*vp[2]/2, vp[1]+(cy/cw+1)*vp[3]/2
			found = true
		}
	}
	if !found {
		t.Fatal("workspace frame omitted the real application texture")
	}
	pixels := make([]byte, len(baseline))
	if err := vk.RenderFrame(frame, clear, pixels); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(pixels, baseline) {
		t.Fatal("application content did not change the GPU frame")
	}
	image, _ := surface.Texture.Snapshot(0)
	center := (image.Height/2*image.Width + image.Width/2) * 4
	device := (int(math.Floor(float64(clickY)))*width + int(math.Floor(float64(clickX)))) * 4
	// Foot's center is blank terminal background. Verify it reached the shared
	// color target, including the RGBA client to BGRA readback conversion.
	for channel, source := range []int{2, 1, 0} {
		if delta := int(pixels[device+channel]) - int(image.Pixels[center+source]); delta < -3 || delta > 3 {
			t.Fatalf("GPU surface center %v differs from real client pixel %v", pixels[device:device+4], image.Pixels[center:center+4])
		}
	}
	dispatch := func(event experience.Event) {
		t.Helper()
		controller.seat(event)
		quit, err := dispatchEvent(work, event, "", io.Discard)
		if err != nil || quit {
			t.Fatalf("application input caused host exit/error: quit=%t err=%v", quit, err)
		}
	}
	for _, kind := range []experience.EventKind{experience.PointerDown, experience.PointerUp} {
		dispatch(experience.Event{Kind: kind, X: clickX, Y: clickY, Button: experience.ButtonPrimary, ButtonCode: 272, Time: 1})
	}
	if !work.OwnsKeyboard() {
		t.Fatal("clicking visible application did not transfer workspace keyboard ownership")
	}
	beforeTyping := work.Document()
	var eventTime uint32 = 2
	key := func(code uint32, name experience.Key) {
		t.Helper()
		for _, pressed := range []bool{true, false} {
			dispatch(experience.Event{Kind: experience.KeyInput, Key: name, Keycode: code, Pressed: pressed, Time: eventTime})
			eventTime++
		}
	}
	for _, input := range []struct {
		code uint32
		name experience.Key
	}{{35, ""}, {18, experience.KeyE}, {38, ""}, {38, ""}, {24, ""}, {18, experience.KeyE}, {48, experience.KeyB}, {28, ""}} {
		key(input.code, input.name)
	}
	pollUntil("typed terminal canary", func() bool {
		data, err := os.ReadFile(canary)
		return err == nil && string(data) == "helloeb"
	})
	if work.Document() != beforeTyping {
		t.Fatal("typing E/B into the application changed native workspace state")
	}
	oldWidth, oldHeight := surface.Texture.Size()
	oldRevision := surface.Texture.Revision()
	if err := work.Dispatch(workspace.Action{Kind: workspace.ToggleApplicationSize}); err != nil {
		t.Fatal(err)
	}
	pollUntil("wide application resize", func() bool {
		w, h := surface.Texture.Size()
		return w >= 1400 && w <= 1440 && h >= 860 && h <= 900 && w > oldWidth && h > oldHeight && surface.Texture.Revision() > oldRevision
	})
	if err := vk.RenderFrame(work.Draw(width, height), clear, pixels); err != nil {
		t.Fatalf("resized application failed to render: %v", err)
	}
	key(28, "") // Complete the shell's second read and exit normally.
	expectingExit = true
	pollUntil("application exit and unmap", func() bool { return controller.exited && len(controller.Surfaces()) == 0 })
	if work.OwnsKeyboard() {
		t.Fatal("exited application retained keyboard ownership")
	}
	afterExit := work.Draw(width, height)
	for _, command := range afterExit.Commands {
		for _, draw := range command.Draws {
			if draw.Texture == surface.Texture {
				t.Fatal("exited application's texture remains in the scene")
			}
		}
	}
	remembered := false
	for _, placement := range work.Document().View.Application.Layouts {
		remembered = remembered || placement.Key == surface.Key
	}
	if !remembered {
		t.Fatal("application exit discarded its saved placement")
	}
	if err := vk.RenderFrame(afterExit, clear, pixels); err != nil {
		t.Fatalf("workspace failed after application exit: %v", err)
	}
	// A closed app now exposes the deliberate Forget Closed Placements
	// control. Complete that action before requiring the entire rendered image
	// to match the original empty workspace, without masking any pixels.
	if err := work.Dispatch(workspace.Action{Kind: workspace.ForgetClosedPlacements}); err != nil {
		t.Fatal(err)
	}
	if err := vk.RenderFrame(work.Draw(width, height), clear, pixels); err != nil {
		t.Fatalf("workspace failed after forgetting the closed placement: %v", err)
	}
	if !bytes.Equal(pixels, baseline) {
		changed, largest := 0, 0
		for i, value := range pixels {
			if value != baseline[i] {
				changed++
				delta := int(value) - int(baseline[i])
				if delta < 0 {
					delta = -delta
				}
				if delta > largest {
					largest = delta
				}
			}
		}
		if directory := os.Getenv("WORLDR_CAPTURE_DIR"); directory != "" {
			_ = savePNG(filepath.Join(directory, "foot-baseline.png"), baseline, width, height)
			_ = savePNG(filepath.Join(directory, "foot-after-exit.png"), pixels, width, height)
		}
		t.Fatalf("application exit and placement cleanup did not restore the unchanged native workspace: %d changed channels, largest difference %d", changed, largest)
	}
}
