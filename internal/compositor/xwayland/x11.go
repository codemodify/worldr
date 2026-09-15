package xwayland

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Minimal X11 core protocol (little-endian) for a rootless XWM.
// Spec: X Window System Protocol, X11R6.

const (
	xOpCreateWindow           = 1
	xOpChangeWindowAttributes = 2
	xOpGetWindowAttributes    = 3
	xOpMapWindow              = 8
	xOpConfigureWindow        = 12
	xOpGetGeometry            = 14
	xOpInternAtom             = 16
	xOpChangeProperty         = 18
	xOpGetProperty            = 20
	xOpSendEvent              = 25
	xOpSetInputFocus          = 42
	xOpQueryExtension         = 98

	xCompositeRedirectSubwindows = 2
	xCompositeRedirectManual     = 1

	xCWEventMask = 1 << 11

	xEventStructureNotify      = 1 << 17
	xEventSubstructureNotify   = 1 << 19
	xEventSubstructureRedirect = 1 << 20
	xEventPropertyChange       = 1 << 22

	xEvError            = 0
	xEvReply            = 1
	xEvDestroyNotify    = 17
	xEvUnmapNotify      = 18
	xEvMapNotify        = 19
	xEvMapRequest       = 20
	xEvConfigureNotify  = 22
	xEvConfigureRequest = 23
	xEvPropertyNotify   = 28
	xEvClientMessage    = 33

	xCfgX          = 1 << 0
	xCfgY          = 1 << 1
	xCfgWidth      = 1 << 2
	xCfgHeight     = 1 << 3
	xCfgBorder     = 1 << 4
	xCfgSibling    = 1 << 5
	xCfgStackMode  = 1 << 6
	xStackAbove    = 0
	xPropReplace   = 0
	xNormalState   = 1
	xAtomAtom      = 4
	xAtomCardinal  = 6
	xAtomString    = 31
	xAtomWindow    = 33
	xRevertToPtr   = 1
	xClassInputOut = 1
)

type queuedMsg struct {
	kind    byte
	payload []byte
}

type xConn struct {
	c            net.Conn
	wmu          sync.Mutex
	seq          uint16
	root         uint32
	ridBase      uint32
	ridMask      uint32
	nextRID      uint32
	wmState      uint32
	allowCommits uint32
	compositeOp  byte
	byteOrder    binary.ByteOrder
	q            []queuedMsg
}

func dialX11(displayNum int, timeout time.Duration) (net.Conn, error) {
	path := fmt.Sprintf("/tmp/.X11-unix/X%d", displayNum)
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("unix", path, 200*time.Millisecond)
		if err == nil {
			return c, nil
		}
		last = err
		time.Sleep(20 * time.Millisecond)
	}
	if last == nil {
		last = fmt.Errorf("timeout connecting to %s", path)
	}
	return nil, last
}

func x11Connect(displayNum int, timeout time.Duration) (*xConn, error) {
	raw, err := dialX11(displayNum, timeout)
	if err != nil {
		return nil, err
	}
	return x11ConnectConn(raw)
}

func x11ConnectConn(raw net.Conn) (*xConn, error) {
	_ = raw.SetDeadline(time.Now().Add(1500 * time.Millisecond))
	xc := &xConn{c: raw, byteOrder: binary.LittleEndian}
	if err := xc.handshake(); err != nil {
		_ = raw.Close()
		return nil, err
	}
	if err := xc.queryComposite(); err != nil {
		_ = raw.Close()
		return nil, err
	}
	_ = raw.SetDeadline(time.Time{})
	return xc, nil
}

