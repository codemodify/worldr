package wlsrv

import (
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestMmapLinear(t *testing.T) {
	const w, h = 4, 2
	stride := w * 4
	size := stride * h
	fd, err := unix.MemfdCreate("t-dma", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	if err := unix.Ftruncate(fd, int64(size)); err != nil {
		t.Fatal(err)
	}
	mem, err := syscall.Mmap(fd, 0, size, syscall.PROT_WRITE|syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		t.Fatal(err)
	}
	mem[0], mem[1], mem[2], mem[3] = 1, 2, 3, 4
	_ = syscall.Munmap(mem)

	d := &dmaBuf{
		w: w, h: h, fourcc: drmFormatXRGB8888, modifier: drmModLinear,
		planes: []dmaPlane{{fd: fd, offset: 0, stride: uint32(stride)}},
	}
	pix, st, err := mmapLinear(d)
	if err != nil {
		t.Fatal(err)
	}
	if st != stride || pix[0] != 1 || pix[2] != 3 {
		t.Fatalf("stride=%d pix=%v", st, pix[:4])
	}
}
