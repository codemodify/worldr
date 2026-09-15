package wlsrv

import (
	"syscall"
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/syncobj"
	"github.com/codemodify/worldr/internal/wayland"
	"golang.org/x/sys/unix"
)

const syncobjIface = "wp_linux_drm_syncobj_manager_v1"

// advertiseSyncobj is replaced in tests. Default: DRM SYNCOBJ_TIMELINE.
var advertiseSyncobj = syncobj.TimelineAvailable

type syncTimeline struct {
	fd int
}

func (t *syncTimeline) close() {
	if t == nil || t.fd <= 0 {
		return
	}
	_ = syscall.Close(t.fd)
	t.fd = -1
}

type syncSurface struct {
	pendingAcq syncobj.Fence
	pendingRel syncobj.Fence
	readyAcq   syncobj.Fence
	readyRel   syncobj.Fence
}

func (c *Client) reqSyncobjMgr(_ *object, op uint16, cur *wayland.Cursor) error {
	switch op {
	case 0: // destroy
		return nil
	case 1: // get_surface(id, surface)
		id, err := cur.U32()
		if err != nil {
			return err
		}
		sid, err := cur.U32()
		if err != nil {
			return err
		}
		so := c.objs[sid]
		if so == nil || so.surf == nil {
			return nil
		}
		ss := &syncSurface{}
		if so.surf.sync == nil {
			so.surf.sync = ss
		} else {
			ss = so.surf.sync
		}
		c.objs[id] = &object{id: id, kind: kindSyncobjSurface, syncSurf: ss, surf: so.surf}
	case 2: // get_timeline(id, fd)
		id, err := cur.U32()
		if err != nil {
			return err
		}
		fd, err := cur.FD()
		if err != nil {
			return err
		}
		if nfd, err := unix.Dup(fd); err == nil {
			fd = nfd
		}
		c.objs[id] = &object{id: id, kind: kindSyncobjTimeline, timeline: &syncTimeline{fd: fd}}
	}
	return nil
}

func (c *Client) reqSyncobjTimeline(o *object, op uint16, _ *wayland.Cursor) error {
	if op == 0 { // destroy
		if o.timeline != nil {
			o.timeline.close()
		}
		delete(c.objs, o.id)
	}
	return nil
}

func (c *Client) reqSyncobjSurface(o *object, op uint16, cur *wayland.Cursor) error {
	ss := o.syncSurf
	if ss == nil && o.surf != nil {
		ss = o.surf.sync
	}
	if ss == nil {
		ss = &syncSurface{}
		if o.surf != nil {
			o.surf.sync = ss
		}
		o.syncSurf = ss
	}
	switch op {
	case 0: // destroy
		ss.pendingAcq.CloseFD()
		ss.pendingRel.CloseFD()
		signalFence(ss.readyRel)
		ss.readyAcq.CloseFD()
		ss.readyRel.CloseFD()
		if o.surf != nil && o.surf.sync == ss {
			o.surf.sync = nil
		}
		delete(c.objs, o.id)
	case 1: // set_acquire_point(timeline, hi, lo)
		c.setSyncPoint(&ss.pendingAcq, cur)
	case 2: // set_release_point(timeline, hi, lo)
		c.setSyncPoint(&ss.pendingRel, cur)
	}
	return nil
}

func (c *Client) setSyncPoint(dst *syncobj.Fence, cur *wayland.Cursor) {
	if dst == nil {
		return
	}
	dst.CloseFD()
	tid, _ := cur.U32()
	hi, _ := cur.U32()
	lo, _ := cur.U32()
	dst.Point = syncobj.Point(hi, lo)
	if to := c.objs[tid]; to != nil && to.timeline != nil && to.timeline.fd > 0 {
		if nfd, err := unix.Dup(to.timeline.fd); err == nil {
			dst.FD = nfd
		}
	}
}

func (c *Client) applySyncobjAcquire(s *surface) {
	if s == nil || s.sync == nil || !s.sync.pendingAcq.Valid() {
		return
	}
	f := s.sync.pendingAcq
	s.sync.pendingAcq = syncobj.Fence{}
	// Vulkan timeline wait (vkWaitSemaphores) when a waiter is attached
	// (Server.Waiter, else Import). DRM ioctl fallback. Failure → implicit sync.
	if err := syncobj.WaitAcquire(f, 100*time.Millisecond, commitWaiter(c.srv)); err != nil && c.srv != nil && c.srv.log != nil {
		c.srv.log.Printf("drm-syncobj acquire wait fallback (implicit sync): %v", err)
	}
	s.sync.readyAcq.CloseFD()
	s.sync.readyAcq = f
}

// commitWaiter prefers Server.Waiter (Vulkan session without Import) then
// an Import that implements the timeline wait. Nil → DRM ioctl only.
func commitWaiter(s *Server) syncobj.Waiter {
	if s == nil {
		return nil
	}
	if s.Waiter != nil {
		return s.Waiter
	}
	if tw, ok := s.Import.(syncobj.Waiter); ok {
		return tw
	}
	return nil
}

func (c *Client) applySyncobjRelease(s *surface) {
	if s == nil || s.sync == nil || !s.sync.pendingRel.Valid() {
		return
	}
	f := s.sync.pendingRel
	s.sync.pendingRel = syncobj.Fence{}
	// Stash until after vkCmdBlit / sample / scanout (shell signals).
	signalFence(s.sync.readyRel)
	s.sync.readyRel.CloseFD()
	s.sync.readyRel = f
}

func signalFence(f syncobj.Fence) {
	if !f.Valid() {
		return
	}
	_ = syncobj.SignalFence(f)
}

func applyActorSync(a *engine.Actor, ss *syncSurface) {
	if a == nil {
		return
	}
	releaseActorSync(a)
	if ss == nil {
		return
	}
	a.AcqFD, a.AcqPoint = ss.readyAcq.FD, ss.readyAcq.Point
	a.RelFD, a.RelPoint = ss.readyRel.FD, ss.readyRel.Point
	ss.readyAcq = syncobj.Fence{}
	ss.readyRel = syncobj.Fence{}
}

func releaseActorSync(a *engine.Actor) {
	if a == nil {
		return
	}
	if a.RelFD > 0 {
		_ = syncobj.SignalFence(syncobj.Fence{FD: a.RelFD, Point: a.RelPoint})
		f := syncobj.Fence{FD: a.RelFD}
		f.CloseFD()
	}
	if a.AcqFD > 0 {
		f := syncobj.Fence{FD: a.AcqFD}
		f.CloseFD()
	}
	a.AcqFD, a.AcqPoint, a.RelFD, a.RelPoint = 0, 0, 0, 0
}
