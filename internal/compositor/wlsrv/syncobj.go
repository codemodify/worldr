package wlsrv

import (
	"syscall"
	"time"

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
	// Best-effort DRM timeline wait. Failure → implicit sync (Intel).
	// TODO: Vulkan VK_KHR_external_semaphore + timeline wait on vk-display.
	if err := syncobj.WaitFence(f, 100*time.Millisecond); err != nil && c.srv != nil && c.srv.log != nil {
		c.srv.log.Printf("drm-syncobj acquire wait fallback (implicit sync): %v", err)
	}
	f.CloseFD()
}

func (c *Client) applySyncobjRelease(s *surface) {
	if s == nil || s.sync == nil || !s.sync.pendingRel.Valid() {
		return
	}
	f := s.sync.pendingRel
	s.sync.pendingRel = syncobj.Fence{}
	// Signal immediately after import. GPU work may still be in flight —
	// TODO: signal after vkCmdBlit / scanout completion.
	if err := syncobj.SignalFence(f); err != nil && c.srv != nil && c.srv.log != nil {
		c.srv.log.Printf("drm-syncobj release signal skipped: %v", err)
	}
	f.CloseFD()
}