func (x *xConn) queryComposite() error {
	name := "Composite"
	pkt := make([]byte, 8+pad4(len(name)))
	pkt[0] = xOpQueryExtension
	binary.LittleEndian.PutUint16(pkt[2:], uint16(len(pkt)/4))
	binary.LittleEndian.PutUint16(pkt[4:], uint16(len(name)))
	copy(pkt[8:], name)
	if err := x.send(pkt); err != nil {
		return err
	}
	payload, err := x.waitReply(x.seq)
	if err != nil {
		return fmt.Errorf("QueryExtension Composite: %w", err)
	}
	if len(payload) < 10 || payload[8] == 0 {
		return fmt.Errorf("Xwayland has no Composite extension (needed for rootless surfaces)")
	}
	x.compositeOp = payload[9]
	return nil
}

func (x *xConn) compositeRedirectSubwindows() error {
	if x.compositeOp == 0 {
		return fmt.Errorf("no Composite opcode")
	}
	pkt := make([]byte, 12)
	pkt[0] = x.compositeOp
	pkt[1] = xCompositeRedirectSubwindows
	binary.LittleEndian.PutUint16(pkt[2:], 3)
	binary.LittleEndian.PutUint32(pkt[4:], x.root)
	pkt[8] = xCompositeRedirectManual
	return x.send(pkt)
}

func (x *xConn) Close() {
	if x != nil && x.c != nil {
		_ = x.c.Close()
	}
}

func (x *xConn) handshake() error {
	// Setup request, no auth (-ac).
	req := make([]byte, 12)
	req[0] = 'l'
	binary.LittleEndian.PutUint16(req[2:], 11)
	if err := x.writeAll(req); err != nil {
		return err
	}
	prefix := make([]byte, 8)
	if _, err := io.ReadFull(x.c, prefix); err != nil {
		return fmt.Errorf("X11 setup prefix: %w", err)
	}
	if prefix[0] != 1 {
		n := int(prefix[1])
		extra := int(binary.LittleEndian.Uint16(prefix[6:])) * 4
		reason := make([]byte, extra)
		_, _ = io.ReadFull(x.c, reason)
		if n > extra {
			n = extra
		}
		return fmt.Errorf("X11 setup failed: %s", string(reason[:n]))
	}
	extra := int(binary.LittleEndian.Uint16(prefix[6:])) * 4
	body := make([]byte, extra)
	if _, err := io.ReadFull(x.c, body); err != nil {
		return fmt.Errorf("X11 setup body: %w", err)
	}
	if len(body) < 32 {
		return fmt.Errorf("X11 setup body too short (%d)", len(body))
	}
	x.ridBase = binary.LittleEndian.Uint32(body[4:])
	x.ridMask = binary.LittleEndian.Uint32(body[8:])
	vendorLen := int(binary.LittleEndian.Uint16(body[16:]))
	nRoots := int(body[20])
	nFormats := int(body[21])
	off := 32 + pad4(vendorLen)
	off += nFormats * 8
	if off+4 > len(body) || nRoots < 1 {
		return fmt.Errorf("X11 setup: no root window")
	}
	x.root = binary.LittleEndian.Uint32(body[off:])
	return nil
}

func (x *xConn) internAtom(name string) (uint32, error) {
	n := len(name)
	pkt := make([]byte, 8+pad4(n))
	pkt[0] = xOpInternAtom
	binary.LittleEndian.PutUint16(pkt[2:], uint16(len(pkt)/4))
	binary.LittleEndian.PutUint16(pkt[4:], uint16(n))
	copy(pkt[8:], name)
	if err := x.send(pkt); err != nil {
		return 0, err
	}
	payload, err := x.waitReply(x.seq)
	if err != nil {
		return 0, fmt.Errorf("intern %q: %w", name, err)
	}
	if len(payload) < 12 {
		return 0, fmt.Errorf("InternAtom reply short")
	}
	return binary.LittleEndian.Uint32(payload[8:]), nil
}

func (x *xConn) selectSubstructure() error {
	mask := uint32(xEventSubstructureNotify | xEventSubstructureRedirect)
	pkt := make([]byte, 16)
	pkt[0] = xOpChangeWindowAttributes
	binary.LittleEndian.PutUint16(pkt[2:], 4)
	binary.LittleEndian.PutUint32(pkt[4:], x.root)
	binary.LittleEndian.PutUint32(pkt[8:], xCWEventMask)
	binary.LittleEndian.PutUint32(pkt[12:], mask)
	return x.send(pkt)
}

