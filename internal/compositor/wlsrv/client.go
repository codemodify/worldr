package wlsrv

import (
	"io"
	"net"
	"syscall"
	"time"

	"github.com/codemodify/worldr/internal/decorations"
	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/wayland"
)

const (
	globalCompositor uint32 = 1
	globalShm        uint32 = 2
	globalXdgWm      uint32 = 3
	globalSeat       uint32 = 4
	globalOutput     uint32 = 5
	globalDmabuf     uint32 = 6
	globalDeco       uint32 = 7
	globalViewporter uint32 = 8
	globalDataDev    uint32 = 9
	globalSubcomp    uint32 = 10
	// 11–15 are in extras.go (cursor, activation, primary, xwayland_shell, fractional_scale)
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
	kindDmaParams
	kindDmaFeedback
	kindDecoMgr
	kindDeco
	kindViewporter
	kindViewport
	kindSubcomp
	kindSubsurface
	kindLinuxDmabuf
	kindCursorShapeMgr
	kindCursorShape
	kindActivation
	kindActToken
	kindPrimMgr
	kindPrimDevice
	kindPrimSource
	kindXwShell
	kindXwSurface
	kindFracScaleMgr
	kindFracScale
)

type object struct {
	id   uint32
	kind objectKind
	pool *shmPool
	buf  *shmBuffer
	surf *surface
	xdgS *xdgSurface
	xdgT *xdgToplevel
	dma  *dmaBuf
}

type shmPool struct {
	fd   int
	size int
	mem  []byte
	live int  // buffers still using this pool
	gone bool // client destroyed the pool object
}

type shmBuffer struct {
	pool   *shmPool
	offset int
	w, h   int
	stride int
	format uint32
}

func releasePool(p *shmPool) {
	if p == nil || !p.gone || p.live > 0 {
		return
	}
	if p.mem != nil {
		_ = syscall.Munmap(p.mem)
		p.mem = nil
	}
	if p.fd > 0 {
		_ = syscall.Close(p.fd)
		p.fd = -1
	}
}

