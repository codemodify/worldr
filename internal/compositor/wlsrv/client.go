package wlsrv

import (
	"io"
	"net"
	"syscall"
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/wayland"
)

const (
	globalCompositor uint32 = 1
	globalShm        uint32 = 2
	globalXdgWm      uint32 = 3
	globalSeat       uint32 = 4
	globalOutput     uint32 = 5
)

type objectKind int

const (
	kindDisplay objectKind = iota
	kindRegistry
	kindCallback
	kindCompositor
	kindShm
	kindShmPool
	kindBuffer
	kindSurface
	kindRegion
	kindXdgWm
	kindXdgSurface
	kindXdgToplevel
	kindSeat
	kindPointer
	kindKeyboard
	kindOutput
	kindDataDeviceManager
	kindDataDevice
	kindPositioner
)

type object struct {
	id   uint32
	kind objectKind
	pool *shmPool
	buf  *shmBuffer
	surf *surface
	xdgS *xdgSurface
	xdgT *xdgToplevel
}

type shmPool struct {
	fd   int
	size int
	mem  []byte
}

type shmBuffer struct {
	pool   *shmPool
	offset int
	w, h   int
	stride int
	format uint32
}

type surface struct {
	id       uint32
	pending  *shmBuffer
	attached *shmBuffer
	actor    *engine.Actor
	xdg      *xdgSurface
	sx, sy   int32
}

type xdgSurface struct {
	id      uint32
	surf    *surface
	top     *xdgToplevel
	serial  uint32
	acked   uint32
	pending bool
}

type xdgToplevel struct {
	id    uint32
	xdg   *xdgSurface
	title string
	app   string
}

// Client is one Wayland connection.
type Client struct {
	srv     *Server
	conn    *net.UnixConn
	rd      *wayland.Reader
	wr      *wayland.Writer
	objs    map[uint32]*object
	serial  uint32
	ptrID   uint32
	kbdID   uint32
	ptrX    int
	ptrY    int
	entered uint32
}

func newClient(s *Server, conn *net.UnixConn) *Client {
	_ = conn.SetReadDeadline(time.Time{})
	c := &Client{
		srv:  s,
		conn: conn,
		rd:   wayland.NewReader(conn),
		wr:   wayland.NewWriter(conn),
		objs: map[uint32]*object{1: {id: 1, kind: kindDisplay}},
	}
	f, err := conn.File()
	if err == nil {
		_ = syscall.SetNonblock(int(f.Fd()), true)
		_ = f.Close()
	}
	return c
}

func (c *Client) close() {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	for _, o := range c.objs {
		if o.surf != nil && o.surf.actor != nil {
			c.srv.Scene.Remove(o.surf.actor)
		}
		if o.pool != nil && o.pool.fd > 0 {
			_ = syscall.Close(o.pool.fd)
			o.pool.fd = -1
		}
	}
	c.srv.dropClient(c)
}

func (c *Client) nextSerial() uint32 {
	c.serial++
	if c.serial == 0 {
		c.serial = 1
	}
	return c.serial
}

func (c *Client) dispatchAvailable() {
	for {
		if c.conn == nil {
			return
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(200 * time.Microsecond))
		msg, err := c.rd.Next()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return
			}
			if err == io.EOF {
				c.close()
				return
			}
			// EAGAIN after nonblock
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				return
			}
			c.srv.log.Printf("read: %v", err)
			c.close()
			return
		}
		if err := c.dispatch(msg); err != nil {
			c.srv.log.Printf("dispatch: %v", err)
			c.close()
			return
		}
	}
}

