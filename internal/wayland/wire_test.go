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

func TestPutStringRoundtrip(t *testing.T) {
	p := PutString(nil, "wl_compositor")
	c := NewCursor(p, nil)
	s, err := c.String()
	if err != nil || s != "wl_compositor" {
		t.Fatalf("got %q %v", s, err)
	}
}
