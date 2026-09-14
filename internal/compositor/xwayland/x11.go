package xwayland

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

// Minimal X11 core protocol (little-endian) for a rootless XWM.
// Spec: X Window System Protocol, X11R6.

const (
	xOpChangeWindowAttributes = 2
	xOpMapWindow              = 8
	xOpConfigureWindow        = 12
	xOpInternAtom             = 16
	xOpChangeProperty         = 18
	xOpQueryExtension         = 98

	xCompositeRedirectSubwindows = 2
	xCompositeRedirectManual     = 1

	xCWEventMask = 1 << 11

	xEventSubstructureNotify   = 1 << 19
	xEventSubstructureRedirect = 1 << 20

	xEvError            = 0
	xEvReply            = 1
	xEvMapRequest       = 20
	xEvConfigureRequest = 23

	xCfgX         = 1 << 0
	xCfgY         = 1 << 1
	xCfgWidth     = 1 << 2
	xCfgHeight    = 1 << 3
	xCfgBorder    = 1 << 4
	xCfgSibling   = 1 << 5
	xCfgStackMode = 1 << 6
	xPropReplace  = 0
	xNormalState  = 1
	xAtomCardinal = 6
)

type xConn struct {
	c            net.Conn
	seq          uint16
	root         uint32
	wmState      uint32
	allowCommits uint32
	compositeOp  byte
	byteOrder    binary.ByteOrder
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
	atom, err := xc.internAtom("WM_STATE")
	if err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("InternAtom WM_STATE: %w", err)
	}
	xc.wmState = atom
	allow, err := xc.internAtom("_XWAYLAND_ALLOW_COMMITS")
	if err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("InternAtom _XWAYLAND_ALLOW_COMMITS: %w", err)
	}
	xc.allowCommits = allow
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
	want := x.seq
	for {
		kind, seq, payload, err := x.readMsg()
		if err != nil {
			return fmt.Errorf("QueryExtension Composite: %w", err)
		}
		if kind == xEvError {
			return fmt.Errorf("QueryExtension Composite: X11 error")
		}
		if kind == xEvReply && seq == want {
			if len(payload) < 10 || payload[8] == 0 {
				return fmt.Errorf("Xwayland has no Composite extension (needed for rootless surfaces)")
			}
			x.compositeOp = payload[9]
			return nil
		}
	}
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
	want := x.seq
	for {
		kind, seq, payload, err := x.readMsg()
		if err != nil {
			return 0, err
		}
		if kind == xEvError {
			return 0, fmt.Errorf("X11 error intern %q code=%d", name, payload[1])
		}
		if kind == xEvReply && seq == want {
			if len(payload) < 12 {
				return 0, fmt.Errorf("InternAtom reply short")
			}
			return binary.LittleEndian.Uint32(payload[8:]), nil
		}
	}
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

func (x *xConn) send(pkt []byte) error {
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

func (x *xConn) readEvent() (kind byte, payload []byte, err error) {
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