func (c *Client) dispatch(msg wayland.Message) error {
	o := c.objs[msg.Object]
	if o == nil {
		return nil
	}
	cur := wayland.NewCursor(msg.Payload, msg.FDs)
	switch o.kind {
	case kindDisplay:
		return c.reqDisplay(o, msg.Opcode, cur)
	case kindRegistry:
		return c.reqRegistry(o, msg.Opcode, cur)
	case kindCompositor:
		return c.reqCompositor(o, msg.Opcode, cur)
	case kindShm:
		return c.reqShm(o, msg.Opcode, cur)
	case kindShmPool:
		return c.reqShmPool(o, msg.Opcode, cur)
	case kindBuffer:
		if msg.Opcode == 0 {
			delete(c.objs, o.id)
		}
		return nil
	case kindSurface:
		return c.reqSurface(o, msg.Opcode, cur)
	case kindRegion:
		if msg.Opcode == 0 {
			delete(c.objs, o.id)
		}
		return nil
	case kindXdgWm:
		return c.reqXdgWm(o, msg.Opcode, cur)
	case kindXdgSurface:
		return c.reqXdgSurface(o, msg.Opcode, cur)
	case kindXdgToplevel:
		return c.reqXdgToplevel(o, msg.Opcode, cur)
	case kindSeat:
		return c.reqSeat(o, msg.Opcode, cur)
	case kindPointer, kindKeyboard, kindOutput, kindDataDeviceManager, kindDataDevice, kindPositioner, kindCallback:
		return nil
	default:
		return nil
	}
}

func (c *Client) reqDisplay(o *object, op uint16, cur *wayland.Cursor) error {
	switch op {
	case 0: // sync
		id, err := cur.U32()
		if err != nil {
			return err
		}
		c.objs[id] = &object{id: id, kind: kindCallback}
		return c.send(id, 0, wayland.PutU32(nil, c.nextSerial()), nil) // done
	case 1: // get_registry
		id, err := cur.U32()
		if err != nil {
			return err
		}
		c.objs[id] = &object{id: id, kind: kindRegistry}
		return c.advertise(id)
	}
	return nil
}

func (c *Client) advertise(reg uint32) error {
	type g struct {
		name  uint32
		iface string
		ver   uint32
	}
	globals := []g{
		{globalCompositor, "wl_compositor", 4},
		{globalShm, "wl_shm", 1},
		{globalXdgWm, "xdg_wm_base", 3},
		{globalSeat, "wl_seat", 7},
		{globalOutput, "wl_output", 3},
	}
	for _, gl := range globals {
		p := wayland.PutU32(nil, gl.name)
		p = wayland.PutString(p, gl.iface)
		p = wayland.PutU32(p, gl.ver)
		if err := c.send(reg, 0, p, nil); err != nil { // global
			return err
		}
	}
	return nil
}

func (c *Client) reqRegistry(_ *object, op uint16, cur *wayland.Cursor) error {
	if op != 0 { // bind
		return nil
	}
	name, err := cur.U32()
	if err != nil {
		return err
	}
	iface, err := cur.String()
	if err != nil {
		return err
	}
	_, err = cur.U32() // version
	if err != nil {
		return err
	}
	id, err := cur.U32()
	if err != nil {
		return err
	}
	o := &object{id: id}
	switch name {
	case globalCompositor:
		o.kind = kindCompositor
	case globalShm:
		o.kind = kindShm
		c.objs[id] = o
		// formats
		_ = c.send(id, 0, wayland.PutU32(nil, 0), nil) // ARGB8888
		_ = c.send(id, 0, wayland.PutU32(nil, 1), nil) // XRGB8888
		return nil
	case globalXdgWm:
		o.kind = kindXdgWm
	case globalSeat:
		o.kind = kindSeat
		c.objs[id] = o
		_ = c.send(id, 0, wayland.PutU32(nil, 3), nil) // pointer|keyboard
		_ = c.send(id, 1, wayland.PutString(nil, "worldr-seat0"), nil)
		return nil
	case globalOutput:
		o.kind = kindOutput
		c.objs[id] = o
		return c.sendOutput(id)
	default:
		switch iface {
		case "wl_data_device_manager":
			o.kind = kindDataDeviceManager
		default:
			c.srv.log.Printf("bind unknown %s name=%d", iface, name)
		}
	}
	c.objs[id] = o
	return nil
}

