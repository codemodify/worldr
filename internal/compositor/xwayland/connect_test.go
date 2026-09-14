package xwayland

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/compositor/wlsrv"
	"github.com/codemodify/worldr/internal/engine"
)

// TestXwaylandConnectsAsWaylandClient starts real Xwayland + the tiny XWM
// against worldr. xeyes must appear as a scene actor (SSD path).
func TestXwaylandConnectsAsWaylandClient(t *testing.T) {
	if _, err := exec.LookPath("Xwayland"); err != nil {
		t.Skip("Xwayland not installed")
	}
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WORLDR_XWAYLAND", "")
	scene := engine.NewScene()
	s, err := wlsrv.Listen("wayland-xw", scene, 800, 600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	stop := make(chan struct{})
	go func() {
		tck := time.NewTicker(time.Millisecond)
		defer tck.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tck.C:
				s.Dispatch()
			}
		}
	}()
	defer close(stop)

	// Dispatch goroutine above already pumps the compositor (do not pass
	// s.Dispatch here — Start would race a second pump).
	in, err := Start(s.DisplayName, dir, nil)
	if err != nil {
		t.Skipf("Xwayland did not become ready (needs EGL + typically /dev/dri): %v", err)
	}
	defer in.Close()
	if in.Display == "" || in.Display[0] != ':' {
		t.Fatalf("display %q", in.Display)
	}
	if in.WM == nil {
		t.Log("XWM did not attach; managed X11 windows may not map")
	}
	xeyes, err := exec.LookPath("xeyes")
	if err != nil {
		t.Log("xeyes not installed; only checked Xwayland start")
		return
	}
	cmd := exec.Command(xeyes)
	cmd.Env = []string{
		"DISPLAY=" + in.Display,
		"XDG_RUNTIME_DIR=" + dir,
		"HOME=" + dir,
		"PATH=" + os.Getenv("PATH"),
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()

	sock := fmt.Sprintf("/tmp/.X11-unix/X%d", in.DisplayNum)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		if scene.HasActors() {
			return
		}
	}
	alive := cmd.Process != nil && cmd.Process.Signal(syscall.Signal(0)) == nil
	_, sockErr := os.Stat(sock)
	t.Fatalf("xeyes mapped no worldr actor display=%s socket=%v xeyes_running=%v (see wlsrv commit logs)",
		in.Display, sockErr, alive)
}
