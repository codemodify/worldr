//go:build linux && cgo

package apps

import (
	"encoding/binary"
	"os"
	"testing"
)

type scaleWireClient struct {
	*cursorWireClient
	viewporter, fractionalManager, viewport, fractional uint32
}

func newScaleWireClient(t *testing.T, s *Server) *scaleWireClient {
	t.Helper()
	wire := newCursorWireClient(t, s)
	c := &scaleWireClient{cursorWireClient: wire}
	registry := c.id()
	c.send(1, 1, -1, registry)
	c.roundtrip()
	for _, e := range c.events {
		if e.id != registry || e.opcode != 0 {
			continue
		}
		n := int(binary.LittleEndian.Uint32(e.data[4:]))
		name := string(e.data[8 : 8+n-1])
		var target *uint32
		if name == "wp_viewporter" {
			target = &c.viewporter
		}
		if name == "wp_fractional_scale_manager_v1" {
			target = &c.fractionalManager
		}
		if target != nil {
			*target = c.id()
			c.send(registry, 0, -1, binary.LittleEndian.Uint32(e.data), name, uint32(1), *target)
		}
	}
	if c.viewporter == 0 || c.fractionalManager == 0 {
		t.Fatal("missing scale globals")
	}
	c.viewport = c.id()
	c.send(c.viewporter, 1, -1, c.viewport, c.surface)
	c.fractional = c.id()
	c.send(c.fractionalManager, 1, -1, c.fractional, c.surface)
	c.roundtrip()
	return c
}
func (c *scaleWireClient) preference() uint32 {
	for i := len(c.events) - 1; i >= 0; i-- {
		e := c.events[i]
		if e.id == c.fractional && e.opcode == 0 {
			return binary.LittleEndian.Uint32(e.data)
		}
	}
	c.t.Fatal("no preferred scale event")
	return 0
}
func (c *scaleWireClient) surfaceState() Surface {
	c.t.Helper()
	all, err := c.s.Poll()
	if err != nil {
		c.t.Fatal(err)
	}
	if len(all) != 1 {
		c.t.Fatalf("expected one mapped root, got%d", len(all))
	}
	return all[0]
}
func (c *scaleWireClient) pattern(w, h int) uint32 {
	c.t.Helper()
	f, err := os.CreateTemp(c.t.TempDir(), "scale-pattern")
	if err != nil {
		c.t.Fatal(err)
	}
	defer f.Close()
	pixels := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			q := uint32(0xffff0000)
			if x >= w/2 {
				q = 0xff00ff00
			}
			binary.LittleEndian.PutUint32(pixels[(y*w+x)*4:], q)
		}
	}
	if _, err := f.Write(pixels); err != nil {
		c.t.Fatal(err)
	}
	pool, buffer := c.id(), c.id()
	c.send(c.shm, 0, int(f.Fd()), pool, len(pixels))
	c.send(pool, 0, -1, buffer, 0, w, h, w*4, 0)
	c.send(pool, 1, -1)
	return buffer
}
func TestFractionalPreferenceAndViewportCommitCropDestroy(t *testing.T) {
	s, err := Open(32, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetScale120(180); err != nil {
		t.Fatal(err)
	}
	c := newScaleWireClient(t, s)
	if c.preference() != 180 {
		t.Fatal("initial 1.5x preference missing")
	}
	c.send(c.viewport, 2, -1, 32, 24)
	buffer := c.pattern(48, 36)
	c.send(c.surface, 1, -1, buffer, 0, 0)
	c.send(c.surface, 6, -1)
	c.roundtrip()
	surface := c.surfaceState()
	if surface.LogicalWidth != 32 || surface.LogicalHeight != 24 {
		t.Fatalf("fractional buffer lost logical extent:%+v", surface)
	}
	if err := s.SetScale120(150); err != nil {
		t.Fatal(err)
	}
	c.roundtrip()
	if c.preference() != 150 {
		t.Fatal("1.25x update missing")
	}
	before := surface.Revision
	c.send(c.viewport, 1, -1, 24*256, 0, 24*256, 36*256)
	c.send(c.viewport, 2, -1, 16, 24)
	c.roundtrip()
	if state := c.surfaceState(); state.Revision != before || state.LogicalWidth != 32 {
		t.Fatal("uncommitted viewport changed live content")
	}
	c.send(c.surface, 6, -1)
	c.roundtrip()
	cropped := c.surfaceState()
	if cropped.LogicalWidth != 16 || cropped.LogicalHeight != 24 {
		t.Fatal("committed crop lost destination")
	}
	if len(cropped.Pixels) < 4 || cropped.Pixels[0] != 0 || cropped.Pixels[1] != 255 {
		t.Fatal("source crop did not select green half", cropped.Pixels[:min(4, len(cropped.Pixels))])
	}
	c.send(c.viewport, 0, -1)
	c.roundtrip()
	if state := c.surfaceState(); state.LogicalWidth != 16 {
		t.Fatal("viewport destruction applied before commit")
	}
	c.send(c.surface, 6, -1)
	c.roundtrip()
	restored := c.surfaceState()
	if restored.LogicalWidth != 48 || restored.LogicalHeight != 36 || len(restored.Pixels) < 4 || restored.Pixels[0] != 255 || restored.Pixels[1] != 0 {
		t.Fatal("viewport destruction did not restore full buffer on commit")
	}
	if err := s.SetScale120(0); err == nil {
		t.Fatal("zero preferred scale accepted")
	}
	if err := s.SetScale120(961); err == nil {
		t.Fatal("unbounded preferred scale accepted")
	}
}
func TestViewportCroppedFractionalChildMapsPointerInLogicalSpace(t *testing.T) {
	s, err := Open(32, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := newScaleWireClient(t, s)
	root := c.surfaceState().ID
	child, sub := c.cursor(), c.id()
	c.send(c.subcompositor, 1, -1, sub, child, c.surface)
	c.send(sub, 1, -1, 5, 6)
	viewport := c.id()
	c.send(c.viewporter, 1, -1, viewport, child)
	c.send(viewport, 1, -1, 6*256, 0, 6*256, 12*256)
	c.send(viewport, 2, -1, 4, 8)
	buffer := c.pattern(12, 12)
	c.send(child, 1, -1, buffer, 0, 0)
	c.send(child, 6, -1)
	c.send(c.surface, 6, -1)
	c.roundtrip()
	check := func(x, y float32, wantSurface uint32, wantX, wantY int) {
		t.Helper()
		from := len(c.events)
		if err := s.Pointer(root, x, y); err != nil {
			t.Fatal(err)
		}
		c.roundtrip()
		for _, e := range c.events[from:] {
			if e.id == c.pointer && e.opcode == 0 {
				if binary.LittleEndian.Uint32(e.data[4:]) != wantSurface || int32(binary.LittleEndian.Uint32(e.data[8:])) != int32(wantX*256) || int32(binary.LittleEndian.Uint32(e.data[12:])) != int32(wantY*256) {
					t.Fatalf("wrong logical pointer target:%x", e.data)
				}
				return
			}
		}
		t.Fatal("pointer enter absent")
	}
	check(7, 9, child, 2, 3)
	check(10, 9, c.surface, 10, 9) // Beyond 4 logical pixels, still inside raw 12px source.
}
func TestViewportProtocolErrorsStayIsolated(t *testing.T) {
	cases := []struct {
		name   string
		act    func(*scaleWireClient)
		object func(*scaleWireClient) uint32
		code   uint32
	}{
		{"duplicate viewport", func(c *scaleWireClient) { c.send(c.viewporter, 1, -1, c.id(), c.surface) }, func(c *scaleWireClient) uint32 { return c.viewporter }, 0},
		{"duplicate fraction", func(c *scaleWireClient) { c.send(c.fractionalManager, 1, -1, c.id(), c.surface) }, func(c *scaleWireClient) uint32 { return c.fractionalManager }, 0},
		{"bad source", func(c *scaleWireClient) { c.send(c.viewport, 1, -1, -1, 0, 256, 256) }, func(c *scaleWireClient) uint32 { return c.viewport }, 0},
		{"fractional source without destination", func(c *scaleWireClient) { c.send(c.viewport, 1, -1, 0, 0, 384, 256); c.send(c.surface, 6, -1) }, func(c *scaleWireClient) uint32 { return c.viewport }, 1},
		{"outside buffer", func(c *scaleWireClient) { c.send(c.viewport, 1, -1, 31*256, 0, 2*256, 256); c.send(c.surface, 6, -1) }, func(c *scaleWireClient) uint32 { return c.viewport }, 2},
		{"defunct surface", func(c *scaleWireClient) {
			c.send(c.top, 0, -1)
			c.send(c.xdg, 0, -1)
			c.send(c.surface, 0, -1)
			c.send(c.viewport, 2, -1, 10, 10)
		}, func(c *scaleWireClient) uint32 { return c.viewport }, 3},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			s, err := Open(32, 24)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			c := newScaleWireClient(t, s)
			healthy := newCursorWireClient(t, s)
			test.act(c)
			if err := c.sync(); err == nil {
				t.Fatal("invalid request accepted")
			}
			found := false
			for _, e := range c.events {
				if e.id == 1 && e.opcode == 0 && len(e.data) >= 8 {
					found = true
					if binary.LittleEndian.Uint32(e.data) != test.object(c) || binary.LittleEndian.Uint32(e.data[4:]) != test.code {
						t.Fatalf("wrong protocol error:%x", e.data)
					}
				}
			}
			if !found {
				t.Fatal("missing protocol error event")
			}
			healthy.roundtrip()
		})
	}
}
