package decorations

import "testing"

func TestDrawPointerMarksPixels(t *testing.T) {
	const w, h, stride = 32, 32, 128
	dst := make([]byte, stride*h)
	DrawPointer(dst, stride, w, h, 2, 2)
	var n int
	for _, b := range dst {
		if b != 0 {
			n++
		}
	}
	if n < 16 {
		t.Fatalf("expected a visible arrow, marked %d bytes", n)
	}
}

func TestOverlayCursorTextShape(t *testing.T) {
	const w, h, stride = 32, 32, 128
	dst := make([]byte, stride*h)
	OverlayCursor(dst, stride, w, h, 4, 4, 0, 0, nil, 0, 0, 0, CursorShapeText)
	var n int
	for _, b := range dst {
		if b != 0 {
			n++
		}
	}
	if n < 8 {
		t.Fatalf("expected I-beam pixels, marked %d bytes", n)
	}
}

func TestOverlayCursorSkipsTransparent(t *testing.T) {
	const w, h, stride = 8, 8, 32
	dst := make([]byte, stride*h)
	src := []byte{1, 2, 3, 0, 9, 8, 7, 255}
	OverlayCursor(dst, stride, w, h, 0, 0, 0, 0, src, 2, 1, 8, 1)
	if dst[0] != 0 || dst[1] != 0 {
		t.Fatalf("transparent pixel written: %v", dst[:4])
	}
	if dst[4] != 9 || dst[5] != 8 || dst[6] != 7 {
		t.Fatalf("opaque pixel %v", dst[4:8])
	}
}
