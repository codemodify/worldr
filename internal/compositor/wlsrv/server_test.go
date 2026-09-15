package wlsrv

import (
	"net"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/wayland"
)

func TestAdvertiseGlobals(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	scene := engine.NewScene()
	s, err := Listen("wayland-test", scene, 800, 600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	done := make(chan struct{})
	go func() {
		tck := time.NewTicker(time.Millisecond)
		defer tck.Stop()
		for {
			select {
			case <-done:
				return
			case <-tck.C:
				s.Dispatch()
			}
		}
	}()
	defer close(done)

	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: s.SocketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	wr := wayland.NewWriter(c)
	rd := wayland.NewReader(c)
	if err := wr.Send(1, 1, wayland.PutU32(nil, 2), nil); err != nil {
		t.Fatal(err)
	}
	if err := wr.Send(1, 0, wayland.PutU32(nil, 3), nil); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var sawComp, sawXdg, sawDma, sawCursor, sawAct, sawPrim, sawXw, sawFrac, sawIME, sawDone bool
	for time.Now().Before(deadline) && !sawDone {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := rd.Next()
		if err != nil {
			continue
		}
		if msg.Object == 2 && msg.Opcode == 0 {
			cur := wayland.NewCursor(msg.Payload, nil)
			_, _ = cur.U32()
			iface, _ := cur.String()
			switch iface {
			case "wl_compositor":
				sawComp = true
			case "xdg_wm_base":
				sawXdg = true
			case "zwp_linux_dmabuf_v1":
				sawDma = true
			case "wp_cursor_shape_manager_v1":
				sawCursor = true
			case "xdg_activation_v1":
				sawAct = true
			case "zwp_primary_selection_device_manager_v1":
				sawPrim = true
			case "xwayland_shell_v1":
				sawXw = true
			case "wp_fractional_scale_manager_v1":
				sawFrac = true
			case "zwp_text_input_manager_v3":
				sawIME = true
			}
		}
		if msg.Object == 3 && msg.Opcode == 0 {
			sawDone = true
		}
	}
	if !sawComp || !sawXdg || !sawDma || !sawCursor || !sawAct || !sawPrim || !sawXw || !sawFrac || !sawIME || !sawDone {
		t.Fatalf("globals compositor=%v xdg=%v dmabuf=%v cursor=%v activation=%v primary=%v xwayland=%v fractional=%v text-input=%v done=%v",
			sawComp, sawXdg, sawDma, sawCursor, sawAct, sawPrim, sawXw, sawFrac, sawIME, sawDone)
	}
}
