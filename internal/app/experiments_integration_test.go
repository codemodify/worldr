//go:build linux && cgo

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/plasma"
	"github.com/codemodify/worldr/internal/platform/linux/apps"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/workspace"
)

// Real input and presenter replacement run only inside a private compositor;
// no scripted pointer or keyboard events can reach the user's desktop.
func TestIsolatedNestedExperimentRoundTripGPU(t *testing.T) {
	if os.Getenv("WORLDR_TEST_NESTED") != "1" {
		t.Skip("set WORLDR_TEST_NESTED=1 for private nested experiment switching")
	}
	compositor := newIsolatedNestedHost(t, 960, 600)
	t.Setenv("WAYLAND_DISPLAY", compositor.socket)
	t.Setenv("WAYLAND_SOCKET", "")
	if err := os.Unsetenv("WAYLAND_SOCKET"); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	navigatorPath, plasmaPath := filepath.Join(directory, "navigator.json"), filepath.Join(directory, "plasma.json")
	options := Options{Backend: "nested", Experience: "navigator", Width: 960, Height: 600, FPS: 30, GPUMemoryMiB: 512, State: navigatorPath}
	plasmaOptions := options
	plasmaOptions.Experience, plasmaOptions.State = "plasma", plasmaPath
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	done := make(chan error, 1)
	finished := false
	t.Cleanup(func() {
		cancel()
		if !finished {
			select {
			case <-done:
			case <-time.After(8 * time.Second):
				t.Error("experiment host did not stop")
			}
		}
	})
	go func() {
		first := true
		done <- RunExperiments(ctx, io.Discard, options, func(o Options) (experience.Experience, error) {
			if o.Experience == "plasma" {
				return plasma.New()
			}
			w, err := workspace.NewNavigator()
			if err == nil && first {
				first = false
				// Bypass catalog seeding so verification never touches dist or
				// launches project/model clients from the developer's session.
				o.experiments.visited["plasma"] = plasmaOptions
				err = w.Dispatch(workspace.Action{Kind: workspace.RenameSpace, Space: 0, SpaceName: "Switcher persistence canary"})
			}
			return w, err
		})
	}()
	waitSurface := func(title string, previous uint64) apps.Surface {
		t.Helper()
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case err := <-done:
				finished = true
				t.Fatalf("experiment host stopped before %s: %v", title, err)
			default:
			}
			var found apps.Surface
			compositor.call(t, func(server *apps.Server, surfaces []apps.Surface, _ *native.VK) error {
				if len(surfaces) > 1 {
					return fmt.Errorf("switch left %d windows mapped", len(surfaces))
				}
				if len(surfaces) == 1 && surfaces[0].ID != previous && strings.Contains(surfaces[0].Title, title) {
					found = surfaces[0]
					return server.Focus(found.ID)
				}
				return nil
			})
			if found.ID != 0 {
				return found
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", title)
		return apps.Surface{}
	}
	click := func(surface apps.Surface, rect image.Rectangle) {
		t.Helper()
		compositor.call(t, func(server *apps.Server, _ []apps.Surface, _ *native.VK) error {
			if err := server.Pointer(surface.ID, float32(rect.Min.X+rect.Dx()/2), float32(rect.Min.Y+rect.Dy()/2)); err != nil {
				return err
			}
			if err := server.Button(272, true, 0); err != nil {
				return err
			}
			return server.Button(272, false, 1)
		})
	}
	stroke := func(code, mods uint32) {
		t.Helper()
		compositor.call(t, func(server *apps.Server, _ []apps.Surface, _ *native.VK) error {
			if err := server.Key(code, true, 0, mods, 0, 0, 0); err != nil {
				return err
			}
			if err := server.Key(code, false, 1, mods, 0, 0, 0); err != nil {
				return err
			}
			return server.Modifiers(0, 0, 0, 0)
		})
	}
	navigator := waitSurface("Navigator", 0)
	geometry := menuForTest(t)
	geometry.layout(navigator.LogicalWidth, navigator.LogicalHeight, 1)
	click(navigator, geometry.button)
	if path := os.Getenv("WORLDR_EXPERIMENT_CAPTURE"); path != "" {
		captureExperimentMenu(t, compositor, navigator, path)
	}
	// At this viewport the entire catalog fits; Navigator remains last.
	if geometry.visible != len(geometry.items) {
		t.Fatal("test viewport no longer fits the catalog")
	}
	plasmaIndex := -1
	for i, item := range geometry.items {
		if item.ID == "plasma" {
			plasmaIndex = i
		}
	}
	if plasmaIndex < 0 {
		t.Fatal("Plasma experiment is missing")
	}
	row := geometry.panel.Min.Y + 46 + (plasmaIndex-geometry.scroll)*experimentRowHeight
	click(navigator, image.Rect(geometry.panel.Min.X, row, geometry.panel.Max.X, row+experimentRowHeight))
	fluid := waitSurface("Fluid surfaces", navigator.ID)
	before, err := os.ReadFile(navigatorPath)
	if err != nil || !bytes.Contains(before, []byte("Switcher persistence canary")) {
		t.Fatalf("switch did not save Navigator: %v", err)
	}
	stroke(50, 0)  // M toggles Plasma's motion preference through actual input.
	stroke(18, 12) // Ctrl+Alt+E opens the shared menu in another experience.
	for i := plasmaIndex + 1; i < len(geometry.items); i++ {
		stroke(108, 0) // Move through later experiments to the final Navigator.
	}
	stroke(28, 0)
	waitSurface("Navigator", fluid.ID)
	plasmaData, err := os.ReadFile(plasmaPath)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := decodeStateEnvelope(plasmaData)
	if err != nil || envelope.ExperienceID != "worldr.plasma" {
		t.Fatalf("Plasma did not retain its own state: %v", err)
	}
	var saved struct {
		Layout struct {
			Motion *bool `json:"motion"`
		} `json:"layout"`
	}
	if err = json.Unmarshal(envelope.State, &saved); err != nil || saved.Layout.Motion == nil || *saved.Layout.Motion {
		t.Fatalf("Plasma keyboard change was not saved: %v", err)
	}
	stroke(16, 12) // Ctrl+Alt+Q saves and quits the restored Navigator.
	select {
	case err := <-done:
		finished = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("global quit did not finish restored experiment")
	}
	after, err := os.ReadFile(navigatorPath)
	if err != nil || !bytes.Contains(after, []byte("Switcher persistence canary")) {
		t.Fatalf("return discarded saved Navigator state: %v", err)
	}
	compositor.call(t, func(_ *apps.Server, surfaces []apps.Surface, _ *native.VK) error {
		if len(surfaces) > 1 {
			return fmt.Errorf("host left %d windows after quit", len(surfaces))
		}
		return nil
	})
}