func (x *xConn) mapWindow(win uint32) error {
	pkt := make([]byte, 8)
	pkt[0] = xOpMapWindow
	binary.LittleEndian.PutUint16(pkt[2:], 2)
	binary.LittleEndian.PutUint32(pkt[4:], win)
	return x.send(pkt)
}

func (x *xConn) changeProp32(win, prop, typ uint32, values ...uint32) error {
	n := len(values)
	pkt := make([]byte, 24+4*n)
	pkt[0] = xOpChangeProperty
	pkt[1] = xPropReplace
	binary.LittleEndian.PutUint16(pkt[2:], uint16(len(pkt)/4))
	binary.LittleEndian.PutUint32(pkt[4:], win)
	binary.LittleEndian.PutUint32(pkt[8:], prop)
	binary.LittleEndian.PutUint32(pkt[12:], typ)
	pkt[16] = 32
	binary.LittleEndian.PutUint32(pkt[20:], uint32(n))
	for i, v := range values {
		binary.LittleEndian.PutUint32(pkt[24+4*i:], v)
	}
	return x.send(pkt)
}

func (x *xConn) setWMStateNormal(win uint32) error {
	if x.wmState == 0 {
		return nil
	}
	return x.changeProp32(win, x.wmState, x.wmState, xNormalState, 0)
}

func (x *xConn) setAllowCommits(win uint32) error {
	if x.allowCommits == 0 {
		return nil
	}
	return x.changeProp32(win, x.allowCommits, xAtomCardinal, 1)
}

func (x *xConn) configureFromRequest(ev []byte) error {
	// ConfigureRequest: stack-mode, seq, parent, window, sibling, x, y, w, h, border, mask
	if len(ev) < 28 {
		return nil
	}
	stack := ev[1]
	win := binary.LittleEndian.Uint32(ev[8:])
	sibling := binary.LittleEndian.Uint32(ev[12:])
	xx := int16(binary.LittleEndian.Uint16(ev[16:]))
	yy := int16(binary.LittleEndian.Uint16(ev[18:]))
	w := binary.LittleEndian.Uint16(ev[20:])
	h := binary.LittleEndian.Uint16(ev[22:])
	border := binary.LittleEndian.Uint16(ev[24:])
	mask := binary.LittleEndian.Uint16(ev[26:])

	vals := make([]uint32, 0, 7)
	if mask&xCfgX != 0 {
		vals = append(vals, uint32(int32(xx)))
	}
	if mask&xCfgY != 0 {
		vals = append(vals, uint32(int32(yy)))
	}
	if mask&xCfgWidth != 0 {
		vals = append(vals, uint32(w))
	}
	if mask&xCfgHeight != 0 {
		vals = append(vals, uint32(h))
	}
	if mask&xCfgBorder != 0 {
		vals = append(vals, uint32(border))
	}
	if mask&xCfgSibling != 0 {
		vals = append(vals, sibling)
	}
	if mask&xCfgStackMode != 0 {
		vals = append(vals, uint32(stack))
	}
	pkt := make([]byte, 12+4*len(vals))
	pkt[0] = xOpConfigureWindow
	binary.LittleEndian.PutUint16(pkt[2:], uint16(len(pkt)/4))
	binary.LittleEndian.PutUint32(pkt[4:], win)
	binary.LittleEndian.PutUint16(pkt[8:], mask)
	for i, v := range vals {
		binary.LittleEndian.PutUint32(pkt[12+4*i:], v)
	}
	return x.send(pkt)
}

func (x *xConn) allocID() uint32 {
	id := x.ridBase | (x.nextRID & x.ridMask)
	x.nextRID++
	return id
}

