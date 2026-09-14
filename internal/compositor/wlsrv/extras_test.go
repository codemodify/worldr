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
