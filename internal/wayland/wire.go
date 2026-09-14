// Package wayland implements the Wayland wire protocol (pure Go).
package wayland

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
)

var le = binary.LittleEndian

// Message is one Wayland request or event.
type Message struct {
	Object  uint32
	Opcode  uint16
	Payload []byte
	FDs     []int
}

// Encode writes a message (without FDs) into buf, returning the slice.
func Encode(object uint32, opcode uint16, payload []byte) []byte {
	size := 8 + len(payload)
	if size%4 != 0 {
		panic("wayland payload not 4-byte aligned")
	}
	out := make([]byte, size)
	le.PutUint32(out[0:4], object)
	le.PutUint32(out[4:8], uint32(size)<<16|uint32(opcode))
	copy(out[8:], payload)
	return out
}

// DecodeHeader parses the 8-byte header.
func DecodeHeader(h []byte) (object uint32, opcode uint16, size int, err error) {
	if len(h) < 8 {
		return 0, 0, 0, io.ErrUnexpectedEOF
	}
	object = le.Uint32(h[0:4])
	v := le.Uint32(h[4:8])
	opcode = uint16(v & 0xffff)
	size = int(v >> 16)
	if size < 8 || size%4 != 0 {
		return 0, 0, 0, fmt.Errorf("wayland: bad message size %d", size)
	}
	return object, opcode, size, nil
}

// Reader is a buffered Wayland byte+fd reader.
type Reader struct {
	c       *net.UnixConn
	buf     []byte
	fds     []int
	scratch [8]byte
}

func NewReader(c *net.UnixConn) *Reader {
	return &Reader{c: c, buf: make([]byte, 0, 4096)}
}

func (r *Reader) Next() (Message, error) {
	for {
		if len(r.buf) >= 8 {
			_, _, size, err := DecodeHeader(r.buf[:8])
			if err != nil {
				return Message{}, err
			}
			if len(r.buf) >= size {
				obj, op, _, _ := DecodeHeader(r.buf[:8])
				payload := append([]byte(nil), r.buf[8:size]...)
				r.buf = r.buf[size:]
				// FDs are a parallel stream (not in the payload). Callers
				// pull one at a time via TakeFD so batched create_pool /
				// dmabuf messages do not steal every SCM_RIGHTS fd.
				return Message{Object: obj, Opcode: op, Payload: payload}, nil
			}
		}
		if err := r.recv(); err != nil {
			return Message{}, err
		}
	}
}

// TakeFD pops the next received file descriptor (Wayland fd arguments).
func (r *Reader) TakeFD() (int, error) {
	if r == nil || len(r.fds) == 0 {
		return -1, errors.New("wayland: missing fd")
	}
	fd := r.fds[0]
	r.fds = r.fds[1:]
	return fd, nil
}

func (r *Reader) recv() error {
	oob := make([]byte, 256)
	b := make([]byte, 4096)
	n, oobn, _, _, err := r.c.ReadMsgUnix(b, oob)
	if err != nil {
		return err
	}
	if n > 0 {
		r.buf = append(r.buf, b[:n]...)
	}
	if oobn > 0 {
		fds, err := ParseUnixFDs(oob[:oobn])
		if err != nil {
			return err
		}
		r.fds = append(r.fds, fds...)
	}
	if n == 0 && oobn == 0 {
		return io.EOF
	}
	return nil
}

// Writer sends messages, optionally with FDs.
type Writer struct {
	c *net.UnixConn
}

func NewWriter(c *net.UnixConn) *Writer { return &Writer{c: c} }

func (w *Writer) Send(object uint32, opcode uint16, payload []byte, fds []int) error {
	msg := Encode(object, opcode, payload)
	if len(fds) == 0 {
		_, err := w.c.Write(msg)
		return err
	}
	oob := UnixRights(fds...)
	_, _, err := w.c.WriteMsgUnix(msg, oob, nil)
	return err
}

// Payload helpers -----------------------------------------------------------

type Cursor struct {
	p    []byte
	fds  []int
	fi   int
	take func() (int, error)
}

func NewCursor(payload []byte, fds []int) *Cursor {
	return &Cursor{p: payload, fds: fds}
}

// SetTakeFD uses a shared fd queue (typically Reader.TakeFD) so several
// messages that arrived in one recvmsg each get the correct descriptor.
func (c *Cursor) SetTakeFD(fn func() (int, error)) {
	c.take = fn
}

func (c *Cursor) U32() (uint32, error) {
	if len(c.p) < 4 {
		return 0, io.ErrUnexpectedEOF
	}
	v := le.Uint32(c.p[:4])
	c.p = c.p[4:]
	return v, nil
}

func (c *Cursor) I32() (int32, error) {
	u, err := c.U32()
	return int32(u), err
}

func (c *Cursor) String() (string, error) {
	n, err := c.U32()
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	padded := int((n + 3) &^ 3)
	if len(c.p) < padded {
		return "", io.ErrUnexpectedEOF
	}
	s := c.p[:n]
	if n > 0 && s[n-1] == 0 {
		s = s[:n-1]
	}
	c.p = c.p[padded:]
	return string(s), nil
}

func (c *Cursor) Array() ([]byte, error) {
	n, err := c.U32()
	if err != nil {
		return nil, err
	}
	padded := int((n + 3) &^ 3)
	if len(c.p) < padded {
		return nil, io.ErrUnexpectedEOF
	}
	out := append([]byte(nil), c.p[:n]...)
	c.p = c.p[padded:]
	return out, nil
}

func (c *Cursor) FD() (int, error) {
	if c.take != nil {
		return c.take()
	}
	if c.fi >= len(c.fds) {
		return -1, errors.New("wayland: missing fd")
	}
	fd := c.fds[c.fi]
	c.fi++
	return fd, nil
}

func PutU32(p []byte, v uint32) []byte {
	var b [4]byte
	le.PutUint32(b[:], v)
	return append(p, b[:]...)
}

func PutI32(p []byte, v int32) []byte { return PutU32(p, uint32(v)) }

func PutString(p []byte, s string) []byte {
	n := len(s) + 1
	p = PutU32(p, uint32(n))
	p = append(p, s...)
	p = append(p, 0)
	for len(p)%4 != 0 {
		p = append(p, 0)
	}
	return p
}

func PutArray(p []byte, data []byte) []byte {
	p = PutU32(p, uint32(len(data)))
	p = append(p, data...)
	for len(p)%4 != 0 {
		p = append(p, 0)
	}
	return p
}