func (c *Client) sendOutput(id uint32) error {
	w, h := c.srv.ScreenW, c.srv.ScreenH
	if w == 0 {
		w = 1920
	}
	if h == 0 {
		h = 1080
	}
	// geometry: x y physical_w physical_h subpixel make model transform
	p := wayland.PutI32(nil, 0)
	p = wayland.PutI32(p, 0)
	p = wayland.PutI32(p, int32(w*38/100)) // fake mm
	p = wayland.PutI32(p, int32(h*38/100))
	p = wayland.PutI32(p, 0)
	p = wayland.PutString(p, "worldr")
	p = wayland.PutString(p, "output-0")
	p = wayland.PutI32(p, 0)
	if err := c.send(id, 0, p, nil); err != nil {
		return err
	}
	// mode: flags w h refresh
	p = wayland.PutU32(nil, 3) // current|preferred
	p = wayland.PutI32(p, int32(w))
	p = wayland.PutI32(p, int32(h))
	p = wayland.PutI32(p, 60000)
	if err := c.send(id, 1, p, nil); err != nil {
		return err
	}
	p = wayland.PutI32(nil, 1)
	if err := c.send(id, 3, p, nil); err != nil { // scale
		return err
	}
	return c.send(id, 2, nil, nil) // done
}

func (c *Client) reqCompositor(_ *object, op uint16, cur *wayland.Cursor) error {
	id, err := cur.U32()
	if err != nil {
		return err
	}
	switch op {
	case 0: // create_surface
		s := &surface{id: id}
		c.objs[id] = &object{id: id, kind: kindSurface, surf: s}
	case 1: // create_region
		c.objs[id] = &object{id: id, kind: kindRegion}
	}
	return nil
}

func (c *Client) reqShm(_ *object, op uint16, cur *wayland.Cursor) error {
	if op != 0 {
		return nil
	}
	id, err := cur.U32()
	if err != nil {
		return err
	}
	fd, err := cur.FD()
	if err != nil {
		return err
	}
	sz, err := cur.I32()
	if err != nil {
		_ = syscall.Close(fd)
		return err
	}
	mem, err := syscall.Mmap(fd, 0, int(sz), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		_ = syscall.Close(fd)
		return err
	}
	c.objs[id] = &object{id: id, kind: kindShmPool, pool: &shmPool{fd: fd, size: int(sz), mem: mem}}
	return nil
}

func (c *Client) reqShmPool(o *object, op uint16, cur *wayland.Cursor) error {
	switch op {
	case 0: // create_buffer
		id, err := cur.U32()
		if err != nil {
			return err
		}
		off, _ := cur.I32()
		w, _ := cur.I32()
		h, _ := cur.I32()
		stride, _ := cur.I32()
		format, _ := cur.U32()
		c.objs[id] = &object{id: id, kind: kindBuffer, buf: &shmBuffer{
			pool: o.pool, offset: int(off), w: int(w), h: int(h), stride: int(stride), format: format,
		}}
	case 1: // destroy
		if o.pool != nil {
			if o.pool.mem != nil {
				_ = syscall.Munmap(o.pool.mem)
			}
			if o.pool.fd > 0 {
				_ = syscall.Close(o.pool.fd)
			}
		}
		delete(c.objs, o.id)
	case 2: // resize
		sz, err := cur.I32()
		if err != nil || o.pool == nil {
			return err
		}
		if o.pool.mem != nil {
			_ = syscall.Munmap(o.pool.mem)
		}
		mem, err := syscall.Mmap(o.pool.fd, 0, int(sz), syscall.PROT_READ, syscall.MAP_SHARED)
		if err != nil {
			return err
		}
		o.pool.mem = mem
		o.pool.size = int(sz)
	}
	return nil
}

