//go:build linux

package wlclient

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/compositor/wlsrv"
	"github.com/codemodify/worldr/internal/engine"
)

func TestNestedClientAgainstWorldr(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", "wayland-nest")

	scene := engine.NewScene()
	s, err := wlsrv.Listen("wayland-nest", scene, 800, 600, nil)
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

	win, err := Open("nest-test", 320, 200, false)
	if err != nil {
		t.Fatal(err)
	}
	defer win.Close()

	// worldr advertises wl_compositor v6, wl_shm v1, xdg_wm_base v5
	cv, sv, xv := win.BoundVersions()
	if cv == 0 || sv == 0 || xv == 0 {
		t.Fatalf("missing binds compositor=%d shm=%d xdg=%d", cv, sv, xv)
	}
	if cv > 6 || sv > 1 || xv > 5 {
		t.Fatalf("bound above advertised compositor=%d shm=%d xdg=%d", cv, sv, xv)
	}

	cw, ch, stride := win.Size()
	if cw < 1 || ch < 1 || stride < cw*4 {
		t.Fatalf("size %dx%d stride %d", cw, ch, stride)
	}
	pix := make([]byte, stride*ch)
	for i := 0; i < 8; i++ {
		if err := win.Present(pix, stride); err != nil {
			t.Fatalf("present %d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
