//go:build linux && cgo

package app

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/modelapp"
	"github.com/codemodify/worldr/internal/nativeapps"
	"github.com/codemodify/worldr/internal/photoapp"
	"github.com/codemodify/worldr/internal/platform/linux/apps"
	"github.com/codemodify/worldr/internal/platform/linux/host"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/workspace"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The nested renderer connects only to this private compositor. Scripted input
// cannot reach the user's host desktop. All server/Vulkan importer calls belong
// to one goroutine, including texture retirement and shutdown.
type isolatedNestedRequest struct {
	call func(*apps.Server, []apps.Surface, *native.VK) error
	done chan error
}

type isolatedNestedHost struct {
	socket string
	calls  chan isolatedNestedRequest
	stop   chan struct{}
	done   chan error
}

func newIsolatedNestedHost(t *testing.T, width, height int) *isolatedNestedHost {
	t.Helper()
	h := &isolatedNestedHost{calls: make(chan isolatedNestedRequest), stop: make(chan struct{}), done: make(chan error, 1)}
	ready := make(chan error, 1)
	go func() {
		vk, err := native.OpenVK(false, 64, 64)
		if err != nil {
			ready <- err
			h.done <- err
			return
		}
		defer vk.Close()
		server, err := apps.Open(width, height)
		if err != nil {
			ready <- err
			h.done <- err
			return
		}
		retire := func() {
			for _, texture := range server.RetiredTextures() {
				vk.ReleaseTexture(texture.ID())
				texture.Close()
			}
		}
		defer func() { server.Close(); retire() }()
		if len(vk.DMABufFormats()) == 0 {
			err = fmt.Errorf("private nested compositor needs Vulkan LINEAR DMA-BUF import")
		} else {
			err = server.SetDMABufImporter(vk.DMABufFormats(), vk.ImportDMABuf)
		}
		if err != nil {
			ready <- err
			h.done <- err
			return
		}
		h.socket = server.Socket()
		ready <- nil
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		var surfaces []apps.Surface
		for {
			select {
			case <-h.stop:
				h.done <- nil
				return
			case request := <-h.calls:
				request.done <- request.call(server, surfaces, vk)
			case <-ticker.C:
				surfaces, err = server.Poll()
				retire()
				if err != nil {
					h.done <- err
					return
				}
			}
		}
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("private nested compositor startup timed out")
	}
	t.Cleanup(func() {
		close(h.stop)
		select {
		case err := <-h.done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("private nested compositor shutdown timed out")
		}
	})
	return h
}

func (h *isolatedNestedHost) call(t *testing.T, callback func(*apps.Server, []apps.Surface, *native.VK) error) {
	t.Helper()
	r := isolatedNestedRequest{call: callback, done: make(chan error, 1)}
	select {
	case h.calls <- r:
	case <-time.After(3 * time.Second):
		t.Fatal("private nested compositor stopped servicing requests")
	}
	select {
	case err := <-r.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("private nested compositor request timed out")
	}
}

