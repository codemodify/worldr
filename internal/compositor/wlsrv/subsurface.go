package wlsrv

import (
	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/wayland"
)

type subsurface struct {
	id     uint32
	surf   *surface
	parent *surface
	dx, dy int32
	sync   bool
	cached *object
}

func (c *Client) reqSubcomp(_ *object, op uint16, cur *wayland.Cursor) error {
	switch op {
	case 0: // destroy
		return nil
	case 1: // get_subsurface(id, surface, parent)
		id, err := cur.U32()
		if err != nil {
			return err
		}
		sid, err := cur.U32()
		if err != nil {
			return err
		}
		pid, err := cur.U32()
		if err != nil {
			return err
		}
		var child, parent *surface
		if so := c.objs[sid]; so != nil {
			child = so.surf
		}
		if po := c.objs[pid]; po != nil {
			parent = po.surf
		}
		sub := &subsurface{id: id, surf: child, parent: parent, sync: true}
		if child != nil {
			child.sub = sub
		}
		c.objs[id] = &object{id: id, kind: kindSubsurface, sub: sub, surf: child}
	}
	return nil
}

func (c *Client) reqSubsurface(o *object, op uint16, cur *wayland.Cursor) error {
	sub := o.sub
	if sub == nil {
		return nil
	}
	switch op {
	case 0: // destroy
		if sub.surf != nil {
			if c.entered == sub.surf.id {
				c.pointerLeaveCurrent()
			}
			if sub.surf.actor != nil {
				c.srv.Scene.Remove(sub.surf.actor)
				sub.surf.actor = nil
			}
			sub.surf.sub = nil
		}
		delete(c.objs, o.id)
	case 1: // set_position
		x, _ := cur.I32()
		y, _ := cur.I32()
		sub.dx, sub.dy = x, y
		if sub.surf != nil && sub.surf.actor != nil {
			c.placeChild(sub.surf)
		}
	case 4: // set_sync
		sub.sync = true
	case 5: // set_desync
		sub.sync = false
	}
	return nil
}

func (c *Client) applyChildSubs(parent *surface) {
	if parent == nil {
		return
	}
	for _, o := range c.objs {
		child := o.surf
		if child == nil || child.sub == nil || child.sub.parent != parent {
			continue
		}
		if child.sub.sync && child.sub.cached != nil {
			child.attached = child.sub.cached
		}
		if child.attached != nil && c.surfaceReady(child) {
			c.mapSurface(child)
		}
	}
}

func (c *Client) unmapSubsOf(parent *surface) {
	if parent == nil {
		return
	}
	for _, o := range c.objs {
		if o.surf != nil && o.surf.sub != nil && o.surf.sub.parent == parent {
			if o.surf.actor != nil {
				c.srv.Scene.Remove(o.surf.actor)
				o.surf.actor = nil
			}
		}
	}
}

func (c *Client) placeChild(s *surface) {
	if s == nil || s.actor == nil {
		return
	}
	parent, relX, relY := c.childOrigin(s)
	if parent != nil {
		s.actor.Owner = parent
		s.actor.X = parent.X + relX
		s.actor.Y = parent.Y + relY
		s.actor.Workspace = parent.Workspace
	}
	s.actor.NoChrome = true
}

func (c *Client) childOrigin(s *surface) (parent *engine.Actor, relX, relY int) {
	if s == nil {
		return nil, 0, 0
	}
	if s.sub != nil && s.sub.parent != nil && s.sub.parent.actor != nil {
		return s.sub.parent.actor, int(s.sub.dx), int(s.sub.dy)
	}
	if s.xdg != nil && s.xdg.pop != nil {
		pop := s.xdg.pop
		relX, relY = int(pop.x), int(pop.y)
		if pop.parent != nil && pop.parent.hasGeo {
			relX += int(pop.parent.geoX)
			relY += int(pop.parent.geoY)
		}
		if pop.parent != nil && pop.parent.surf != nil && pop.parent.surf.actor != nil {
			return pop.parent.surf.actor, relX, relY
		}
	}
	return nil, relX, relY
}
