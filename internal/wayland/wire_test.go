package wayland

import "testing"

func TestEncodeDecodeHeader(t *testing.T) {
	p := PutU32(nil, 7)
	msg := Encode(1, 1, p)
	obj, op, size, err := DecodeHeader(msg)
	if err != nil {
		t.Fatal(err)
	}
	if obj != 1 || op != 1 || size != 12 {
		t.Fatalf("obj=%d op=%d size=%d", obj, op, size)
	}
	c := NewCursor(msg[8:], nil)
	v, err := c.U32()
	if err != nil || v != 7 {
		t.Fatalf("payload %v %v", v, err)
	}
}

func TestCursorRemaining(t *testing.T) {
	p := PutU32(nil, 9)
	c := NewCursor(p, nil)
	if c.Remaining() != 4 {
		t.Fatalf("before %d", c.Remaining())
	}
	_, _ = c.U32()
	if c.Remaining() != 0 {
		t.Fatalf("after %d", c.Remaining())
	}
}

func TestTakeFDOrder(t *testing.T) {
	r := &Reader{fds: []int{11, 22}}
	a, err := r.TakeFD()
	if err != nil || a != 11 {
		t.Fatalf("first %d %v", a, err)
	}
	b, err := r.TakeFD()
	if err != nil || b != 22 {
		t.Fatalf("second %d %v", b, err)
	}
	if _, err := r.TakeFD(); err == nil {
		t.Fatal("expected missing fd")
	}
	c := NewCursor(nil, nil)
	c.SetTakeFD(func() (int, error) { return 7, nil })
	fd, err := c.FD()
	if err != nil || fd != 7 {
		t.Fatalf("cursor take %d %v", fd, err)
	}
}

func TestPutStringRoundtrip(t *testing.T) {
	p := PutString(nil, "wl_compositor")
	c := NewCursor(p, nil)
	s, err := c.String()
	if err != nil || s != "wl_compositor" {
		t.Fatalf("got %q %v", s, err)
	}
}