func TestIsolatedNestedWorkspaceInputAndRecoveryGPU(t *testing.T) {
	if os.Getenv("WORLDR_TEST_NESTED") != "1" {
		t.Skip("set WORLDR_TEST_NESTED=1 for private nested Vulkan/input/recovery verification")
	}
	compositor := newIsolatedNestedHost(t, 960, 600)
	t.Setenv("WAYLAND_DISPLAY", compositor.socket)
	t.Setenv("WAYLAND_SOCKET", "")
	if err := os.Unsetenv("WAYLAND_SOCKET"); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	canary := filepath.Join(directory, "typed.txt")
	// Static shell code; the canary path is a separate argument, never source.
	script := `stty -echo
printf '\033]2;nested-ready\007'
while IFS= read -r line; do printf '%s\n' "$line" >> "$1"; done`
	terminals := nativeapps.NewManager(nativeapps.Options{Command: "/bin/sh", Args: []string{"-c", script, "worldr-nested-test", canary}})
	photos := photoapp.NewCollection()
	models := modelapp.NewManager()
	hub := newApplicationHub(terminals, photos, models)
	defer hub.Close()
	terminalKey, err := terminals.LaunchApplication("terminal")
	if err != nil {
		t.Fatal(err)
	}
	photoPath := filepath.Join(directory, "gradient.png")
	photo := image.NewNRGBA(image.Rect(0, 0, 320, 180))
	for y := 0; y < 180; y++ {
		for x := 0; x < 320; x++ {
			photo.SetNRGBA(x, y, color.NRGBA{uint8(x * 255 / 319), uint8(y * 255 / 179), 160, 255})
		}
	}
	file, err := os.Create(photoPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(file, photo); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	file, err = os.Open(photoPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = photos.OpenFile(file, "gradient.png"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	modelPath := filepath.Join(directory, "tetrahedron.obj")
	if err = os.WriteFile(modelPath, []byte("o Tetrahedron\nv 0 1 0\nv -1 -1 1\nv 1 -1 1\nv 0 -1 -1\nf 1 2 3\nf 1 4 2\nf 1 3 4\nf 2 4 3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err = os.Open(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = models.OpenFile(file, "tetrahedron.obj"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	work, err := workspace.NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Close()
	work.SetApplications(hub)
	defer work.SetApplications(nil)
	p, err := openPresenter(Options{Width: 960, Height: 600, GPUMemoryMiB: 512}, "nested", "worldr isolated verification", work.Atlas())
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	metrics := runMetrics{start: time.Now()}
	var hostEvents []host.Event
	dispatch := func() {
		t.Helper()
		var err error
		hostEvents, err = p.win.Poll(hostEvents)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range hostEvents {
			if event.Kind == host.Close {
				t.Fatal("private compositor unexpectedly closed workspace")
			}
			e := hostEvent(event)
			if e.Kind == experience.KeyInput || e.Kind == experience.PointerMove || e.Kind == experience.PointerDown || e.Kind == experience.PointerScroll {
				metrics.dispatch(time.Now())
			}
			hub.seat(e)
			if quit, err := dispatchEvent(work, e, "", io.Discard); err != nil || quit {
				t.Fatalf("input requested exit: quit=%v err=%v", quit, err)
			}
		}
	}
	renderFrame := func() {
		t.Helper()
		begin := time.Now()
		dispatch()
		if err := hub.Poll(); err != nil {
			t.Fatal(err)
		}
		work.Update(0)
		frame := work.Draw(p.w, p.h)
		beforeRender := time.Now()
		if err := p.vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, nil); err != nil {
			t.Fatal(err)
		}
		metrics.frame(begin, beforeRender, time.Now(), p.vk)
		for _, id := range hub.RetiredTextures() {
			p.vk.ReleaseTexture(id)
		}
		for _, id := range work.RetiredTextures() {
			p.vk.ReleaseTexture(id)
		}
		for _, id := range hub.RetiredGeometryIDs() {
			p.vk.ReleaseGeometry(id)
		}
	}
	until := func(reason string, predicate func() bool) {
		t.Helper()
		deadline := time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) {
			renderFrame()
			if predicate() {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", reason)
	}
	until("loaded terminal/photo/model", func() bool {
		if len(hub.Surfaces()) != 3 || len(photos.SessionStates()) != 1 {
			return false
		}
		terminalReady, modelReady := false, false
		for _, surface := range hub.Surfaces() {
			terminalReady = terminalReady || surface.Key == terminalKey && strings.Contains(surface.Title, "nested-ready")
			modelReady = modelReady || surface.Spatial != nil && len(surface.Spatial.Objects) > 0
		}
		return terminalReady && modelReady
	})
	if err := work.Dispatch(workspace.Action{Kind: workspace.SelectApplication, ApplicationKey: terminalKey}); err != nil {
		t.Fatal(err)
	}
	renderFrame()
	var surfaceID uint64
	compositor.call(t, func(server *apps.Server, surfaces []apps.Surface, vk *native.VK) error {
		if len(surfaces) != 1 || surfaces[0].Texture == nil {
			return fmt.Errorf("nested Vulkan frames did not map one GPU surface")
		}
		surfaceID = surfaces[0].ID
		return server.Focus(surfaceID)
	})
	stroke := func(code, mods uint32) {
		t.Helper()
		compositor.call(t, func(server *apps.Server, _ []apps.Surface, vk *native.VK) error {
			for _, down := range []bool{true, false} {
				if err := server.Key(code, down, uint32(time.Since(metrics.start)/time.Millisecond), mods, 0, 0, 0); err != nil {
					return err
				}
			}
			return server.Modifiers(0, 0, 0, 0)
		})
	}
	stroke(24, 12) // Ctrl+Alt+O, through the actual XKB/Wayland input path.
	until("overview from nested keyboard", func() bool { return work.Document().View.Application.Overview })
	stroke(28, 0)
	until("overview exit without PTY focus", func() bool { return !work.Document().View.Application.Overview })
	if work.OwnsKeyboard() || hub.focused != 0 {
		t.Fatal("overview Enter leaked into the PTY")
	}
	stroke(28, 0)
	until("explicit terminal activation", func() bool { return work.OwnsKeyboard() && hub.focused != 0 })
	typeLine := func(codes []uint32, want string) {
		t.Helper()
		for _, code := range codes {
			stroke(code, 0)
			renderFrame()
		}
		stroke(28, 0)
		until("typed PTY canary", func() bool { data, err := os.ReadFile(canary); return err == nil && string(data) == want })
	}
	typeLine([]uint32{17, 24, 19, 38, 32, 19}, "worldr\n")
	// One protocol pointer event per rendered frame provides an actual input
	// sample distribution. This is dispatch-to-render-return, not photon time.
	for i := 0; i < 90; i++ {
		compositor.call(t, func(server *apps.Server, _ []apps.Surface, vk *native.VK) error {
			return server.Pointer(surfaceID, float32(20+i%100), float32(80+i%30))
		})
		renderFrame()
		time.Sleep(8 * time.Millisecond)
	}
	beforeDocument, err := work.CheckpointState()
	if err != nil {
		t.Fatal(err)
	}
	beforeFocus := hub.focused
	beforeSurfaces := append([]experience.ApplicationSurface(nil), hub.Surfaces()...)
	if err := p.resize(0, 0); !errors.Is(err, native.ErrNotReady) {
		t.Fatal("zero extent must defer without invalidating content", err)
	}
	if p.w != 960 || p.h != 600 {
		t.Fatal("zero extent changed live framebuffer")
	}
	// A real budget failure during WSI target replacement leaves a typed
	// recoverable renderer state, without killing any application resources.
	if err := p.vk.SetMemoryBudget(p.vk.MemoryStats().AllocatedBytes); err != nil {
		t.Fatal(err)
	}
	compositor.call(t, func(server *apps.Server, _ []apps.Surface, vk *native.VK) error {
		return server.Resize(surfaceID, 1280, 800)
	})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		dispatch()
		w, h := p.win.Size()
		if w == 1280 && h == 800 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if w, h := p.win.Size(); w != 1280 || h != 800 {
		t.Fatalf("nested resize configure missing: %dx%d", w, h)
	}
	if err := p.resize(1280, 800); !errors.Is(err, native.ErrOutOfMemory) {
		t.Fatal("real target allocation did not hit budget", err)
	}
	cause := p.vk.RenderFrame(work.Draw(p.w, p.h), [4]float32{0, 0, 0, 1}, nil)
	if !errors.Is(cause, native.ErrNeedsRecovery) || !recoverableGraphics(cause) {
		t.Fatal("failed WSI resize did not expose a recoverable state", cause)
	}
	if err := p.vk.SetMemoryBudget(512 << 20); err != nil {
		t.Fatal(err)
	}
	var recovery graphicsRecovery
	if err := p.recover(&recovery, time.Now(), cause); err != nil {
		t.Fatal(err)
	}
	metrics.recoveries++
	metrics.resizes++
	if p.w != 1280 || p.h != 800 {
		t.Fatalf("recovery lost requested extent: %dx%d", p.w, p.h)
	}
	renderFrame()
	afterResize, err := work.CheckpointState()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeDocument, afterResize) || hub.focused != beforeFocus || !work.OwnsKeyboard() {
		t.Fatal("failed resize/recovery altered document or live PTY focus")
	}
	// Show all retained app content before the second recreation. Read mode
	// hides background apps and would not exercise model/photo residency.
	stroke(24, 12)
	until("overview after first recovery", func() bool { return work.Document().View.Application.Overview })
	stroke(28, 0)
	until("mixed spatial view after first recovery", func() bool { return !work.Document().View.Application.Overview && !work.OwnsKeyboard() })
	stroke(1, 0) // Escape leaves the remembered Read layout for the spatial view.
	until("all applications visible", func() bool { return !work.Document().View.Application.Reading && !work.OwnsKeyboard() })
	beforeDocument, err = work.CheckpointState()
	if err != nil {
		t.Fatal(err)
	}
	beforeFocus = hub.focused
	stableFrame := work.Draw(p.w, p.h)
	wantedTextures := map[uint64]bool{}
	wantedGeometry := map[uint64]bool{}
	for _, surface := range hub.Surfaces() {
		wantedTextures[surface.Texture.ID()] = false
		if surface.Spatial != nil {
			for _, object := range surface.Spatial.Objects {
				if object.Node.Mesh != nil {
					wantedGeometry[object.Node.Mesh.Geometry().ID()] = false
				}
			}
		}
	}
	for _, command := range stableFrame.Commands {
		for _, draw := range command.Draws {
			if draw.Texture != nil {
				if _, ok := wantedTextures[draw.Texture.ID()]; ok {
					wantedTextures[draw.Texture.ID()] = true
				}
			}
			if draw.Geometry != nil {
				if _, ok := wantedGeometry[draw.Geometry.ID()]; ok {
					wantedGeometry[draw.Geometry.ID()] = true
				}
			}
		}
	}
	for id, found := range wantedTextures {
		if !found {
			t.Fatalf("retained application texture %d absent from recovery scene", id)
		}
	}
	for id, found := range wantedGeometry {
		if !found {
			t.Fatalf("native model geometry %d absent from recovery scene", id)
		}
	}

	snapshot := func() []byte {
		t.Helper()
		var previous uint64
		compositor.call(t, func(_ *apps.Server, surfaces []apps.Surface, _ *native.VK) error {
			if len(surfaces) != 1 {
				return fmt.Errorf("missing private output")
			}
			previous = surfaces[0].Revision
			return nil
		})
		if err := p.vk.RenderFrame(stableFrame, [4]float32{0, 0, 0, 1}, nil); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			var pixels []byte
			compositor.call(t, func(_ *apps.Server, surfaces []apps.Surface, vk *native.VK) error {
				if len(surfaces) != 1 || surfaces[0].Revision == previous {
					return nil
				}
				surface := surfaces[0]
				if surface.Texture == nil || surface.Width != p.w || surface.Height != p.h {
					return fmt.Errorf("private output lost resized GPU image")
				}
				if err := vk.Resize(uint32(p.w), uint32(p.h)); err != nil {
					return err
				}
				pixels = make([]byte, p.w*p.h*4)
				frame := render.Frame{Commands: []render.Command{{Kind: render.ImageCommand, Image: render.Image{Texture: surface.Texture, Bounds: [4]float32{0, 0, float32(p.w), float32(p.h)}}}}}
				return vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels)
			})
			if pixels != nil {
				return pixels
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("private output failed to commit latest GPU frame")
		return nil
	}
	baseline := snapshot()
	// The second attempt exercises a complete device recreation at unchanged
	// extent, allowing exact retained mesh/photo/terminal/glyph pixel comparison.
	if err := p.recover(&recovery, time.Now(), native.ErrDeviceLost); err != nil {
		t.Fatal(err)
	}
	metrics.recoveries++
	rebuilt := snapshot()
	if !bytes.Equal(baseline, rebuilt) {
		t.Fatal("loaded workspace pixels changed across device recreation")
	}
	if err := p.recover(&recovery, time.Now(), native.ErrDeviceLost); err == nil {
		t.Fatal("persistent failure exceeded host recovery limit")
	}
	afterDocument, err := work.CheckpointState()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeDocument, afterDocument) || hub.focused != beforeFocus || work.OwnsKeyboard() {
		t.Fatal("device recreation altered mixed workspace state or focus")
	}
	afterSurfaces := hub.Surfaces()
	if len(afterSurfaces) != len(beforeSurfaces) {
		t.Fatal("recovery lost a provider")
	}
	for i, surface := range afterSurfaces {
		if surface.ID != beforeSurfaces[i].ID || surface.Texture != beforeSurfaces[i].Texture {
			t.Fatal("recovery replaced a live application identity or retained texture")
		}
	}
	stroke(28, 0)
	until("same terminal activated after mixed recovery", func() bool { return work.OwnsKeyboard() && hub.focused != 0 })
	typeLine([]uint32{30, 34, 30, 23, 49}, "worldr\nagain\n")
	renderFrame()
	if metrics.input.count < 60 || metrics.inputEvents < 100 {
		t.Fatalf("too few real nested input samples: samples=%d events=%d", metrics.input.count, metrics.inputEvents)
	}
	metrics.end = time.Now()
	metrics.sampleMemory(p.vk)
	t.Logf("private nested frames=%d input events=%d samples=%d dispatch-to-render mean=%.3fms p99=%.3fms, recoveries=%d", metrics.render.count, metrics.inputEvents, metrics.input.count, metrics.input.report().MeanMS, metrics.input.report().P99MS, metrics.recoveries)
	if destination := os.Getenv("WORLDR_CAPTURE_DIR"); destination != "" {
		if err := metrics.write(filepath.Join(destination, "nested-input-recovery.json"), "nested-private", p.w, p.h, nil); err != nil {
			t.Fatal(err)
		}
		if err := savePNG(filepath.Join(destination, "nested-input-recovery.png"), rebuilt, p.w, p.h); err != nil {
			t.Fatal(err)
		}
	}
}
