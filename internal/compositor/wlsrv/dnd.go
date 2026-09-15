package wlsrv

import (
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/wayland"
)

// dragSession is one wl_data_device.start_drag.
type dragSession struct {
	src    *Client
	source *dataSource
	dest   *Client
	offer  *dataOffer
	surf   uint32
	x, y   int
}

func (s *Server) startDrag(from *Client, src *dataSource) {
	if s == nil || from == nil || src == nil {
		return
	}
	s.mu.Lock()
	old := s.drag
	s.drag = &dragSession{src: from, source: src}
	s.mu.Unlock()
	if old != nil {
		old.cancel("replaced")
	}
}

func (s *Server) cancelDragFromClient(c *Client) {
	if s == nil || c == nil {
		return
	}
	s.mu.Lock()
	d := s.drag
	if d == nil || d.src != c {
		s.mu.Unlock()
		return
	}
	s.drag = nil
	s.mu.Unlock()
	d.cancel("client gone")
}

func (s *Server) cancelDragIfSource(src *dataSource) {
	if s == nil || src == nil {
		return
	}
	s.mu.Lock()
	d := s.drag
	if d == nil || d.source != src {
		s.mu.Unlock()
		return
	}
	s.drag = nil
	s.mu.Unlock()
	d.cancel("source destroyed")
}

func (d *dragSession) cancel(_ string) {
	if d == nil {
		return
	}
	d.leaveDest()
	if d.source != nil && d.source.client != nil {
		_ = d.source.client.send(d.source.id, d.source.cancelledOp(), nil, nil)
	}
}

func (d *dragSession) leaveDest() {
	if d == nil || d.dest == nil || d.dest.dataDev == 0 || d.surf == 0 {
		d.dest, d.offer, d.surf = nil, nil, 0
		return
	}
	_ = d.dest.send(d.dest.dataDev, wlDataDevLeave, nil, nil)
	d.dest, d.offer, d.surf = nil, nil, 0
}

func (s *Server) dragMotion(sx, sy int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	d := s.drag
	s.mu.Unlock()
	if d == nil {
		return
	}
	dest, surf := s.clientSurfaceAt(sx, sy)
	if dest == nil || surf == nil || dest.dataDev == 0 {
		if d.surf != 0 {
			d.leaveDest()
		}
		d.x, d.y = sx, sy
		return
	}
	lx := sx - surf.actor.X
	ly := sy - surf.actor.Y
	if ox, oy := surf.inputOffset(); ox != 0 || oy != 0 {
		lx += ox
		ly += oy
	}
	if d.dest != dest || d.surf != surf.id {
		d.leaveDest()
		d.enterDest(dest, surf, lx, ly)
	} else {
		p := wayland.PutU32(nil, uint32(time.Now().UnixMilli()))
		p = wayland.PutI32(p, int32(lx*256))
		p = wayland.PutI32(p, int32(ly*256))
		_ = dest.send(dest.dataDev, wlDataDevMotion, p, nil)
	}
	d.x, d.y = sx, sy
}

func (d *dragSession) enterDest(dest *Client, surf *surface, lx, ly int) {
	if d == nil || dest == nil || dest.dataDev == 0 || surf == nil || d.source == nil {
		return
	}
	oid := dest.allocServerID()
	off := &dataOffer{id: oid, source: d.source, prim: false}
	dest.objs[oid] = &object{id: oid, kind: kindDataOffer, offer: off}
	_ = dest.send(dest.dataDev, wlDataDevDataOffer, wayland.PutU32(nil, oid), nil)
	for _, m := range d.source.mimes {
		_ = dest.send(oid, wlDataOfferOffer, wayland.PutString(nil, m), nil)
	}
	_ = dest.send(oid, wlDataOfferSrcActs, wayland.PutU32(nil, dndActionCopy), nil)
	_ = dest.send(oid, wlDataOfferAction, wayland.PutU32(nil, dndActionCopy), nil)
	p := wayland.PutU32(nil, dest.nextSerial())
	p = wayland.PutU32(p, surf.id)
	p = wayland.PutI32(p, int32(lx*256))
	p = wayland.PutI32(p, int32(ly*256))
	p = wayland.PutU32(p, oid)
	_ = dest.send(dest.dataDev, wlDataDevEnter, p, nil)
	d.dest = dest
	d.offer = off
	d.surf = surf.id
}

func (s *Server) dragButtonUp(sx, sy int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	d := s.drag
	s.mu.Unlock()
	if d == nil {
		return
	}
	s.dragMotion(sx, sy)
	s.mu.Lock()
	d = s.drag
	s.mu.Unlock()
	if d == nil {
		return
	}
	if d.dest == nil || d.dest.dataDev == 0 || d.surf == 0 {
		// No client under the pointer — no desktop icon canvas yet.
		s.mu.Lock()
		s.drag = nil
		s.mu.Unlock()
		d.cancel("desktop follow-up")
		return
	}
	_ = d.dest.send(d.dest.dataDev, wlDataDevDrop, nil, nil)
	if d.source != nil && d.source.client != nil {
		_ = d.source.client.send(d.source.id, wlDataSrcDndDrop, nil, nil)
		_ = d.source.client.send(d.source.id, wlDataSrcAction, wayland.PutU32(nil, dndActionCopy), nil)
	}
}

func (s *Server) finishDrag(off *dataOffer) {
	if s == nil || off == nil {
		return
	}
	s.mu.Lock()
	d := s.drag
	if d == nil || d.offer != off {
		s.mu.Unlock()
		return
	}
	s.drag = nil
	s.mu.Unlock()
	if d.source != nil && d.source.client != nil {
		_ = d.source.client.send(d.source.id, wlDataSrcDndDone, nil, nil)
	}
	d.leaveDest()
}

func (s *Server) clientSurfaceAt(sx, sy int) (*Client, *surface) {
	if s == nil || s.Scene == nil {
		return nil, nil
	}
	ws := s.Scene.ActiveWorkspace()
	actors := s.Scene.Actors()
	var hit *engine.Actor
	for i := len(actors) - 1; i >= 0; i-- {
		a := actors[i]
		if a == nil || a.Workspace != ws {
			continue
		}
		if sx < a.X || sy < a.Y || sx >= a.X+a.Width || sy >= a.Y+a.Height {
			continue
		}
		hit = a
		break
	}
	if hit == nil {
		return nil, nil
	}
	s.mu.Lock()
	cl := append([]*Client(nil), s.clients...)
	s.mu.Unlock()
	for _, c := range cl {
		for _, o := range c.objs {
			if o != nil && o.surf != nil && o.surf.actor == hit {
				return c, o.surf
			}
		}
	}
	return nil, nil
}