type surface struct {
	id       uint32
	pending  *object
	attached *object
	actor    *engine.Actor
	xdg      *xdgSurface
	sx, sy   int32
	destW    int
	destH    int
	xwayland bool
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
	srv      *Server
	conn     *net.UnixConn
	rd       *wayland.Reader
	wr       *wayland.Writer
	objs     map[uint32]*object
	serial   uint32
	ptrID    uint32
	kbdID    uint32
	ptrX     int
	ptrY     int
	entered  uint32
	ptrBtns  pointerButtons
	kbdSurf  uint32
	serverID uint32
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
	cur.SetTakeFD(c.rd.TakeFD)
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
			if o.buf != nil && o.buf.pool != nil {
				o.buf.pool.live--
				releasePool(o.buf.pool)
			}
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
	case kindLinuxDmabuf:
		return c.reqLinuxDmabuf(o, msg.Opcode, cur)
	case kindDmaParams:
		return c.reqDmaParams(o, msg.Opcode, cur)
	case kindDecoMgr:
		return c.reqDecoMgr(o, msg.Opcode, cur)
	case kindDeco:
		return c.reqDeco(o, msg.Opcode, cur)
	case kindViewporter:
		return c.reqViewporter(o, msg.Opcode, cur)
	case kindViewport:
		return c.reqViewport(o, msg.Opcode, cur)
	case kindSubcomp:
		id, err := cur.U32()
		if err == nil {
			c.objs[id] = &object{id: id, kind: kindSubsurface}
		}
		return nil
	case kindDataDeviceManager:
		id, err := cur.U32()
		if err != nil {
			return nil
		}
		c.objs[id] = &object{id: id, kind: kindDataDevice}
		return nil
	case kindPointer:
		return c.reqPointer(o, msg.Opcode, cur)
	case kindCursorShapeMgr:
		return c.reqCursorShapeMgr(o, msg.Opcode, cur)
	case kindCursorShape:
		return c.reqCursorShape(o, msg.Opcode, cur)
	case kindActivation:
		return c.reqActivation(o, msg.Opcode, cur)
	case kindActToken:
		return c.reqActToken(o, msg.Opcode, cur)
	case kindPrimMgr:
		return c.reqPrimaryMgr(o, msg.Opcode, cur)
	case kindPrimDevice:
		return c.reqPrimDevice(o, msg.Opcode, cur)
	case kindXwShell:
		return c.reqXwaylandShell(o, msg.Opcode, cur)
	case kindXwSurface:
		return c.reqXwaylandSurface(o, msg.Opcode, cur)
	case kindFracScaleMgr:
		return c.reqFracScaleMgr(o, msg.Opcode, cur)
	case kindFracScale:
		return c.reqFracScale(o, msg.Opcode, cur)
	case kindKeyboard, kindOutput, kindDataDevice, kindPositioner, kindCallback, kindDmaFeedback, kindSubsurface, kindPrimSource:
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
		{globalCompositor, "wl_compositor", 6},
		{globalShm, "wl_shm", 1},
		{globalXdgWm, "xdg_wm_base", 5},
		{globalSeat, "wl_seat", 8},
		{globalOutput, "wl_output", 4},
		{globalDmabuf, "zwp_linux_dmabuf_v1", linuxDmabufAdvertiseVersion()},
		{globalDeco, "zxdg_decoration_manager_v1", 1},
		{globalViewporter, "wp_viewporter", 1},
		{globalDataDev, "wl_data_device_manager", 3},
		{globalSubcomp, "wl_subcompositor", 1},
		{globalCursorShape, "wp_cursor_shape_manager_v1", 1},
		{globalActivation, "xdg_activation_v1", 1},
		{globalPrimary, "zwp_primary_selection_device_manager_v1", 1},
		{globalXwayland, "xwayland_shell_v1", 1},
		{globalFractionalScale, "wp_fractional_scale_manager_v1", 1},
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
		c.objs[id] = o
		return c.send(id, 0, wayland.PutU32(nil, c.nextSerial()), nil) // ping
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
	case globalDmabuf:
		o.kind = kindLinuxDmabuf
		c.objs[id] = o
		return c.advertiseLinuxDmabuf(id)
	case globalDeco:
		o.kind = kindDecoMgr
	case globalViewporter:
		o.kind = kindViewporter
	case globalDataDev:
		o.kind = kindDataDeviceManager
	case globalSubcomp:
		o.kind = kindSubcomp
	case globalCursorShape:
		o.kind = kindCursorShapeMgr
	case globalActivation:
		o.kind = kindActivation
	case globalPrimary:
		o.kind = kindPrimMgr
	case globalXwayland:
		o.kind = kindXwShell
	case globalFractionalScale:
		o.kind = kindFracScaleMgr
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
		if o.pool != nil {
			o.pool.live++
		}
		c.objs[id] = &object{id: id, kind: kindBuffer, buf: &shmBuffer{
			pool: o.pool, offset: int(off), w: int(w), h: int(h), stride: int(stride), format: format,
		}}
	case 1: // destroy — buffers may still map this pool
		if o.pool != nil {
			o.pool.gone = true
			releasePool(o.pool)
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
		if c.entered == o.id {
			c.pointerLeaveCurrent()
		}
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
			s.pending = b
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
	if s.attached != nil && s.attached.dma != nil {
		if err := c.resolveDma(s.attached.dma); err != nil {
			c.srv.log.Printf("dmabuf resolve: %v", err)
		}
	}
	xdgReady := s.xdg != nil && s.xdg.top != nil && s.xdg.acked != 0
	if s.attached != nil && (xdgReady || s.xwayland) {
		c.mapSurface(s)
	}
}

func (c *Client) mapSurface(s *surface) {
	o := s.attached
	if o == nil {
		return
	}
	var pix []byte
	var w, h, stride int
	switch {
	case o.buf != nil && o.buf.pool != nil:
		b := o.buf
		n := b.stride * b.h
		if b.w <= 0 || b.h <= 0 || b.stride <= 0 || b.offset < 0 || n <= 0 {
			return
		}
		need := b.offset + n
		if b.pool.mem != nil && need >= b.offset && need <= len(b.pool.mem) {
			out := make([]byte, n)
			copy(out, b.pool.mem[b.offset:need])
			pix, w, h, stride = out, b.w, b.h, b.stride
			break
		}
		if b.pool.fd > 0 {
			out := make([]byte, n)
			got, err := syscall.Pread(b.pool.fd, out, int64(b.offset))
			if err != nil || got != n {
				c.srv.log.Printf("shm pread w=%d h=%d stride=%d off=%d got=%d err=%v",
					b.w, b.h, b.stride, b.offset, got, err)
				return
			}
			pix, w, h, stride = out, b.w, b.h, b.stride
			break
		}
		return
	case o.dma != nil && len(o.dma.pixels) > 0:
		d := o.dma
		pix = d.pixels
		w, h, stride = d.w, d.h, d.stride
	default:
		return
	}
	if s.destW > 0 && s.destH > 0 {
		w, h = s.destW, s.destH
	}
	if s.actor == nil {
		s.actor = &engine.Actor{}
		c.srv.Scene.PlaceNew(s.actor, c.srv.ScreenW, c.srv.ScreenH, decorations.Border, decorations.TitleH)
		c.srv.Scene.Add(s.actor)
	}
	s.actor.Width = w
	s.actor.Height = h
	s.actor.Stride = stride
	s.actor.Pixels = pix
	if s.xdg != nil && s.xdg.top != nil {
		s.actor.Title = s.xdg.top.title
		s.actor.AppID = s.xdg.top.app
	} else if s.xwayland {
		if s.actor.Title == "" {
			s.actor.Title = "X11"
		}
		if s.actor.AppID == "" {
			s.actor.AppID = "xwayland"
		}
		c.srv.log.Printf("mapped X11 actor %dx%d", w, h)
	}
	_ = c.send(o.id, 0, nil, nil) // wl_buffer.release
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
	w, h := c.srv.ScreenW*2/3, c.srv.ScreenH*2/3
	if w < 800 {
		w = 800
	}
	if h < 600 {
		h = 600
	}
	// wm_capabilities (v5): window_menu=1 maximize=2 fullscreen=3 minimize=4
	caps := wayland.PutU32(nil, 1)
	caps = wayland.PutU32(caps, 2)
	caps = wayland.PutU32(caps, 3)
	caps = wayland.PutU32(caps, 4)
	if err := c.send(xs.top.id, 3, wayland.PutArray(nil, caps), nil); err != nil {
		return err
	}
	p := wayland.PutI32(nil, int32(c.srv.ScreenW))
	p = wayland.PutI32(p, int32(c.srv.ScreenH))
	if err := c.send(xs.top.id, 2, p, nil); err != nil { // configure_bounds
		return err
	}
	p = wayland.PutI32(nil, int32(w))
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
		return c.sendKeymap(id)
	case 2:
		c.objs[id] = &object{id: id, kind: kindDataDevice} // get_touch ignored as dummy
	}
	return nil
}

