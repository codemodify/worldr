//go:build linux && cgo

package xwayland

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/platform/linux/apps"
	"github.com/codemodify/worldr/internal/platform/linux/dmabuf"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/render"
)

func TestRealXwaylandGlamorDMABufReachesWorkspace(t *testing.T) {
	if os.Getenv("WORLDR_TEST_XWAYLAND") != "1" {
		t.Skip("set WORLDR_TEST_XWAYLAND=1 for isolated Xwayland/X11 acceptance")
	}
	if _, err := exec.LookPath("Xwayland"); err != nil {
		t.Skip(err)
	}
	xmessage, err := exec.LookPath("xmessage")
	if err != nil {
		t.Skip(err)
	}
	vk, err := native.OpenVK(false, 96, 64)
	if err != nil {
		t.Skipf("headless Vulkan unavailable: %v", err)
	}
	defer vk.Close()
	formats, renderNode := vk.DMABufFormats(), vk.RenderNode()
	if renderNode == "" || !x11DMABufFormats(formats) || !renderNodeUsable(renderNode) {
		t.Skipf("Vulkan device lacks Xwayland DMA-BUF prerequisites: node=%q formats=%v", renderNode, formats)
	}
	server, err := apps.Open(640, 400)
	if err != nil {
		t.Fatal(err)
	}
	var imported []dmabuf.Format
	if err := server.SetDMABufImporterForDevice(formats, renderNode, func(descriptor dmabuf.Descriptor) (*render.Texture, error) {
		imported = append(imported, dmabuf.Format{FourCC: descriptor.FourCC, Modifier: descriptor.Modifier})
		return vk.ImportDMABuf(descriptor)
	}); err != nil {
		server.Close()
		t.Fatal(err)
	}
	defer func() {
		server.Close()
		for _, texture := range server.RetiredTextures() {
			_ = vk.ReleaseTexture(texture.ID())
			_ = texture.Close()
		}
	}()
	bridge, err := Open(server, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	if !bridge.Accelerated() {
		t.Fatalf("eligible Xwayland fell back to SHM: %s", bridge.log.String())
	}
	cmd := exec.Command(xmessage, "-name", "worldr-glamor-test", "-title", "GPU X11 acceptance", "-geometry", "300x120", "GPU DMA-BUF")
	cmd.Env = bridge.Environment(os.Environ())
	var log boundedLog
	cmd.Stdout, cmd.Stderr = &log, &log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}()
	var surface apps.Surface
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if err := bridge.Poll(); err != nil {
			t.Fatal(err)
		}
		surfaces, err := server.Poll()
		if err != nil {
			t.Fatal(err)
		}
		for _, candidate := range surfaces {
			if candidate.Title == "GPU X11 acceptance" && len(candidate.Layers) > 0 {
				surface = candidate
				break
			}
		}
		if surface.ID != 0 {
			break
		}
		time.Sleep(3 * time.Millisecond)
	}
	if surface.ID == 0 {
		t.Fatalf("GPU X11 surface timed out; client=%q Xwayland=%q imports=%v", log.String(), bridge.log.String(), imported)
	}
	window, ok := bridge.Window(surface.ID)
	if !ok || window.ID == 0 || window.SurfaceID != surface.ID || window.Title != surface.Title {
		t.Fatalf("GPU surface lost exact XWM association: surface=%+v window=%+v", surface, window)
	}
	if surface.Texture == nil || surface.Texture.ExternalSource() == nil || len(surface.Layers) == 0 || len(surface.Pixels) != 0 || len(imported) == 0 {
		t.Fatalf("Xwayland buffer did not remain on GPU: texture=%v layers=%d pixels=%d imports=%v", surface.Texture, len(surface.Layers), len(surface.Pixels), imported)
	}
	identity := [16]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
	projection, model := identity, identity
	projection[5], model[14] = -1, .5
	frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: render.View{Projection: projection, Viewport: [4]float32{0, 0, 96, 64}}, Draws: []render.Draw{{Texture: surface.Texture, Model: model, Color: [4]float32{1, 1, 1, 1}}}}}}
	rendered := make([]byte, 96*64*4)
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, rendered); err != nil {
		t.Fatalf("workspace renderer rejected Xwayland GPU texture: %v", err)
	}
	visible := false
	for i := 0; i < len(rendered); i += 4 {
		if rendered[i] != 0 || rendered[i+1] != 0 || rendered[i+2] != 0 {
			visible = true
			break
		}
	}
	if !visible {
		t.Fatal("workspace renderer produced only the clear color for Xwayland GPU texture")
	}
	t.Logf("Xwayland GPU route imported %v on %s and rendered surface %d", imported, renderNode, surface.ID)
}

func TestRealXwaylandDisabledGlamorFallsBackToSHM(t *testing.T) {
	if os.Getenv("WORLDR_TEST_XWAYLAND") != "1" {
		t.Skip("set WORLDR_TEST_XWAYLAND=1 for isolated Xwayland/X11 acceptance")
	}
	xmessage, err := exec.LookPath("xmessage")
	if err != nil {
		t.Skip(err)
	}
	vk, err := native.OpenVK(false, 64, 64)
	if err != nil {
		t.Skipf("headless Vulkan unavailable: %v", err)
	}
	defer vk.Close()
	formats, renderNode := vk.DMABufFormats(), vk.RenderNode()
	if renderNode == "" || !x11DMABufFormats(formats) || !renderNodeUsable(renderNode) {
		t.Skip("Vulkan device lacks Xwayland DMA-BUF prerequisites")
	}
	server, err := apps.Open(480, 300)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := server.SetDMABufImporterForDevice(formats, renderNode, vk.ImportDMABuf); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XWAYLAND_NO_GLAMOR", "1")
	var startup bytes.Buffer
	bridge, err := Open(server, &startup)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	if bridge.Accelerated() || !bytes.Contains(startup.Bytes(), []byte("glamor unavailable")) {
		t.Fatalf("disabled glamor did not select explicit SHM fallback: accelerated=%v startup=%q", bridge.Accelerated(), startup.String())
	}
	cmd := exec.Command(xmessage, "-name", "worldr-shm-fallback", "-title", "X11 fallback acceptance", "-geometry", "240x100", "software fallback")
	cmd.Env = bridge.Environment(os.Environ())
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := bridge.Poll(); err != nil {
			t.Fatal(err)
		}
		surfaces, err := server.Poll()
		if err != nil {
			t.Fatal(err)
		}
		for _, surface := range surfaces {
			if surface.Title != "X11 fallback acceptance" {
				continue
			}
			if len(surface.Pixels) != surface.Width*surface.Height*4 || surface.Texture != nil || len(surface.Layers) != 0 {
				t.Fatalf("SHM fallback surface has unexpected backing: %+v", surface)
			}
			t.Logf("Xwayland startup fallback produced SHM surface %d", surface.ID)
			return
		}
		time.Sleep(3 * time.Millisecond)
	}
	t.Fatal("SHM fallback client did not reach workspace")
}
