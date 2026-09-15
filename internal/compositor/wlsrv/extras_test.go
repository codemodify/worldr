package wlsrv

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/wayland"
)

func TestActivationTokenDone(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	scene := engine.NewScene()
	s, err := Listen("wayland-act", scene, 800, 600, nil)
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

	deadline := time.Now().Add(2 * time.Second)
	var actName uint32
	for time.Now().Before(deadline) && actName == 0 {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := rd.Next()
		if err != nil {
			continue
		}
		if msg.Object == 2 && msg.Opcode == 0 {
			cur := wayland.NewCursor(msg.Payload, nil)
			name, _ := cur.U32()
			iface, _ := cur.String()
			if iface == "xdg_activation_v1" {
				actName = name
			}
		}
	}
	if actName == 0 {
		t.Fatal("xdg_activation_v1 not advertised")
	}

	const (
		actID   = 3
		tokenID = 4
	)
	p := wayland.PutU32(nil, actName)
	p = wayland.PutString(p, "xdg_activation_v1")
	p = wayland.PutU32(p, 1)
	p = wayland.PutU32(p, actID)
	if err := wr.Send(2, 0, p, nil); err != nil {
		t.Fatal(err)
	}
	if err := wr.Send(actID, 1, wayland.PutU32(nil, tokenID), nil); err != nil {
		t.Fatal(err)
	}
	if err := wr.Send(tokenID, 4, nil, nil); err != nil {
		t.Fatal(err)
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := rd.Next()
		if err != nil {
			continue
		}
		if msg.Object == tokenID && msg.Opcode == 0 {
			cur := wayland.NewCursor(msg.Payload, nil)
			tok, err := cur.String()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(tok, "worldr-") {
				t.Fatalf("token %q", tok)
			}
			return
		}
	}
	t.Fatal("no xdg_activation_token.done")
}

func TestFractionalScalePreferredScale(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	scene := engine.NewScene()
	s, err := Listen("wayland-frac", scene, 800, 600, nil)
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

	deadline := time.Now().Add(2 * time.Second)
	var fracName uint32
	for time.Now().Before(deadline) && fracName == 0 {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := rd.Next()
		if err != nil {
			continue
		}
		if msg.Object == 2 && msg.Opcode == 0 {
			cur := wayland.NewCursor(msg.Payload, nil)
			name, _ := cur.U32()
			iface, _ := cur.String()
			if iface == "wp_fractional_scale_manager_v1" {
				fracName = name
			}
		}
	}
	if fracName == 0 {
		t.Fatal("wp_fractional_scale_manager_v1 not advertised")
	}

	const (
		compID = 3
		surfID = 4
		mgrID  = 5
		fracID = 6
	)
	if err := wr.Send(2, 0, bindPayload(globalCompositor, "wl_compositor", 4, compID), nil); err != nil {
		t.Fatal(err)
	}
	if err := wr.Send(compID, 0, wayland.PutU32(nil, surfID), nil); err != nil {
		t.Fatal(err)
	}
	if err := wr.Send(2, 0, bindPayload(fracName, "wp_fractional_scale_manager_v1", 1, mgrID), nil); err != nil {
		t.Fatal(err)
	}
	p := wayland.PutU32(nil, fracID)
	p = wayland.PutU32(p, surfID)
	if err := wr.Send(mgrID, 1, p, nil); err != nil {
		t.Fatal(err)
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := rd.Next()
		if err != nil {
			continue
		}
		if msg.Object == fracID && msg.Opcode == 0 {
			cur := wayland.NewCursor(msg.Payload, nil)
			scale, err := cur.U32()
			if err != nil {
				t.Fatal(err)
			}
			if scale != PreferredScale120ths {
				t.Fatalf("preferred_scale %d want %d", scale, PreferredScale120ths)
			}
			return
		}
	}
	t.Fatal("no wp_fractional_scale_v1.preferred_scale")
}

func bindPayload(name uint32, iface string, ver, id uint32) []byte {
	p := wayland.PutU32(nil, name)
	p = wayland.PutString(p, iface)
	p = wayland.PutU32(p, ver)
	return wayland.PutU32(p, id)
}
