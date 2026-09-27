//go:build linux && cgo

package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/glass"
	"github.com/codemodify/worldr/internal/platform/linux/apps"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/terminal"
	kit "github.com/codemodify/worldr/sdk/app/v1"
)

func TestGlassTerminalPrivateWaylandTypingResizeClipboardGPU(t *testing.T) {
	if os.Getenv("WORLDR_TEST_NESTED") != "1" {
		t.Skip("set WORLDR_TEST_NESTED=1 for isolated glass terminal Wayland verification")
	}
	compositor := newIsolatedNestedHost(t, 900, 600)
	t.Setenv("WAYLAND_DISPLAY", compositor.socket)
	t.Setenv("WAYLAND_SOCKET", "")
	if err := os.Unsetenv("WAYLAND_SOCKET"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	answer, sizePath := filepath.Join(dir, "typed"), filepath.Join(dir, "pty-size")
	script := `stty -echo
printf '\033]2;glass-native-ready\007\033[36mGlass terminal native integration\033[0m\n'
while IFS= read -r line; do printf '%s\n' "$line" >> "$1"; stty size > "$2"; done`
	prefs := glass.DefaultPreferences()
	prefs.Motion = false
	application, err := glass.New(terminal.Options{Command: "/bin/sh", Args: []string{"-c", script, "glass-integration", answer, sizePath}, Cols: 80, Rows: 24}, prefs)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- kit.Run(ctx, kit.Options{Title: "WorldR Terminal verification", Width: 900, Height: 600, FPS: 120, Transparent: true, ClientDecorated: true}, application)
	}()
	finished := false
	t.Cleanup(func() {
		cancel()
		if !finished {
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(5 * time.Second):
				t.Error("terminal failed to stop")
			}
		}
	})
	var surfaceID uint64
	var surfaceWidth, surfaceHeight int
	clipboardRequests := 0
	var latestOffer apps.ClipboardOffer
	const clipboardText = "pasted-through-private-wayland\n"
	until := func(reason string, predicate func([]apps.Surface) bool) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			matched := false
			compositor.call(t, func(server *apps.Server, surfaces []apps.Surface, _ *native.VK) error {
				for _, request := range server.PollClipboardRequests() {
					file := os.NewFile(uintptr(request.FD), "private-clipboard")
					if request.ExternalID != 71 {
						file.Close()
						return fmt.Errorf("unexpected clipboard source %d", request.ExternalID)
					}
					_, err := file.WriteString(clipboardText)
					file.Close()
					if err != nil {
						return err
					}
					clipboardRequests++
				}
				latestOffer = server.ClipboardOffer()
				matched = predicate(surfaces)
				return nil
			})
			if matched {
				return
			}
			select {
			case err := <-done:
				finished = true
				t.Fatalf("terminal exited while waiting for %s: %v", reason, err)
			default:
			}
			time.Sleep(3 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", reason)
	}
	until("real transparent window", func(surfaces []apps.Surface) bool {
		if len(surfaces) != 1 || !strings.Contains(surfaces[0].Title, "glass-native-ready") {
			return false
		}
		surfaceID = surfaces[0].ID
		surfaceWidth, surfaceHeight = surfaces[0].Width, surfaces[0].Height
		return true
	})
	compositor.call(t, func(server *apps.Server, _ []apps.Surface, _ *native.VK) error { return server.Focus(surfaceID) })
	stroke := func(code, mods uint32) {
		t.Helper()
		compositor.call(t, func(server *apps.Server, _ []apps.Surface, _ *native.VK) error {
			for _, down := range []bool{true, false} {
				if err := server.Key(code, down, 1, mods, 0, 0, 0); err != nil {
					return err
				}
			}
			return server.Modifiers(0, 0, 0, 0)
		})
	}
	for _, code := range []uint32{17, 24, 19, 38, 32, 19, 28} {
		stroke(code, 0)
	}
	until("Wayland keys reaching PTY", func([]apps.Surface) bool {
		data, err := os.ReadFile(answer)
		return err == nil && string(data) == "worldr\n"
	})
	var initialRows, initialCols int
	until("initial PTY size", func([]apps.Surface) bool {
		data, err := os.ReadFile(sizePath)
		if err != nil {
			return false
		}
		n, _ := fmt.Sscanf(string(data), "%d %d", &initialRows, &initialCols)
		return n == 2
	})
	compositor.call(t, func(server *apps.Server, _ []apps.Surface, _ *native.VK) error {
		return server.Resize(surfaceID, 1100, 760)
	})
	until("resized Vulkan presentation", func(surfaces []apps.Surface) bool {
		return len(surfaces) == 1 && surfaces[0].Width == 1100 && surfaces[0].Height == 760
	})
	for _, code := range []uint32{30, 34, 30, 23, 49, 28} {
		stroke(code, 0)
	}
	until("typing after resize", func([]apps.Surface) bool {
		data, err := os.ReadFile(answer)
		return err == nil && string(data) == "worldr\nagain\n"
	})
	until("resized PTY rows and columns", func([]apps.Surface) bool {
		data, err := os.ReadFile(sizePath)
		if err != nil {
			return false
		}
		var rows, cols int
		_, err = fmt.Sscanf(string(data), "%d %d", &rows, &cols)
		return err == nil && rows > initialRows && cols > initialCols
	})
	compositor.call(t, func(server *apps.Server, _ []apps.Surface, _ *native.VK) error {
		return server.OfferClipboard(71, []string{"text/plain;charset=utf-8", "text/plain"})
	})
	// Advertising never reads clipboard data. The real shortcut requests it.
	if clipboardRequests != 0 {
		t.Fatal("clipboard contents were read before explicit paste")
	}
	stroke(47, 5) // Ctrl+Shift+V through the compositor's XKB modifier path.
	until("Wayland clipboard entering PTY", func([]apps.Surface) bool {
		data, err := os.ReadFile(answer)
		return err == nil && string(data) == "worldr\nagain\n"+clipboardText
	})
	if clipboardRequests != 1 {
		t.Fatalf("clipboard requested %d times", clipboardRequests)
	}
	// Find owns its query and Enter/Escape while the real shell keeps running.
	// A subsequent command proves that focus returns to the PTY and that none
	// of the search keys were delivered as shell input through native XKB.
	stroke(33, 5)                                              // Ctrl+Shift+F.
	for _, code := range []uint32{34, 38, 30, 31, 31, 28, 1} { // glass, Enter, Escape.
		stroke(code, 0)
	}
	for _, code := range []uint32{30, 33, 20, 18, 19, 33, 23, 49, 32, 28} { // afterfind, Enter.
		stroke(code, 0)
	}
	until("native search isolation and restored PTY focus", func([]apps.Surface) bool {
		data, err := os.ReadFile(answer)
		return err == nil && string(data) == "worldr\nagain\n"+clipboardText+"afterfind\n"
	})
	// Drag-select the first word of the shell's first output line using the
	// actual Wayland pointer stream, then export through Ctrl+Shift+C. At the
	// private compositor's scale=1, the wide session rail places the terminal
	// body at (246,162); a 15px Hack cell is approximately nine pixels wide.
	const bodyX, bodyY = 246, 162
	compositor.call(t, func(server *apps.Server, _ []apps.Surface, _ *native.VK) error {
		if err := server.Pointer(surfaceID, bodyX+3, bodyY+10); err != nil {
			return err
		}
		if err := server.Button(0x110, true, 1); err != nil {
			return err
		}
		if err := server.Pointer(surfaceID, bodyX+47, bodyY+10); err != nil {
			return err
		}
		return server.Button(0x110, false, 2)
	})
	stroke(46, 5)
	until("selected text exported to Wayland", func([]apps.Surface) bool {
		return latestOffer.ID != 0 && latestOffer.ExternalID == 0
	})
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	compositor.call(t, func(server *apps.Server, _ []apps.Surface, _ *native.VK) error {
		return server.ReceiveClipboard(latestOffer.ID, "text/plain;charset=utf-8", int(writer.Fd()))
	})
	writer.Close()
	if err := reader.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	copied, err := io.ReadAll(reader)
	if err != nil || string(copied) != "Glass" {
		t.Fatalf("native drag selection/copy = %q, %v", copied, err)
	}
	compositor.call(t, func(_ *apps.Server, surfaces []apps.Surface, gpu *native.VK) error {
		if len(surfaces) != 1 {
			return fmt.Errorf("terminal surface disappeared")
		}
		s := surfaces[0]
		texture := s.Texture
		transport := "linux-dmabuf"
		if texture == nil {
			transport = "wl_shm"
			var err error
			texture, err = render.NewTexture(s.Width, s.Height, s.Pixels)
			if err != nil {
				return err
			}
			defer texture.Close()
			defer gpu.ReleaseTexture(texture.ID())
		}
		if err := gpu.Resize(uint32(s.Width), uint32(s.Height)); err != nil {
			return err
		}
		pixels := make([]byte, s.Width*s.Height*4)
		frame := render.Frame{LinearColor: true, Commands: []render.Command{{Kind: render.ImageCommand, Image: render.Image{Texture: texture, Bounds: [4]float32{0, 0, float32(s.Width), float32(s.Height)}}}}}
		if err := gpu.RenderFrame(frame, [4]float32{}, pixels); err != nil {
			return err
		}
		if pixels[3] != 0 {
			return fmt.Errorf("native transparent window corner has alpha %d", pixels[3])
		}
		alpha := pixels[(400*s.Width+100)*4+3]
		if alpha < 100 || alpha >= 250 {
			return fmt.Errorf("native glass interior opacity lost: alpha=%d", alpha)
		}
		t.Logf("real Wayland transport=%s; compositor GPU=%s; initial=%dx%d; resized=%dx%d; glass alpha=%d; PTY typing and clipboard passed", transport, gpu.DeviceName(), surfaceWidth, surfaceHeight, s.Width, s.Height, alpha)
		return nil
	})
	compositor.call(t, func(server *apps.Server, _ []apps.Surface, _ *native.VK) error { return server.CloseSurface(surfaceID) })
	select {
	case err := <-done:
		finished = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("native close did not stop the terminal")
	}
}
