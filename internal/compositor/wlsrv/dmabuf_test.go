package wlsrv

import (
	"io"
	"log"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestValidateDMABuf(t *testing.T) {
	if err := ValidateDMABuf(0, 10, drmFormatXRGB8888, 1); err == nil {
		t.Fatal("empty size")
	}
	if err := ValidateDMABuf(10, 10, drmFormatXRGB8888, 0); err == nil {
		t.Fatal("no planes")
	}
	if err := ValidateDMABuf(10, 10, 0xdeadbeef, 1); err == nil {
		t.Fatal("bad fourcc")
	}
	if err := ValidateDMABuf(10, 10, drmFormatARGB8888, 1); err != nil {
		t.Fatal(err)
	}
	if !GPUSampleFourcc(drmFormatXRGB8888) || GPUSampleFourcc(drmFormatABGR8888) {
		t.Fatal("GPU sample is ARGB/XRGB only (BGRA swapchain)")
	}
}

func TestResolveDmaRefuseNoImportTiled(t *testing.T) {
	c := &Client{srv: &Server{log: log.New(io.Discard, "", 0)}}
	d := &dmaBuf{w: 8, h: 8, fourcc: drmFormatXRGB8888, modifier: 0x0100000000000009, planes: []dmaPlane{{fd: -1, stride: 32}}}
	if err := c.resolveDma(d); err == nil {
		t.Fatal("tiled without import must refuse")
	}
}

func TestResolveDmaRefuseBadFourcc(t *testing.T) {
	c := &Client{srv: &Server{}}
	d := &dmaBuf{w: 8, h: 8, fourcc: 0x11111111, planes: []dmaPlane{{fd: 3, stride: 32}}}
	if err := ValidateDMABuf(d.w, d.h, d.fourcc, len(d.planes)); err == nil {
		t.Fatal("expected fourcc refuse")
	}
	if err := c.resolveDma(d); err == nil {
		t.Fatal("resolve must refuse")
	}
}

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
