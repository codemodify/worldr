package wlsrv

import (
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/wayland"
)

// Linux evdev BTN_LEFT — the only button the shell currently forwards.
const btnLeft uint32 = 0x110

const (
	wlPointerEnter  uint16 = 0
	wlPointerLeave  uint16 = 1
	wlPointerMotion uint16 = 2
	wlPointerButton uint16 = 3
	wlPointerFrame  uint16 = 5
)

const (
	wlPointerReleased uint32 = 0
	wlPointerPressed  uint32 = 1
)

// pointerButtons is the set of wl_pointer.button codes this client has
// received as pressed and not yet released. Releases are sent only for
// buttons in this set, so clients like foot never see a stray release.
type pointerButtons struct {
	down map[uint32]struct{}
}

func (p *pointerButtons) press(btn uint32) bool {
	if p.down == nil {
		p.down = make(map[uint32]struct{})
	}
	if _, ok := p.down[btn]; ok {
		return false
	}
	p.down[btn] = struct{}{}
	return true
}

func (p *pointerButtons) release(btn uint32) bool {
	if p == nil || p.down == nil {
		return false
	}
	if _, ok := p.down[btn]; !ok {
		return false
	}
	delete(p.down, btn)
	return true
}

func (p *pointerButtons) anyDown() bool {
	return p != nil && len(p.down) > 0
}

func (p *pointerButtons) takeAll() []uint32 {
	if p == nil || len(p.down) == 0 {
		return nil
	}
	out := make([]uint32, 0, len(p.down))
	for b := range p.down {
		out = append(out, b)
	}
	p.down = nil
	return out
}

func (c *Client) pointerFrame() {
	if c.ptrID == 0 {
		return
	}
	_ = c.send(c.ptrID, wlPointerFrame, nil, nil)
}

func (c *Client) sendPointerButton(btn uint32, pressed bool) {
	if c.ptrID == 0 {
		return
	}
	state := wlPointerReleased
	if pressed {
		state = wlPointerPressed
	}
	p := wayland.PutU32(nil, c.nextSerial())
	p = wayland.PutU32(p, uint32(time.Now().UnixMilli()))
	p = wayland.PutU32(p, btn)
	p = wayland.PutU32(p, state)
	_ = c.send(c.ptrID, wlPointerButton, p, nil)
	c.pointerFrame()
}

// pointerLeaveCurrent sends matching releases for any buttons still down
// (Mutter-style) then wl_pointer.leave. Implicit grab normally avoids this
// path; surface destroy and focus steal still must not leave a button down.
func (c *Client) pointerLeaveCurrent() {
	if c.entered == 0 {
		c.ptrBtns.takeAll()
		return
	}
	for _, btn := range c.ptrBtns.takeAll() {
		c.sendPointerButton(btn, false)
	}
	if c.ptrID != 0 {
		p := wayland.PutU32(nil, c.nextSerial())
		p = wayland.PutU32(p, c.entered)
		_ = c.send(c.ptrID, wlPointerLeave, p, nil)
		c.pointerFrame()
	}
	c.entered = 0
}

func (c *Client) grabbedSurface() *surface {
	if !c.ptrBtns.anyDown() || c.entered == 0 {
		return nil
	}
	o := c.objs[c.entered]
	if o == nil || o.surf == nil || o.surf.actor == nil {
		return nil
	}
	return o.surf
}

func (c *Client) ownsFocusedSurface() bool {
	ws := 0
	if c.srv != nil && c.srv.Scene != nil {
		ws = c.srv.Scene.ActiveWorkspace()
	}
	for _, o := range c.objs {
		if o.surf != nil && o.surf.actor != nil && o.surf.actor.Focused && o.surf.actor.Workspace == ws {
			return true
		}
	}
	return false
}

func (c *Client) surfaceAt(sx, sy int) *surface {
	if c.srv == nil || c.srv.Scene == nil {
		return c.focusedSurface()
	}
	ws := c.srv.Scene.ActiveWorkspace()
	actors := c.srv.Scene.Actors()
	z := make(map[*engine.Actor]int, len(actors))
	for i, a := range actors {
		z[a] = i
	}
	var best *surface
	bestZ := -1
	for _, o := range c.objs {
		s := o.surf
		if s == nil || s.actor == nil || s.actor.Workspace != ws {
			continue
		}
		a := s.actor
		if sx < a.X || sy < a.Y || sx >= a.X+a.Width || sy >= a.Y+a.Height {
			continue
		}
		if zi, ok := z[a]; ok && zi >= bestZ {
			best = s
			bestZ = zi
		}
	}
	return best
}

func (s *surface) inputOffset() (x, y int) {
	if s == nil || !s.cropped || s.xdg == nil || !s.xdg.hasGeo {
		return 0, 0
	}
	return int(s.xdg.geoX), int(s.xdg.geoY)
}

func (c *Client) pointerMotion(sx, sy int) {
	c.ptrX, c.ptrY = sx, sy
	if c.ptrID == 0 {
		return
	}
	s := c.grabbedSurface()
	if s == nil {
		s = c.surfaceAt(sx, sy)
	}
	if s == nil || s.actor == nil {
		if c.entered != 0 {
			c.pointerLeaveCurrent()
		}
		return
	}
	lx := sx - s.actor.X
	ly := sy - s.actor.Y
	if ox, oy := s.inputOffset(); ox != 0 || oy != 0 {
		lx += ox
		ly += oy
	}
	if c.entered != s.id {
		if c.entered != 0 {
			c.pointerLeaveCurrent()
		}
		p := wayland.PutU32(nil, c.nextSerial())
		p = wayland.PutU32(p, s.id)
		p = wayland.PutI32(p, int32(lx*256)) // wl_fixed
		p = wayland.PutI32(p, int32(ly*256))
		_ = c.send(c.ptrID, wlPointerEnter, p, nil)
		if !s.skipKeyboard() && c.kbdSurf != s.id {
			if c.kbdSurf != 0 {
				c.keyboardLeave(c.kbdSurf)
			}
			c.keyboardEnter(s)
			c.kbdSurf = s.id
		}
		c.entered = s.id
		c.pointerFrame()
	}
	p := wayland.PutU32(nil, uint32(time.Now().UnixMilli()))
	p = wayland.PutI32(p, int32(lx*256))
	p = wayland.PutI32(p, int32(ly*256))
	_ = c.send(c.ptrID, wlPointerMotion, p, nil)
	c.pointerFrame()
}

func (c *Client) pointerButton(sx, sy int, pressed bool) {
	c.pointerMotion(sx, sy)
	if pressed {
		c.dismissPopupsIfOutside(sx, sy)
		c.pointerMotion(sx, sy)
	}
	if c.ptrID == 0 {
		return
	}
	if pressed {
		if c.entered == 0 {
			return
		}
		// New press: only the focused client (implicit grab keeps later ones).
		if !c.ptrBtns.anyDown() && !c.ownsFocusedSurface() {
			return
		}
		if !c.ptrBtns.press(btnLeft) {
			return
		}
		c.sendPointerButton(btnLeft, true)
		return
	}
	if !c.ptrBtns.release(btnLeft) {
		return
	}
	c.sendPointerButton(btnLeft, false)
}
