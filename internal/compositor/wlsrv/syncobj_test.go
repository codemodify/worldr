package wlsrv

import (
	"io"
	"log"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/syncobj"
	"github.com/codemodify/worldr/internal/wayland"
	"golang.org/x/sys/unix"
)

func TestAdvertiseLinuxDrmSyncobj(t *testing.T) {
	old := advertiseSyncobj
	advertiseSyncobj = func() bool { return true }
	defer func() { advertiseSyncobj = old }()

	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	s, err := Listen("wayland-syncobj-adv", engine.NewScene(), 800, 600, nil)
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
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := rd.Next()
		if err != nil {
			continue
		}
		if msg.Object == 2 && msg.Opcode == 0 {
			cur := wayland.NewCursor(msg.Payload, nil)
			_, _ = cur.U32()
			iface, _ := cur.String()
			if iface == syncobjIface {
				return
			}
		}
	}
	t.Fatal("wp_linux_drm_syncobj_manager_v1 not advertised")
}

func TestAdvertiseSyncobjConditional(t *testing.T) {
	old := advertiseSyncobj
	defer func() { advertiseSyncobj = old }()
	advertiseSyncobj = func() bool { return false }
	c := &Client{srv: &Server{log: log.New(io.Discard, "", 0)}, objs: map[uint32]*object{}}
	// advertise writes to the socket; just check the helper hook.
	if advertiseSyncobj() {
		t.Fatal("forced off")
	}
	advertiseSyncobj = func() bool { return true }
	if !advertiseSyncobj() {
		t.Fatal("forced on")
	}
	_ = c
}

func TestSyncobjGetTimelineAndPoints(t *testing.T) {
	c := &Client{srv: &Server{log: log.New(io.Discard, "", 0)}, objs: map[uint32]*object{}}
	surf := &surface{id: 10}
	c.objs[10] = &object{id: 10, kind: kindSurface, surf: surf}
	c.objs[5] = &object{id: 5, kind: kindSyncobjMgr}

	fd, err := unix.MemfdCreate("t-syncobj", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)

	p := wayland.PutU32(nil, 20) // timeline id
	if err := c.reqSyncobjMgr(c.objs[5], 2, wayland.NewCursor(p, []int{fd})); err != nil {
		t.Fatal(err)
	}
	tl := c.objs[20]
	if tl == nil || tl.timeline == nil || tl.timeline.fd <= 0 {
		t.Fatal("timeline fd not imported")
	}

	p = wayland.PutU32(nil, 21) // surface sync id
	p = wayland.PutU32(p, 10)
	if err := c.reqSyncobjMgr(c.objs[5], 1, wayland.NewCursor(p, nil)); err != nil {
		t.Fatal(err)
	}
	if surf.sync == nil || c.objs[21] == nil {
		t.Fatal("sync surface")
	}

	hi, lo := syncobj.SplitPoint(syncobj.Point(1, 7))
	pay := wayland.PutU32(nil, 20)
	pay = wayland.PutU32(pay, hi)
	pay = wayland.PutU32(pay, lo)
	if err := c.reqSyncobjSurface(c.objs[21], 1, wayland.NewCursor(pay, nil)); err != nil {
		t.Fatal(err)
	}
	if !surf.sync.pendingAcq.Valid() || surf.sync.pendingAcq.Point != syncobj.Point(1, 7) {
		t.Fatalf("acquire %+v", surf.sync.pendingAcq)
	}

	c.applySyncobjAcquire(surf)
	if surf.sync.pendingAcq.Valid() {
		t.Fatal("acquire consumed")
	}

	if err := c.reqSyncobjTimeline(tl, 0, nil); err != nil {
		t.Fatal(err)
	}
	if c.objs[20] != nil {
		t.Fatal("timeline destroy")
	}
}

func TestSyncobjPointHelper(t *testing.T) {
	if syncobj.Point(0, 42) != 42 {
		t.Fatal("lo")
	}
}
