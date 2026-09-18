//go:build linux && cgo

package xwayland

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// A small independent X11 client checks what the real X server observes, rather
// than treating the bridge's own cached metadata as proof of focus behavior.
type wireClient struct {
	t    *testing.T
	conn net.Conn
	root uint32
	base uint32
	next uint32
	seq  uint16
}

func openWireClient(t *testing.T, b *Bridge) *wireClient {
	t.Helper()
	data, err := os.ReadFile(b.authority)
	if err != nil {
		t.Fatal(err)
	}
	r := bytes.NewReader(data[2:])
	var fields [][]byte
	for r.Len() > 0 {
		var n uint16
		binary.Read(r, binary.BigEndian, &n)
		p := make([]byte, n)
		if _, err := io.ReadFull(r, p); err != nil {
			t.Fatal(err)
		}
		fields = append(fields, p)
	}
	if len(fields) != 4 {
		t.Fatal("malformed authority file")
	}
	name, cookie := fields[2], fields[3]
	c, err := net.DialTimeout("unix", "/tmp/.X11-unix/X"+strings.TrimPrefix(b.Display(), ":"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(2 * time.Second))
	pad := func(n int) int { return (n + 3) &^ 3 }
	req := make([]byte, 12+pad(len(name))+pad(len(cookie)))
	req[0] = 'l'
	binary.LittleEndian.PutUint16(req[2:], 11)
	binary.LittleEndian.PutUint16(req[6:], uint16(len(name)))
	binary.LittleEndian.PutUint16(req[8:], uint16(len(cookie)))
	copy(req[12:], name)
	copy(req[12+pad(len(name)):], cookie)
	if _, err := c.Write(req); err != nil {
		t.Fatal(err)
	}
	var prefix [8]byte
	if _, err := io.ReadFull(c, prefix[:]); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, int(binary.LittleEndian.Uint16(prefix[6:]))*4)
	if _, err := io.ReadFull(c, body); err != nil {
		t.Fatal(err)
	}
	if prefix[0] != 1 {
		t.Fatalf("authenticated X11 setup failed: %s", body)
	}
	if len(body) < 32 {
		t.Fatal("short X11 setup")
	}
	off := 32 + pad(int(binary.LittleEndian.Uint16(body[16:]))) + int(body[21])*8
	if off+4 > len(body) || body[20] < 1 {
		t.Fatal("missing X11 root")
	}
	return &wireClient{t: t, conn: c, root: binary.LittleEndian.Uint32(body[off:]), base: binary.LittleEndian.Uint32(body[4:]), next: 1}
}
func (c *wireClient) send(packet []byte) {
	c.t.Helper()
	c.conn.SetDeadline(time.Now().Add(2 * time.Second))
	binary.LittleEndian.PutUint16(packet[2:], uint16(len(packet)/4))
	c.seq++
	if _, err := c.conn.Write(packet); err != nil {
		c.t.Fatal(err)
	}
}
func (c *wireClient) reply() []byte {
	c.t.Helper()
	for {
		p := c.packet()
		if p[0] == 0 {
			c.t.Fatalf("X11 error %d sequence=%d request=%d", p[1], binary.LittleEndian.Uint16(p[2:]), p[10])
		}
		if p[0] == 1 {
			return p
		}
	}
}
func (c *wireClient) packet() []byte {
	c.t.Helper()
	p := make([]byte, 32)
	if _, err := io.ReadFull(c.conn, p); err != nil {
		c.t.Fatal(err)
	}
	if p[0] == 1 {
		extra := binary.LittleEndian.Uint32(p[4:]) * 4
		if extra > 2<<20 {
			c.t.Fatal("oversized X11 test reply")
		}
		data := make([]byte, extra)
		if _, err := io.ReadFull(c.conn, data); err != nil {
			c.t.Fatal(err)
		}
		p = append(p, data...)
	}
	return p
}
func (c *wireClient) event(kind byte) []byte {
	c.t.Helper()
	c.conn.SetDeadline(time.Now().Add(5 * time.Second))
	for {
		p := c.packet()
		if p[0] == 0 {
			c.t.Fatalf("X11 error %d sequence=%d request=%d", p[1], binary.LittleEndian.Uint16(p[2:]), p[10])
		}
		if p[0]&0x7f == kind {
			return p
		}
		if p[0] == 1 {
			c.t.Fatalf("unexpected X11 reply while waiting for event %d", kind)
		}
	}
}
func (c *wireClient) focus() uint32 {
	p := make([]byte, 4)
	p[0] = 43
	c.send(p)
	return binary.LittleEndian.Uint32(c.reply()[8:])
}
func (c *wireClient) atom(name string) uint32 {
	p := make([]byte, 8+(len(name)+3)&^3)
	p[0] = 16
	binary.LittleEndian.PutUint16(p[4:], uint16(len(name)))
	copy(p[8:], name)
	c.send(p)
	return binary.LittleEndian.Uint32(c.reply()[8:])
}
func (c *wireClient) activate(id uint32) {
	atom := c.atom("_NET_ACTIVE_WINDOW")
	p := make([]byte, 44)
	p[0] = 25
	binary.LittleEndian.PutUint32(p[4:], c.root)
	binary.LittleEndian.PutUint32(p[8:], (1<<20)|(1<<19))
	p[12] = 33
	p[13] = 32
	binary.LittleEndian.PutUint32(p[16:], id)
	binary.LittleEndian.PutUint32(p[20:], atom)
	binary.LittleEndian.PutUint32(p[24:], 1)
	c.send(p)
}
func (c *wireClient) title(id uint32, title string) {
	atom, utf8 := c.atom("_NET_WM_NAME"), c.atom("UTF8_STRING")
	p := make([]byte, 24+(len(title)+3)&^3)
	p[0] = 18
	binary.LittleEndian.PutUint32(p[4:], id)
	binary.LittleEndian.PutUint32(p[8:], atom)
	binary.LittleEndian.PutUint32(p[12:], utf8)
	p[16] = 8
	binary.LittleEndian.PutUint32(p[20:], uint32(len(title)))
	copy(p[24:], title)
	c.send(p)
}
func (c *wireClient) mapped(id uint32, visible bool) {
	p := make([]byte, 8)
	p[0] = 10
	if visible {
		p[0] = 8
	}
	binary.LittleEndian.PutUint32(p[4:], id)
	c.send(p)
}
func (c *wireClient) window() uint32 {
	return c.windowSize(1, 1)
}
func (c *wireClient) windowSize(width, height uint16) uint32 {
	id := c.base | c.next
	c.next++
	p := make([]byte, 32)
	p[0] = 1
	binary.LittleEndian.PutUint32(p[4:], id)
	binary.LittleEndian.PutUint32(p[8:], c.root)
	binary.LittleEndian.PutUint16(p[16:], width)
	binary.LittleEndian.PutUint16(p[18:], height)
	binary.LittleEndian.PutUint16(p[22:], 1)
	c.send(p)
	return id
}
func (c *wireClient) clientMessage(destination, messageType uint32, data [5]uint32) {
	event := make([]byte, 32)
	event[0] = 33
	event[1] = 32
	binary.LittleEndian.PutUint32(event[4:], destination)
	binary.LittleEndian.PutUint32(event[8:], messageType)
	for i, value := range data {
		binary.LittleEndian.PutUint32(event[12+i*4:], value)
	}
	p := make([]byte, 44)
	p[0] = 25
	binary.LittleEndian.PutUint32(p[4:], destination)
	copy(p[12:], event)
	c.send(p)
}
func (c *wireClient) changeProperty(window, property, propertyType uint32, format byte, data []byte) {
	c.t.Helper()
	unit := 1
	switch format {
	case 8:
	case 16:
		unit = 2
	case 32:
		unit = 4
	default:
		c.t.Fatalf("invalid property format %d", format)
	}
	if len(data)%unit != 0 {
		c.t.Fatal("unaligned property data")
	}
	p := make([]byte, 24+(len(data)+3)&^3)
	p[0] = 18
	binary.LittleEndian.PutUint32(p[4:], window)
	binary.LittleEndian.PutUint32(p[8:], property)
	binary.LittleEndian.PutUint32(p[12:], propertyType)
	p[16] = format
	binary.LittleEndian.PutUint32(p[20:], uint32(len(data)/unit))
	copy(p[24:], data)
	c.send(p)
}
func (c *wireClient) selectionOwner(selection uint32) uint32 {
	p := make([]byte, 8)
	p[0] = 23
	binary.LittleEndian.PutUint32(p[4:], selection)
	c.send(p)
	return binary.LittleEndian.Uint32(c.reply()[8:])
}
func (c *wireClient) setSelectionOwner(owner, selection uint32) {
	p := make([]byte, 16)
	p[0] = 22
	binary.LittleEndian.PutUint32(p[4:], owner)
	binary.LittleEndian.PutUint32(p[8:], selection)
	c.send(p)
}
func (c *wireClient) convertSelection(requestor, selection, target, property uint32) {
	p := make([]byte, 24)
	p[0] = 24
	binary.LittleEndian.PutUint32(p[4:], requestor)
	binary.LittleEndian.PutUint32(p[8:], selection)
	binary.LittleEndian.PutUint32(p[12:], target)
	binary.LittleEndian.PutUint32(p[16:], property)
	c.send(p)
}
func (c *wireClient) selectionNotify(request []byte, property uint32) {
	event := make([]byte, 32)
	event[0] = 31
	copy(event[4:8], request[4:8])
	copy(event[8:12], request[12:16])
	copy(event[12:16], request[16:20])
	copy(event[16:20], request[20:24])
	binary.LittleEndian.PutUint32(event[20:], property)
	p := make([]byte, 44)
	p[0] = 25
	binary.LittleEndian.PutUint32(p[4:], binary.LittleEndian.Uint32(request[12:]))
	copy(p[12:], event)
	c.send(p)
}
func (c *wireClient) property(window, property, propertyType uint32) (byte, uint32, []byte) {
	p := make([]byte, 24)
	p[0] = 20
	binary.LittleEndian.PutUint32(p[4:], window)
	binary.LittleEndian.PutUint32(p[8:], property)
	binary.LittleEndian.PutUint32(p[12:], propertyType)
	binary.LittleEndian.PutUint32(p[20:], 1<<19)
	c.send(p)
	reply := c.reply()
	format, actual := reply[1], binary.LittleEndian.Uint32(reply[8:])
	length := int(binary.LittleEndian.Uint32(reply[16:])) * int(format) / 8
	if length > len(reply)-32 {
		c.t.Fatal("short X11 property reply")
	}
	return format, actual, append([]byte(nil), reply[32:32+length]...)
}
func (c *wireClient) String() string { return fmt.Sprintf("X11 root %x", c.root) }
