package engine

import "testing"

func TestFillAndBlit(t *testing.T) {
	const w, h, stride = 8, 4, 32
	dst := make([]byte, stride*h)
	FillBGRA(dst, stride, w, h, 0xff112233)
	if dst[0] != 0x33 || dst[1] != 0x22 || dst[2] != 0x11 {
		t.Fatalf("fill bytes %x %x %x", dst[0], dst[1], dst[2])
	}
	src := []byte{9, 8, 7, 6}
	BlitBGRA(dst, stride, w, h, 1, 1, src, 4, 1, 1)
	i := 1*stride + 1*4
	if dst[i] != 9 {
		t.Fatalf("blit %v", dst[i:i+4])
	}
}

func TestBlitScaledAlpha(t *testing.T) {
	const w, h, stride = 4, 2, 16
	dst := make([]byte, stride*h)
	FillBGRA(dst, stride, w, h, 0xff000000)
	src := []byte{0xff, 0xff, 0xff, 0xff}
	BlitBGRAScaledAlpha(dst, stride, w, h, 0, 0, 1, 1, src, 4, 1, 1, 0.5)
	if dst[0] < 0x70 || dst[0] > 0x90 {
		t.Fatalf("blend %x", dst[0])
	}
	BlitBGRAScaledAlpha(dst, stride, w, h, 0, 0, 1, 1, src, 4, 1, 1, 0)
	// alpha 0 is a no-op; previous blend remains
}