func (x *xConn) createInputOutput(parent uint32, w, h uint16) (uint32, error) {
	id := x.allocID()
	pkt := make([]byte, 32)
	pkt[0] = xOpCreateWindow
	binary.LittleEndian.PutUint16(pkt[2:], 8)
	binary.LittleEndian.PutUint32(pkt[4:], id)
	binary.LittleEndian.PutUint32(pkt[8:], parent)
	binary.LittleEndian.PutUint16(pkt[16:], w)
	binary.LittleEndian.PutUint16(pkt[18:], h)
	binary.LittleEndian.PutUint16(pkt[22:], xClassInputOut)
	return id, x.send(pkt)
}

func (x *xConn) selectWindowEvents(win uint32) error {
	mask := uint32(xEventStructureNotify | xEventPropertyChange)
	pkt := make([]byte, 16)
	pkt[0] = xOpChangeWindowAttributes
	binary.LittleEndian.PutUint16(pkt[2:], 4)
	binary.LittleEndian.PutUint32(pkt[4:], win)
	binary.LittleEndian.PutUint32(pkt[8:], xCWEventMask)
	binary.LittleEndian.PutUint32(pkt[12:], mask)
	return x.send(pkt)
}

func (x *xConn) getOverrideRedirect(win uint32) (bool, error) {
	pkt := make([]byte, 8)
	pkt[0] = xOpGetWindowAttributes
	binary.LittleEndian.PutUint16(pkt[2:], 2)
	binary.LittleEndian.PutUint32(pkt[4:], win)
	if err := x.send(pkt); err != nil {
		return false, err
	}
	payload, err := x.waitReply(x.seq)
	if err != nil {
		return false, err
	}
	if len(payload) < 28 {
		return false, nil
	}
	return payload[27] != 0, nil
}

func (x *xConn) getGeometry(win uint32) (xx, yy, w, h int, err error) {
	pkt := make([]byte, 8)
	pkt[0] = xOpGetGeometry
	binary.LittleEndian.PutUint16(pkt[2:], 2)
	binary.LittleEndian.PutUint32(pkt[4:], win)
	if err := x.send(pkt); err != nil {
		return 0, 0, 0, 0, err
	}
	payload, err := x.waitReply(x.seq)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if len(payload) < 20 {
		return 0, 0, 0, 0, nil
	}
	xx = int(int16(binary.LittleEndian.Uint16(payload[12:])))
	yy = int(int16(binary.LittleEndian.Uint16(payload[14:])))
	w = int(binary.LittleEndian.Uint16(payload[16:]))
	h = int(binary.LittleEndian.Uint16(payload[18:]))
	return xx, yy, w, h, nil
}

func (x *xConn) getProperty(win, prop uint32) (typ uint32, format byte, val []byte, err error) {
	pkt := make([]byte, 24)
	pkt[0] = xOpGetProperty
	binary.LittleEndian.PutUint16(pkt[2:], 6)
	binary.LittleEndian.PutUint32(pkt[4:], win)
	binary.LittleEndian.PutUint32(pkt[8:], prop)
	binary.LittleEndian.PutUint32(pkt[20:], 0xffff)
	if err := x.send(pkt); err != nil {
		return 0, 0, nil, err
	}
	payload, err := x.waitReply(x.seq)
	if err != nil {
		return 0, 0, nil, err
	}
	if len(payload) < 32 {
		return 0, 0, nil, nil
	}
	format = payload[1]
	typ = binary.LittleEndian.Uint32(payload[8:])
	n := binary.LittleEndian.Uint32(payload[16:])
	if typ == 0 || n == 0 {
		return typ, format, nil, nil
	}
	nbytes := int(n)
	switch format {
	case 16:
		nbytes = int(n) * 2
	case 32:
		nbytes = int(n) * 4
	}
	if 32+nbytes > len(payload) {
		nbytes = len(payload) - 32
		if nbytes < 0 {
			return typ, format, nil, nil
		}
	}
	val = make([]byte, nbytes)
	copy(val, payload[32:32+nbytes])
	return typ, format, val, nil
}

