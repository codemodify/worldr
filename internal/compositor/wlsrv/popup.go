package wlsrv

import "github.com/codemodify/worldr/internal/wayland"

const (
	xdgPopupConfigure    uint16 = 0
	xdgPopupDone         uint16 = 1
	xdgPopupRepositioned uint16 = 2
)

// positioner is a snapshot-able xdg_positioner (size + anchor + offset).
// Constraint / gravity / reactive edges are ignored — enough for menus.
type positioner struct {
	id        uint32
	w, h      int32
	ax, ay    int32
	aw, ah    int32
	ox, oy    int32
	hasAnchor bool
}

func (p *positioner) place() (x, y, w, h int32) {
	if p == nil {
		return 0, 0, 1, 1
	}
	x, y = p.ox, p.oy
	if p.hasAnchor {
		x += p.ax
		y += p.ay
	}
	w, h = p.w, p.h
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return x, y, w, h
}

func (p *positioner) snapshot() positioner {
	if p == nil {
		return positioner{}
	}
	out := *p
	out.id = 0
	return out
}

type xdgPopup struct {
	id      uint32
	xdg     *xdgSurface
	parent  *xdgSurface
	pos     positioner
	x, y    int32
	w, h    int32
	grabbed bool
	done    bool
}

func (s *surface) skipKeyboard() bool {
	if s == nil {
		return false
	}
	if s.sub != nil {
		return true
	}
	return s.xdg != nil && s.xdg.pop != nil
}

func (c *Client) isChildSurface(s *surface) bool {
	if s == nil {
		return false
	}
	if s.sub != nil {
		return true
	}
	return s.xdg != nil && s.xdg.pop != nil
}

func (c *Client) reqPositioner(o *object, op uint16, cur *wayland.Cursor) error {
	p := o.pos
	if p == nil {
		p = &positioner{id: o.id}
		o.pos = p
	}
	switch op {
	case 0: // destroy
		delete(c.objs, o.id)
	case 1: // set_size
		w, _ := cur.I32()
		h, _ := cur.I32()
		p.w, p.h = w, h
	case 2: // set_anchor_rect
		x, _ := cur.I32()
		y, _ := cur.I32()
		w, _ := cur.I32()
		h, _ := cur.I32()
		p.ax, p.ay, p.aw, p.ah = x, y, w, h
		p.hasAnchor = true
	case 6: // set_offset
		x, _ := cur.I32()
		y, _ := cur.I32()
		p.ox, p.oy = x, y
	}
	return nil
}

func (c *Client) getXdgPopup(xs *xdgSurface, cur *wayland.Cursor) error {
	id, err := cur.U32()
	if err != nil {
		return err
	}
	parentID, err := cur.U32()
	if err != nil {
		return err
	}
	posID, err := cur.U32()
	if err != nil {
		return err
	}
	pop := &xdgPopup{id: id, xdg: xs}
	if parentID != 0 {
		if po := c.objs[parentID]; po != nil {
			pop.parent = po.xdgS
		}
	}
	if posID != 0 {
		if po := c.objs[posID]; po != nil && po.pos != nil {
			pop.pos = po.pos.snapshot()
		}
	}
	xs.pop = pop
	c.objs[id] = &object{id: id, kind: kindXdgPopup, xdgP: pop}
	return c.configurePopup(pop)
}

func (c *Client) configurePopup(pop *xdgPopup) error {
	if pop == nil || pop.xdg == nil {
		return nil
	}
	pop.x, pop.y, pop.w, pop.h = pop.pos.place()
	p := wayland.PutI32(nil, pop.x)
	p = wayland.PutI32(p, pop.y)
	p = wayland.PutI32(p, pop.w)
	p = wayland.PutI32(p, pop.h)
	if err := c.send(pop.id, xdgPopupConfigure, p, nil); err != nil {
		return err
	}
	pop.xdg.serial = c.nextSerial()
	return c.send(pop.xdg.id, 0, wayland.PutU32(nil, pop.xdg.serial), nil)
}

func (c *Client) reqXdgPopup(o *object, op uint16, cur *wayland.Cursor) error {
	pop := o.xdgP
	if pop == nil {
		return nil
	}
	switch op {
	case 0: // destroy
		c.unmapPopup(pop)
		delete(c.objs, o.id)
	case 1: // grab(seat, serial)
		_, _ = cur.U32()
		_, _ = cur.U32()
		pop.grabbed = true
	case 2: // reposition(positioner, token)
		posID, _ := cur.U32()
		token, _ := cur.U32()
		if posID != 0 {
			if po := c.objs[posID]; po != nil && po.pos != nil {
				pop.pos = po.pos.snapshot()
			}
		}
		if err := c.send(pop.id, xdgPopupRepositioned, wayland.PutU32(nil, token), nil); err != nil {
			return err
		}
		return c.configurePopup(pop)
	}
	return nil
}

func (c *Client) popupDone(pop *xdgPopup) {
	if pop == nil || pop.done {
		return
	}
	pop.done = true
	pop.grabbed = false
	_ = c.send(pop.id, xdgPopupDone, nil, nil)
	c.unmapPopup(pop)
}

func (c *Client) unmapPopup(pop *xdgPopup) {
	if pop == nil || pop.xdg == nil || pop.xdg.surf == nil {
		return
	}
	s := pop.xdg.surf
	if c.entered == s.id {
		c.pointerLeaveCurrent()
	}
	if s.actor != nil && c.srv != nil && c.srv.Scene != nil {
		c.srv.Scene.Remove(s.actor)
		s.actor = nil
	}
}

func (c *Client) dismissXdg(xs *xdgSurface) {
	if xs == nil {
		return
	}
	if xs.pop != nil {
		c.popupDone(xs.pop)
	}
	c.dismissChildPopups(xs)
}

func (c *Client) dismissChildPopups(parent *xdgSurface) {
	if parent == nil {
		return
	}
	for _, o := range c.objs {
		if o.xdgP != nil && o.xdgP.parent == parent && !o.xdgP.done {
			c.popupDone(o.xdgP)
		}
	}
}

func (c *Client) dismissPopupsIfOutside(sx, sy int) {
	hit := c.surfaceAt(sx, sy)
	for _, o := range c.objs {
		pop := o.xdgP
		if pop == nil || !pop.grabbed || pop.done {
			continue
		}
		if hit != nil && c.popupContains(pop, hit) {
			continue
		}
		c.popupDone(pop)
	}
}

func (c *Client) popupContains(pop *xdgPopup, s *surface) bool {
	if pop == nil || s == nil || pop.xdg == nil {
		return false
	}
	if s == pop.xdg.surf {
		return true
	}
	if s.sub != nil && s.sub.parent == pop.xdg.surf {
		return true
	}
	return false
}
