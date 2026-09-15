package wlsrv

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/wayland"
	"golang.org/x/sys/unix"
)

// DRM fourcc / linux-dmabuf.
const (
	drmFormatXRGB8888 = 0x34325258
	drmFormatARGB8888 = 0x34325241
	drmFormatXBGR8888 = 0x34324258
	drmFormatABGR8888 = 0x34324241
	drmModLinear      = 0
	drmModInvalid     = 0x00ffffffffffffff
)

const linuxDmabufVersion = 4

func linuxDmabufAdvertiseVersion() uint32 {
	if _, ok := drmDeviceID(); ok {
		return linuxDmabufVersion
	}
	return 3 // no feedback; Xwayland 23 SEGVs on a zero main_device
}

type dmaPlane struct {
	fd     int
	offset uint32
	stride uint32
}

type dmaBuf struct {
	w, h     int
	fourcc   uint32
	modifier uint64
	planes   []dmaPlane
	pixels   []byte
	stride   int
	gpuSlot  int
	resolved bool
}

// ValidateDMABuf rejects empty / unsupported client buffers before import.
func ValidateDMABuf(w, h int, fourcc uint32, nplanes int) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("empty dmabuf")
	}
	if nplanes < 1 || nplanes > 4 {
		return fmt.Errorf("dmabuf plane count %d", nplanes)
	}
	if !SupportedDMABufFourcc(fourcc) {
		return fmt.Errorf("unsupported dmabuf fourcc 0x%x (want ARGB/XRGB/ABGR/XBGR8888)", fourcc)
	}
	return nil
}

// SupportedDMABufFourcc is the 8888 set advertised to clients.
func SupportedDMABufFourcc(fourcc uint32) bool {
	switch fourcc {
	case drmFormatARGB8888, drmFormatXRGB8888, drmFormatABGR8888, drmFormatXBGR8888:
		return true
	}
	return false
}

// GPUSampleFourcc is true when the compositor can blit the import onto a BGRA swapchain.
func GPUSampleFourcc(fourcc uint32) bool {
	return fourcc == drmFormatARGB8888 || fourcc == drmFormatXRGB8888
}

func dmaHasScan(d *dmaBuf) bool {
	return d != nil && len(d.planes) >= 1 && d.planes[0].fd > 0 && GPUSampleFourcc(d.fourcc)
}

func applyDmaScan(a *engine.Actor, d *dmaBuf) {
	if a == nil || !dmaHasScan(d) {
		return
	}
	p := d.planes[0]
	a.ScanFD = p.fd
	a.ScanFourcc = d.fourcc
	a.ScanMod = d.modifier
	a.ScanOff = p.offset
	a.ScanStride = p.stride
}

func (d *dmaBuf) closeFDs() {
	for i := range d.planes {
		if d.planes[i].fd > 0 {
			_ = syscall.Close(d.planes[i].fd)
			d.planes[i].fd = -1
		}
	}
}

func (c *Client) advertiseLinuxDmabuf(id uint32) error {
	// v3-compatible format+modifier events (older clients).
	for _, fourcc := range []uint32{drmFormatARGB8888, drmFormatXRGB8888, drmFormatABGR8888, drmFormatXBGR8888} {
		if err := c.send(id, 0, wayland.PutU32(nil, fourcc), nil); err != nil {
			return err
		}
		p := wayland.PutU32(nil, fourcc)
		p = wayland.PutU32(p, 0)
		p = wayland.PutU32(p, 0) // LINEAR
		if err := c.send(id, 1, p, nil); err != nil {
			return err
		}
	}
	return nil
}

func drmDeviceID() ([]byte, bool) {
	matches, _ := filepath.Glob("/dev/dri/card*")
	if len(matches) == 0 {
		matches, _ = filepath.Glob("/dev/dri/renderD*")
	}
	for _, p := range matches {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		sys, ok := st.Sys().(*syscall.Stat_t)
		if !ok || sys.Rdev == 0 {
			continue
		}
		dev := make([]byte, 8)
		binary.LittleEndian.PutUint64(dev, uint64(sys.Rdev))
		return dev, true
	}
	return nil, false
}