func (x *xConn) changeProp8(win, prop, typ uint32, data []byte) error {
	pkt := make([]byte, 24+pad4(len(data)))
	pkt[0] = xOpChangeProperty
	pkt[1] = xPropReplace
	binary.LittleEndian.PutUint16(pkt[2:], uint16(len(pkt)/4))
	binary.LittleEndian.PutUint32(pkt[4:], win)
	binary.LittleEndian.PutUint32(pkt[8:], prop)
	binary.LittleEndian.PutUint32(pkt[12:], typ)
	pkt[16] = 8
	binary.LittleEndian.PutUint32(pkt[20:], uint32(len(data)))
	copy(pkt[24:], data)
	return x.send(pkt)
}

func (x *xConn) setInputFocus(win uint32) error {
	pkt := make([]byte, 12)
	pkt[0] = xOpSetInputFocus
	pkt[1] = xRevertToPtr
	binary.LittleEndian.PutUint16(pkt[2:], 3)
	binary.LittleEndian.PutUint32(pkt[4:], win)
	return x.send(pkt)
}

func (x *xConn) sendClientMessage(win, typ uint32, data [5]uint32) error {
	ev := make([]byte, 32)
	ev[0] = xEvClientMessage
	ev[1] = 32
	binary.LittleEndian.PutUint32(ev[4:], win)
	binary.LittleEndian.PutUint32(ev[8:], typ)
	for i := 0; i < 5; i++ {
		binary.LittleEndian.PutUint32(ev[12+4*i:], data[i])
	}
	pkt := make([]byte, 44)
	pkt[0] = xOpSendEvent
	binary.LittleEndian.PutUint16(pkt[2:], 11)
	binary.LittleEndian.PutUint32(pkt[4:], win)
	copy(pkt[12:], ev)
	return x.send(pkt)
}

func (x *xConn) send(pkt []byte) error {
	x.wmu.Lock()
	defer x.wmu.Unlock()
	x.seq++
	if x.seq == 0 {
		x.seq = 1
	}
	return x.writeAll(pkt)
}

func (x *xConn) writeAll(b []byte) error {
	_, err := x.c.Write(b)
	return err
}

func (x *xConn) waitReply(want uint16) ([]byte, error) {
	for {
		kind, seq, payload, err := x.readMsg()
		if err != nil {
			return nil, err
		}
		if kind == xEvError && seq == want {
			code := byte(0)
			if len(payload) > 1 {
				code = payload[1]
			}
			return nil, fmt.Errorf("X11 error code=%d", code)
		}
		if kind == xEvReply && seq == want {
			return payload, nil
		}
		if kind != xEvReply && kind != xEvError {
			x.q = append(x.q, queuedMsg{kind: kind, payload: payload})
		}
	}
}

func (x *xConn) readEvent() (kind byte, payload []byte, err error) {
	if len(x.q) > 0 {
		ev := x.q[0]
		x.q = x.q[1:]
		return ev.kind, ev.payload, nil
	}
	kind, _, payload, err = x.readMsg()
	return kind, payload, err
}

func (x *xConn) readMsg() (kind byte, seq uint16, payload []byte, err error) {
	hdr := make([]byte, 32)
	if _, err = io.ReadFull(x.c, hdr); err != nil {
		return 0, 0, nil, err
	}
	kind = hdr[0] & 0x7f
	seq = binary.LittleEndian.Uint16(hdr[2:])
	if kind == xEvReply || kind == xEvError {
		extra := int(binary.LittleEndian.Uint32(hdr[4:])) * 4
		if kind == xEvError {
			extra = 0
		}
		if extra > 0 {
			more := make([]byte, 32+extra)
			copy(more, hdr)
			if _, err = io.ReadFull(x.c, more[32:]); err != nil {
				return kind, seq, nil, err
			}
			return kind, seq, more, nil
		}
	}
	return kind, seq, hdr, nil
}

func pad4(n int) int { return (n + 3) &^ 3 }
