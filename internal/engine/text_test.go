package engine

import "testing"

func TestTextWidthScale(t *testing.T) {
	if TextWidth("", 2) != 0 {
		t.Fatal("empty")
	}
	// one glyph = 5*scale
	if TextWidth("A", 1) != 5 {
		t.Fatalf("A %d", TextWidth("A", 1))
	}
	if TextWidth("AB", 2) != 2*6*2-2 {
		t.Fatalf("AB %d", TextWidth("AB", 2))
	}
}

func TestDrawTextPaintsGlyph(t *testing.T) {
	const w, h, stride = 20, 10, 80
	dst := make([]byte, stride*h)
	DrawText(dst, stride, w, h, 0, 0, "I", 0xffffffff, 1)
	var n int
	for i := 0; i < len(dst); i += 4 {
		if dst[i] != 0 {
			n++
		}
	}
	if n < 7 {
		t.Fatalf("expected I pixels, got %d", n)
	}
}

func TestDrawTextUnknownIsBox(t *testing.T) {
	const w, h, stride = 16, 10, 64
	dst := make([]byte, stride*h)
	DrawText(dst, stride, w, h, 0, 0, "@", 0xffffffff, 1)
	// top-left of the box glyph is on
	if dst[0] == 0 {
		t.Fatal("expected fallback box")
	}
}
