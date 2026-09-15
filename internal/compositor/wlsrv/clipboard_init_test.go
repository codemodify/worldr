package wlsrv

import (
	"net"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/wayland"
)

// TestArkInitRoundtripNoPrematureSelection replays the WAYLAND_DEBUG
// sequence from ark (and the Brave Qt shim) on worldr 0.9.25: bind seat v8,
// get_data_device, get_primary device, then a second display.sync.
//
// Check: that sync's callback.done must arrive, and data_device.selection
// must not. After get_keyboard, keymap + repeat_info must arrive.
func TestArkInitRoundtripNoPrematureSelection(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	scene := engine.NewScene()
	s, err := Listen("wayland-qt-init", scene, 1280, 684, nil)
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

	uc, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: s.SocketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer uc.Close()
	wr := wayland.NewWriter(uc)
	rd := wayland.NewReader(uc)

	if err := wr.Send(1, 1, wayland.PutU32(nil, 2), nil); err != nil { // get_registry
		t.Fatal(err)
	}
	if err := wr.Send(1, 0, wayland.PutU32(nil, 3), nil); err != nil { // sync #1
		t.Fatal(err)
	}
	if !waitCallback(t, uc, rd, 3) {
		t.Fatal("first sync")
	}

	const (
		seatID    = 6
		ddmgrID   = 9
		dataDevID = 10
		primMgrID = 13
		primDevID = 14
		sync2ID   = 16
		kbdID     = 17
		sync3ID   = 20
	)
	binds := []struct {
		name  uint32
		iface string
		ver   uint32
		id    uint32
	}{
		{globalSeat, "wl_seat", 8, seatID},
		{globalDataDev, "wl_data_device_manager", 3, ddmgrID},
		{globalPrimary, "zwp_primary_selection_device_manager_v1", 1, primMgrID},
	}
	for _, b := range binds {
		if err := wr.Send(2, 0, bindPayload(b.name, b.iface, b.ver, b.id), nil); err != nil {
			t.Fatal(err)
		}
	}
	p := wayland.PutU32(nil, dataDevID)
	p = wayland.PutU32(p, seatID)
	if err := wr.Send(ddmgrID, 1, p, nil); err != nil { // get_data_device
		t.Fatal(err)
	}
	p = wayland.PutU32(nil, primDevID)
	p = wayland.PutU32(p, seatID)
	if err := wr.Send(primMgrID, 1, p, nil); err != nil { // get_device
		t.Fatal(err)
	}
	if err := wr.Send(1, 0, wayland.PutU32(nil, sync2ID), nil); err != nil {
		t.Fatal(err)
	}

	ev := drainUntilCallback(t, uc, rd, sync2ID)
	if ev.selection {
		t.Fatal("wl_data_device.selection during init roundtrip (Qt6 SEGV)")
	}
	if ev.primSel {
		t.Fatal("zwp_primary_selection.selection during init roundtrip")
	}
	if !ev.seatCaps {
		t.Fatal("expected wl_seat.capabilities")
	}

	if err := wr.Send(seatID, 1, wayland.PutU32(nil, kbdID), nil); err != nil { // get_keyboard
		t.Fatal(err)
	}
	if err := wr.Send(1, 0, wayland.PutU32(nil, sync3ID), nil); err != nil {
		t.Fatal(err)
	}
	ev = drainUntilCallback(t, uc, rd, sync3ID)
	if !ev.keymap {
		t.Fatal("wl_keyboard.keymap missing after get_keyboard")
	}
	if !ev.repeat {
		t.Fatal("wl_keyboard.repeat_info missing (seat v4+)")
	}
	if ev.selection || ev.primSel {
		t.Fatal("selection still must wait for keyboard.enter")
	}
}

type initEvents struct {
	seatCaps   bool
	selection  bool
	primSel    bool
	keymap     bool
	repeat     bool
	callbackOK bool
}

func drainUntilCallback(t *testing.T, c *net.UnixConn, rd *wayland.Reader, cb uint32) initEvents {
	t.Helper()
	var ev initEvents
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := rd.Next()
		if err != nil {
			continue
		}
		switch {
		case msg.Object == cb && msg.Opcode == 0:
			ev.callbackOK = true
			return ev
		case msg.Object == 6 && msg.Opcode == 0: // wl_seat.capabilities
			ev.seatCaps = true
		case msg.Object == 10 && msg.Opcode == wlDataDevSelection:
			ev.selection = true
		case msg.Object == 14 && msg.Opcode == primDevSelection:
			ev.primSel = true
		case msg.Object == 17 && msg.Opcode == 0: // keymap
			ev.keymap = true
			if _, err := rd.TakeFD(); err != nil {
				t.Fatalf("keymap fd: %v", err)
			}
		case msg.Object == 17 && msg.Opcode == 5: // repeat_info
			ev.repeat = true
		}
	}
	if !ev.callbackOK {
		t.Fatal("sync callback.done never arrived")
	}
	return ev
}

func waitCallback(t *testing.T, c *net.UnixConn, rd *wayland.Reader, cb uint32) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := rd.Next()
		if err != nil {
			continue
		}
		if msg.Object == cb && msg.Opcode == 0 {
			return true
		}
	}
	return false
}