func (c *Client) sendDmabufFeedback(id uint32) error {
	// Intel Arrow Lake / Mesa often uses 4-tiled; advertise common modifiers
	// so GPU clients will create importable buffers (import still goes through Vulkan).
	intelMods := []uint64{
		0x0100000000000001, // I915_FORMAT_MOD_X_TILED
		0x0100000000000002, // I915_FORMAT_MOD_Y_TILED
		0x0100000000000004, // I915_FORMAT_MOD_Yf_TILED
		0x0100000000000009, // I915_FORMAT_MOD_4_TILED
	}
	table := make([]byte, 0, 16*16)
	add := func(format uint32, mod uint64) {
		var e [16]byte
		binary.LittleEndian.PutUint32(e[0:4], format)
		binary.LittleEndian.PutUint64(e[8:16], mod)
		table = append(table, e[:]...)
	}
	add(drmFormatARGB8888, drmModLinear)
	add(drmFormatXRGB8888, drmModLinear)
	add(drmFormatABGR8888, drmModLinear)
	add(drmFormatXBGR8888, drmModLinear)
	for _, m := range intelMods {
		add(drmFormatARGB8888, m)
		add(drmFormatXRGB8888, m)
	}
	fd, err := unix.MemfdCreate("worldr-dmabuf-formats", 0)
	if err != nil {
		return err
	}
	if err := unix.Ftruncate(fd, int64(len(table))); err != nil {
		_ = syscall.Close(fd)
		return err
	}
	mem, err := syscall.Mmap(fd, 0, len(table), syscall.PROT_WRITE|syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		_ = syscall.Close(fd)
		return err
	}
	copy(mem, table)
	_ = syscall.Munmap(mem)

	// format_table
	p := wayland.PutU32(nil, uint32(len(table)))
	if err := c.send(id, 1, p, []int{fd}); err != nil {
		_ = syscall.Close(fd)
		return err
	}
	_ = syscall.Close(fd) // already sent

	dev, ok := drmDeviceID()
	if !ok {
		// Zero main_device makes Xwayland 23 crash ("Failed to fetch DRM device").
		return nil
	}
	// main_device
	if err := c.send(id, 2, wayland.PutArray(nil, dev), nil); err != nil {
		return err
	}
	// tranche_target_device
	if err := c.send(id, 4, wayland.PutArray(nil, dev), nil); err != nil {
		return err
	}
	// tranche_formats: indices 0..n-1 as uint16
	n := len(table) / 16
	idx := make([]byte, n*2)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(idx[i*2:], uint16(i))
	}
	if err := c.send(id, 5, wayland.PutArray(nil, idx), nil); err != nil {
		return err
	}
	// tranche_flags = 0
	if err := c.send(id, 6, wayland.PutU32(nil, 0), nil); err != nil {
		return err
	}
	if err := c.send(id, 3, nil, nil); err != nil { // tranche_done
		return err
	}
	return c.send(id, 0, nil, nil) // done
}

func (c *Client) reqLinuxDmabuf(o *object, op uint16, cur *wayland.Cursor) error {
	switch op {
	case 0: // destroy
		delete(c.objs, o.id)
	case 1: // create_params
		id, err := cur.U32()
		if err != nil {
			return err
		}
		c.objs[id] = &object{id: id, kind: kindDmaParams, dma: &dmaBuf{}}
	case 2, 3: // get_default_feedback / get_surface_feedback
		id, err := cur.U32()
		if err != nil {
			return err
		}
		if op == 3 {
			_, _ = cur.U32() // surface
		}
		c.objs[id] = &object{id: id, kind: kindDmaFeedback}
		return c.sendDmabufFeedback(id)
	}
	return nil
}

func (c *Client) reqDmaParams(o *object, op uint16, cur *wayland.Cursor) error {
	d := o.dma
	if d == nil {
		d = &dmaBuf{}
		o.dma = d
	}
	switch op {
	case 0: // destroy
		d.closeFDs()
		delete(c.objs, o.id)
	case 1: // add
		fd, err := cur.FD()
		if err != nil {
			return err
		}
		plane, _ := cur.U32()
		off, _ := cur.U32()
		stride, _ := cur.U32()
		hi, _ := cur.U32()
		lo, _ := cur.U32()
		for len(d.planes) <= int(plane) {
			d.planes = append(d.planes, dmaPlane{fd: -1})
		}
		if d.planes[plane].fd > 0 {
			_ = syscall.Close(d.planes[plane].fd)
		}
		d.planes[plane] = dmaPlane{fd: fd, offset: off, stride: stride}
		d.modifier = uint64(hi)<<32 | uint64(lo)
		_ = plane
	case 2: // create (async)
		w, _ := cur.I32()
		h, _ := cur.I32()
		fourcc, _ := cur.U32()
		_, _ = cur.U32()
		d.w, d.h, d.fourcc = int(w), int(h), fourcc
		id := c.allocServerID()
		if err := c.finishDmaBuffer(id, d); err != nil {
			c.srv.log.Printf("dmabuf create failed: %v", err)
			return c.send(o.id, 1, nil, nil) // failed
		}
		return c.send(o.id, 0, wayland.PutU32(nil, id), nil) // created
	case 3: // create_immed
		id, err := cur.U32()
		if err != nil {
			return err
		}
		w, _ := cur.I32()
		h, _ := cur.I32()
		fourcc, _ := cur.U32()
		_, _ = cur.U32()
		d.w, d.h, d.fourcc = int(w), int(h), fourcc
		if err := c.finishDmaBuffer(id, d); err != nil {
			// Spec wants a fatal error; Chromium/Brave treat that as a
			// GPU-process crash. Keep the connection with a black placeholder
			// so the browser can finish bring-up (shm/SwiftShader path).
			c.srv.log.Printf("dmabuf create_immed failed (placeholder): %v", err)
			c.installDmaPlaceholder(id, int(w), int(h), fourcc)
			return nil
		}
	}
	return nil
}

