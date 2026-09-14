package xwayland

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestPad4(t *testing.T) {
	if pad4(0) != 0 || pad4(1) != 4 || pad4(4) != 4 || pad4(5) != 8 {
		t.Fatalf("pad4")
	}
}

func TestConfigureFromRequestEncodesMaskOrder(t *testing.T) {
	ev := make([]byte, 32)
	ev[0] = xEvConfigureRequest
	binary.LittleEndian.PutUint32(ev[8:], 0x42)
	binary.LittleEndian.PutUint16(ev[16:], 10)
	binary.LittleEndian.PutUint16(ev[18:], 20)
	binary.LittleEndian.PutUint16(ev[20:], 200)
	binary.LittleEndian.PutUint16(ev[22:], 100)
	binary.LittleEndian.PutUint16(ev[26:], xCfgX|xCfgY|xCfgWidth|xCfgHeight)

	rec := &recordConn{}
	xc := &xConn{c: rec, byteOrder: binary.LittleEndian}
	if err := xc.configureFromRequest(ev); err != nil {
		t.Fatal(err)
	}
	if rec.n < 12+16 {
		t.Fatalf("short configure packet %d", rec.n)
	}
	if rec.buf[0] != xOpConfigureWindow {
		t.Fatalf("opcode %d", rec.buf[0])
	}
	mask := binary.LittleEndian.Uint16(rec.buf[8:])
	if mask != xCfgX|xCfgY|xCfgWidth|xCfgHeight {
		t.Fatalf("mask %#x", mask)
	}
	if binary.LittleEndian.Uint32(rec.buf[4:]) != 0x42 {
		t.Fatalf("window")
	}
	if binary.LittleEndian.Uint32(rec.buf[12:]) != 10 {
		t.Fatalf("x")
	}
	if binary.LittleEndian.Uint32(rec.buf[24:]) != 100 {
		t.Fatalf("h")
	}
}

func TestMapWindowPacket(t *testing.T) {
	rec := &recordConn{}
	xc := &xConn{c: rec, byteOrder: binary.LittleEndian}
	if err := xc.mapWindow(0xabc); err != nil {
		t.Fatal(err)
	}
	if rec.n != 8 || rec.buf[0] != xOpMapWindow {
		t.Fatalf("%x", rec.buf)
	}
	if binary.LittleEndian.Uint32(rec.buf[4:]) != 0xabc {
		t.Fatalf("window")
	}
}

type recordConn struct {
	buf []byte
	n   int
}

func (r *recordConn) Read([]byte) (int, error) { return 0, nil }
func (r *recordConn) Write(p []byte) (int, error) {
	r.buf = append(r.buf, p...)
	r.n += len(p)
	return len(p), nil
}
func (r *recordConn) Close() error                     { return nil }
func (r *recordConn) LocalAddr() net.Addr              { return nil }
func (r *recordConn) RemoteAddr() net.Addr             { return nil }
func (r *recordConn) SetDeadline(time.Time) error      { return nil }
func (r *recordConn) SetReadDeadline(time.Time) error  { return nil }
func (r *recordConn) SetWriteDeadline(time.Time) error { return nil }