func (c *Client) reqSurface(o *object, op uint16, cur *wayland.Cursor) error {
	s := o.surf
	if s == nil {
		return nil
	}
	switch op {
	case 0: // destroy
		if s.actor != nil {
			c.srv.Scene.Remove(s.actor)
		}
		delete(c.objs, o.id)
	case 1: // attach
		bufID, _ := cur.U32()
		x, _ := cur.I32()
		y, _ := cur.I32()
		s.sx, s.sy = x, y
		if bufID == 0 {
			s.pending = nil
			break
		}
		if b := c.objs[bufID]; b != nil {
			s.pending = b.buf
		}
	case 3: // frame
		id, err := cur.U32()
		if err != nil {
			return err
		}
		c.objs[id] = &object{id: id, kind: kindCallback}
		// done immediately with a fake time
		_ = c.send(id, 0, wayland.PutU32(nil, uint32(time.Now().UnixMilli())), nil)
		_ = c.send(1, 1, wayland.PutU32(nil, id), nil) // wl_display.delete_id
		delete(c.objs, id)
	case 6: // commit
		c.commit(s)
	}
	return nil
}

func (c *Client) commit(s *surface) {
	if s.pending != nil {
		s.attached = s.pending
	}
	if s.xdg != nil && s.xdg.top != nil && s.attached != nil && s.xdg.acked != 0 {
		c.mapSurface(s)
	}
}

func (c *Client) mapSurface(s *surface) {
	b := s.attached
	if b == nil || b.pool == nil || b.pool.mem == nil {
		return
	}
	need := b.offset + b.stride*b.h
	if need > len(b.pool.mem) {
		return
	}
	pix := make([]byte, b.stride*b.h)
	copy(pix, b.pool.mem[b.offset:need])
	if s.actor == nil {
		s.actor = &engine.Actor{}
		c.srv.Scene.PlaceNew(s.actor, c.srv.ScreenW, c.srv.ScreenH, 6, 26)
		c.srv.Scene.Add(s.actor)
	}
	s.actor.Width = b.w
	s.actor.Height = b.h
	s.actor.Stride = b.stride
	s.actor.Pixels = pix
	if s.xdg != nil && s.xdg.top != nil {
		s.actor.Title = s.xdg.top.title
		s.actor.AppID = s.xdg.top.app
	}
	// release buffer
	for id, o := range c.objs {
		if o.buf == b {
			_ = c.send(id, 0, nil, nil)
			break
		}
	}
}

func (c *Client) reqXdgWm(_ *object, op uint16, cur *wayland.Cursor) error {
	switch op {
	case 0:
		return nil
	case 1: // create_positioner
		id, err := cur.U32()
		if err != nil {
			return err
		}
		c.objs[id] = &object{id: id, kind: kindPositioner}
	case 2: // get_xdg_surface
		id, err := cur.U32()
		if err != nil {
			return err
		}
		sid, err := cur.U32()
		if err != nil {
			return err
		}
		so := c.objs[sid]
		xs := &xdgSurface{id: id}
		if so != nil {
			xs.surf = so.surf
			if so.surf != nil {
				so.surf.xdg = xs
			}
		}
		c.objs[id] = &object{id: id, kind: kindXdgSurface, xdgS: xs}
	case 3: // pong
	}
	return nil
}

func (c *Client) reqXdgSurface(o *object, op uint16, cur *wayland.Cursor) error {
	xs := o.xdgS
	if xs == nil {
		return nil
	}
	switch op {
	case 0:
		delete(c.objs, o.id)
	case 1: // get_toplevel
		id, err := cur.U32()
		if err != nil {
			return err
		}
		t := &xdgToplevel{id: id, xdg: xs}
		xs.top = t
		c.objs[id] = &object{id: id, kind: kindXdgToplevel, xdgT: t}
		return c.configure(xs)
	case 4: // ack_configure
		ser, err := cur.U32()
		if err != nil {
			return err
		}
		xs.acked = ser
	}
	return nil
}