func captureExperimentMenu(t *testing.T, compositor *isolatedNestedHost, initial apps.Surface, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var captured *image.RGBA
		compositor.call(t, func(_ *apps.Server, surfaces []apps.Surface, vk *native.VK) error {
			if len(surfaces) != 1 || surfaces[0].ID != initial.ID || surfaces[0].Revision < initial.Revision+3 {
				return nil
			}
			surface := surfaces[0]
			captured = image.NewRGBA(image.Rect(0, 0, surface.Width, surface.Height))
			if surface.Texture == nil {
				if len(surface.Pixels) != len(captured.Pix) {
					return fmt.Errorf("private output has unexpected pixel size")
				}
				copy(captured.Pix, surface.Pixels)
				return nil
			}
			if err := vk.Resize(uint32(surface.Width), uint32(surface.Height)); err != nil {
				return err
			}
			frame := render.Frame{Commands: []render.Command{{Kind: render.ImageCommand, Image: render.Image{Texture: surface.Texture, Bounds: [4]float32{0, 0, float32(surface.Width), float32(surface.Height)}}}}}
			return vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, captured.Pix)
		})
		if captured != nil {
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(file, captured)
			closeErr := file.Close()
			if err != nil {
				t.Fatal(err)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("private menu screenshot timed out")
}