func (c *Client) reqDecoMgr(_ *object, op uint16, cur *wayland.Cursor) error {
	if op != 1 { // get_toplevel_decoration
		return nil
	}
	id, err := cur.U32()
	if err != nil {
		return err
	}
	_, _ = cur.U32() // xdg_toplevel
	c.objs[id] = &object{id: id, kind: kindDeco}
	return c.send(id, 0, wayland.PutU32(nil, 2), nil) // server-side
}

func (c *Client) reqDeco(o *object, op uint16, cur *wayland.Cursor) error {
	switch op {
	case 0:
		delete(c.objs, o.id)
	case 1: // set_mode — we still force SSD
		_ = c.send(o.id, 0, wayland.PutU32(nil, 2), nil)
	}
	return nil
}

func (c *Client) reqViewporter(_ *object, op uint16, cur *wayland.Cursor) error {
	if op != 1 {
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
	c.objs[id] = &object{id: id, kind: kindViewport, surf: nil}
	if so := c.objs[sid]; so != nil {
		c.objs[id].surf = so.surf
	}
	return nil
}

func (c *Client) reqViewport(o *object, op uint16, cur *wayland.Cursor) error {
	s := o.surf
	switch op {
	case 0:
		delete(c.objs, o.id)
	case 2: // set_destination
		w, _ := cur.I32()
		h, _ := cur.I32()
		if s != nil {
			s.destW, s.destH = int(w), int(h)
		}
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
	ws := 0
	if c.srv != nil && c.srv.Scene != nil {
		ws = c.srv.Scene.ActiveWorkspace()
	}
	for _, o := range c.objs {
		if o.surf != nil && o.surf.actor != nil && o.surf.actor.Focused && o.surf.actor.Workspace == ws {
			return o.surf
		}
	}
	// first mapped on the active desktop
	for _, o := range c.objs {
		if o.surf != nil && o.surf.actor != nil && o.surf.actor.Workspace == ws {
			return o.surf
		}
	}
	return nil
}

