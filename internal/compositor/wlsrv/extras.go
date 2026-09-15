package wlsrv

import (
	"fmt"

	"github.com/codemodify/worldr/internal/wayland"
)

const (
	globalCursorShape     uint32 = 11
	globalActivation      uint32 = 12
	globalPrimary         uint32 = 13
	globalXwayland        uint32 = 14
	globalFractionalScale uint32 = 15
	globalToplevelIcon    uint32 = 16
	globalSyncobj         uint32 = 17
)

// wp_cursor_shape_v1 shapes (enum starts at 1).
const (
	cursorShapeDefault = 1
	cursorShapeText    = 9
	cursorShapePointer = 4
)

func (c *Client) reqPointer(o *object, op uint16, cur *wayland.Cursor) error {
	switch op {
	case 0: // set_cursor(serial, surface, hx, hy)
		_, _ = cur.U32()
		sid, _ := cur.U32()
		hx, _ := cur.I32()
		hy, _ := cur.I32()
		c.srv.setCursorFromSurface(c, sid, int(hx), int(hy))
	case 1: // release
		delete(c.objs, o.id)
		if c.ptrID == o.id {
			c.ptrID = 0
			c.entered = 0
			c.ptrBtns.takeAll()
		}
	}
	return nil
}

func (c *Client) reqCursorShapeMgr(_ *object, op uint16, cur *wayland.Cursor) error {
	if op != 1 { // get_pointer
		return nil
	}
	id, err := cur.U32()
	if err != nil {
		return err
	}
	_, _ = cur.U32() // wl_pointer
	c.objs[id] = &object{id: id, kind: kindCursorShape}
	return nil
}

func (c *Client) reqCursorShape(_ *object, op uint16, cur *wayland.Cursor) error {
	if op != 1 { // set_shape(serial, shape)
		return nil
	}
	_, _ = cur.U32()
	shape, _ := cur.U32()
	c.srv.setCursorShape(shape)
	return nil
}

func (c *Client) reqActivation(_ *object, op uint16, cur *wayland.Cursor) error {
	switch op {
	case 0: // destroy
		return nil
	case 1: // get_activation_token
		id, err := cur.U32()
		if err != nil {
			return err
		}
		c.objs[id] = &object{id: id, kind: kindActToken}
	case 2: // activate(token, surface)
		_, _ = cur.String()
		sid, _ := cur.U32()
		if so := c.objs[sid]; so != nil && so.surf != nil && so.surf.actor != nil {
			c.srv.Scene.FocusAt(so.surf.actor.X+1, so.surf.actor.Y+1, 26, 6)
		}
	}
	return nil
}

func (c *Client) reqActToken(o *object, op uint16, cur *wayland.Cursor) error {
	switch op {
	case 0:
		delete(c.objs, o.id)
	case 1, 2, 3: // set_serial / set_app_id / set_surface
		return nil
	case 4: // commit → done(token)
		tok := fmt.Sprintf("worldr-%d", c.nextSerial())
		return c.send(o.id, 0, wayland.PutString(nil, tok), nil)
	}
	return nil
}

func (c *Client) reqXwaylandShell(_ *object, op uint16, cur *wayland.Cursor) error {
	if op != 1 { // get_xwayland_surface
		return nil
	}
	id, err := cur.U32()
	if err != nil {
		return err
	}
	sid, err := cur.U32()
	if err != nil {
		return err
	}
	var surf *surface
	if so := c.objs[sid]; so != nil {
		surf = so.surf
	}
	if surf != nil {
		surf.xwayland = true
	}
	c.objs[id] = &object{id: id, kind: kindXwSurface, surf: surf}
	return nil
}

func (c *Client) reqXwaylandSurface(o *object, op uint16, cur *wayland.Cursor) error {
	// Protocol XML order has varied: some scanners emit set_serial as
	// opcode 0, others destroy=0 / set_serial=1. Two uint32s → serial.
	lo, err1 := cur.U32()
	hi, err2 := cur.U32()
	if err1 == nil && err2 == nil {
		_, _ = lo, hi
		if o.surf != nil {
			o.surf.xwayland = true
			if o.surf.attached != nil {
				c.mapSurface(o.surf)
			}
		}
		return nil
	}
	delete(c.objs, o.id)
	return nil
}

func (c *Client) reqFracScaleMgr(_ *object, op uint16, cur *wayland.Cursor) error {
	switch op {
	case 0: // destroy
		return nil
	case 1: // get_fractional_scale(id, surface)
		id, err := cur.U32()
		if err != nil {
			return err
		}
		sid, _ := cur.U32()
		var surf *surface
		if so := c.objs[sid]; so != nil {
			surf = so.surf
		}
		c.objs[id] = &object{id: id, kind: kindFracScale, surf: surf}
		if surf != nil {
			surf.fracID = id
		}
		return c.send(id, 0, wayland.PutU32(nil, c.srv.PreferredScale120ths()), nil)
	}
	return nil
}

func (c *Client) reqFracScale(o *object, op uint16, _ *wayland.Cursor) error {
	if op == 0 {
		if o.surf != nil && o.surf.fracID == o.id {
			o.surf.fracID = 0
		}
		delete(c.objs, o.id)
	}
	return nil
}