func (c *Client) installDmaPlaceholder(id uint32, w, h int, fourcc uint32) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	c.objs[id] = &object{id: id, kind: kindBuffer, dma: &dmaBuf{
		w: w, h: h, fourcc: fourcc,
		pixels:   make([]byte, w*h*4),
		stride:   w * 4,
		resolved: true,
	}}
}

func (c *Client) allocServerID() uint32 {
	if c.serverID < 0xff000000 {
		c.serverID = 0xff000000
	}
	c.serverID++
	return c.serverID
}

func (c *Client) finishDmaBuffer(id uint32, d *dmaBuf) error {
	cp := *d
	cp.planes = append([]dmaPlane(nil), d.planes...)
	// do not close fds on the original until copied; finish owns them
	d.planes = nil
	if err := c.resolveDma(&cp); err != nil {
		cp.closeFDs()
		return err
	}
	c.objs[id] = &object{id: id, kind: kindBuffer, dma: &cp}
	return nil
}

func (c *Client) resolveDma(d *dmaBuf) error {
	if d == nil {
		return fmt.Errorf("empty dmabuf")
	}
	if d.resolved {
		return nil
	}
	if err := ValidateDMABuf(d.w, d.h, d.fourcc, len(d.planes)); err != nil {
		return err
	}
	if c == nil || c.srv == nil {
		return fmt.Errorf("no compositor for dmabuf import")
	}
	linear := d.modifier == drmModLinear || d.modifier == 0 || d.modifier == drmModInvalid
	if linear && len(d.planes) >= 1 && d.planes[0].fd > 0 && d.planes[0].stride > 0 {
		if pix, stride, err := mmapLinear(d); err == nil {
			d.pixels, d.stride = pix, stride
		}
	}
	planes := make([]DMABufPlane, len(d.planes))
	for i, p := range d.planes {
		planes[i] = DMABufPlane{FD: p.fd, Offset: p.offset, Stride: p.stride}
	}
	if gpu, ok := c.srv.Import.(DMABufGPU); ok && gpu.CanGPUComposite() && GPUSampleFourcc(d.fourcc) {
		slot, err := gpu.RetainDMABuf(uint32(d.w), uint32(d.h), d.fourcc, d.modifier, planes)
		if err == nil && slot > 0 {
			d.gpuSlot = slot
			d.resolved = true
			return nil
		}
		if d.pixels == nil && err != nil {
			c.srv.log.Printf("dmabuf retain failed, trying readback: %v", err)
		}
	}
	if d.pixels != nil {
		d.resolved = true
		return nil
	}
	if c.srv.Import != nil {
		pix, stride, err := c.srv.Import.ImportDMABuf(uint32(d.w), uint32(d.h), d.fourcc, d.modifier, planes)
		if err != nil {
			return err
		}
		d.pixels, d.stride = pix, stride
		d.resolved = true
		return nil
	}
	// No CPU pixels / GPU slot: keep the fd so DRM primary scanout can
	// AddFB2 a fullscreen tiled buffer (Intel first; NVIDIA/AMD try).
	if GPUSampleFourcc(d.fourcc) && len(d.planes) >= 1 && d.planes[0].fd > 0 {
		d.resolved = true
		return nil
	}
	return fmt.Errorf("no Vulkan dmabuf import (tiled buffer, shm fallback not possible)")
}

func (c *Client) releaseDma(d *dmaBuf) {
	if d == nil || d.gpuSlot <= 0 || c.srv == nil {
		return
	}
	if gpu, ok := c.srv.Import.(DMABufGPU); ok {
		gpu.ReleaseDMABuf(d.gpuSlot)
	}
	d.gpuSlot = 0
}

func mmapLinear(d *dmaBuf) ([]byte, int, error) {
	p := d.planes[0]
	stride := int(p.stride)
	if stride < d.w*4 {
		stride = d.w * 4
	}
	size := int(p.offset) + stride*d.h
	if size < stride*d.h {
		return nil, 0, fmt.Errorf("dmabuf size")
	}
	mem, err := syscall.Mmap(p.fd, 0, size, syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, 0, err
	}
	defer syscall.Munmap(mem)
	out := make([]byte, d.w*4*d.h)
	src := mem[p.offset:]
	swizzle := d.fourcc == drmFormatXBGR8888 || d.fourcc == drmFormatABGR8888
	for y := 0; y < d.h; y++ {
		row := src[y*int(p.stride):]
		dst := out[y*d.w*4:]
		if !swizzle {
			copy(dst, row[:d.w*4])
			continue
		}
		for x := 0; x < d.w; x++ {
			dst[x*4+0] = row[x*4+2]
			dst[x*4+1] = row[x*4+1]
			dst[x*4+2] = row[x*4+0]
			dst[x*4+3] = row[x*4+3]
		}
	}
	return out, d.w * 4, nil
}