func (c *Client) configure(xs *xdgSurface) error {
	if xs == nil || xs.top == nil {
		return nil
	}
	w, h := 800, 600
	// toplevel.configure w h states
	p := wayland.PutI32(nil, int32(w))
	p = wayland.PutI32(p, int32(h))
	p = wayland.PutArray(p, nil)
	if err := c.send(xs.top.id, 0, p, nil); err != nil {
		return err
	}
	xs.serial = c.nextSerial()
	return c.send(xs.id, 0, wayland.PutU32(nil, xs.serial), nil)
}

func (c *Client) reqXdgToplevel(o *object, op uint16, cur *wayland.Cursor) error {
	t := o.xdgT
	if t == nil {
		return nil
	}
	switch op {
	case 0:
		if t.xdg != nil && t.xdg.surf != nil && t.xdg.surf.actor != nil {
			c.srv.Scene.Remove(t.xdg.surf.actor)
			t.xdg.surf.actor = nil
		}
		delete(c.objs, o.id)
	case 2:
		s, err := cur.String()
		if err == nil {
			t.title = s
			if t.xdg != nil && t.xdg.surf != nil && t.xdg.surf.actor != nil {
				t.xdg.surf.actor.Title = s
			}
		}
	case 3:
		s, err := cur.String()
		if err == nil {
			t.app = s
		}
	}
	return nil
}

func (c *Client) reqSeat(_ *object, op uint16, cur *wayland.Cursor) error {
	id, err := cur.U32()
	if err != nil {
		return nil
	}
	switch op {
	case 0:
		c.ptrID = id
		c.objs[id] = &object{id: id, kind: kindPointer}
	case 1:
		c.kbdID = id
		c.objs[id] = &object{id: id, kind: kindKeyboard}
	}
	return nil
}

func (c *Client) send(object uint32, opcode uint16, payload []byte, fds []int) error {
	if payload == nil {
		payload = []byte{}
	}
	return c.wr.Send(object, opcode, payload, fds)
}

func (c *Client) focusedSurface() *surface {
	for _, o := range c.objs {
		if o.surf != nil && o.surf.actor != nil && o.surf.actor.Focused {
			return o.surf
		}
	}
	// first mapped
	for _, o := range c.objs {
		if o.surf != nil && o.surf.actor != nil {
			return o.surf
		}
	}
	return nil
}

func (c *Client) pointerMotion(sx, sy int) {
	c.ptrX, c.ptrY = sx, sy
	if c.ptrID == 0 {
		return
	}
	s := c.focusedSurface()
	if s == nil || s.actor == nil {
		return
	}
	lx := sx - s.actor.X
	ly := sy - s.actor.Y
	if c.entered != s.id {
		if c.entered != 0 {
			p := wayland.PutU32(nil, c.nextSerial())
			p = wayland.PutU32(p, c.entered)
			_ = c.send(c.ptrID, 1, p, nil) // leave
		}
		p := wayland.PutU32(nil, c.nextSerial())
		p = wayland.PutU32(p, s.id)
		p = wayland.PutI32(p, int32(lx*256)) // wl_fixed
		p = wayland.PutI32(p, int32(ly*256))
		_ = c.send(c.ptrID, 0, p, nil) // enter
		c.entered = s.id
	}
	p := wayland.PutU32(nil, uint32(time.Now().UnixMilli()))
	p = wayland.PutI32(p, int32(lx*256))
	p = wayland.PutI32(p, int32(ly*256))
	_ = c.send(c.ptrID, 2, p, nil) // motion
}

func (c *Client) pointerButton(sx, sy int, pressed bool) {
	c.pointerMotion(sx, sy)
	if c.ptrID == 0 {
		return
	}
	state := uint32(0)
	if pressed {
		state = 1
	}
	p := wayland.PutU32(nil, c.nextSerial())
	p = wayland.PutU32(p, uint32(time.Now().UnixMilli()))
	p = wayland.PutU32(p, 0x110) // BTN_LEFT
	p = wayland.PutU32(p, state)
	_ = c.send(c.ptrID, 3, p, nil)
}
