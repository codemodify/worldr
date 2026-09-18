//go:build linux && cgo

package apps

import (
	"encoding/binary"
	"testing"
)

func syncChild(c *cursorWireClient, parent uint32, x, y, w, h int, color uint32) (surface, sub uint32) {
	surface = c.cursor()
	sub = c.id()
	c.send(c.subcompositor, 1, -1, sub, surface, parent)
	c.send(sub, 1, -1, x, y)
	buffer := c.image(w, h, color)
	c.send(surface, 1, -1, buffer, 0, 0)
	c.send(surface, 6, -1)
	return
}
func clientPixel(t *testing.T, s *Server, x, y int, want [3]byte) {
	t.Helper()
	surfaces, err := s.Poll()
	if err != nil || len(surfaces) != 1 {
		t.Fatal(surfaces, err)
	}
	v := surfaces[0]
	got := v.Pixels[(y*v.Width+x)*4:][:3]
	if got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("pixel(%d,%d)=%v want%v", x, y, got, want)
	}
}
func pointerObject(c *cursorWireClient, root uint64, x, y float32) uint32 {
	c.t.Helper()
	c.s.Pointer(0, 0, 0)
	c.s.Pointer(root, x, y)
	c.roundtrip()
	for i := len(c.events) - 1; i >= 0; i-- {
		e := c.events[i]
		if e.id == c.pointer && e.opcode == 0 {
			return binary.LittleEndian.Uint32(e.data[4:])
		}
	}
	c.t.Fatal("pointer enter missing")
	return 0
}
func callbackDone(c *cursorWireClient, id uint32) bool {
	for _, e := range c.events {
		if e.id == id && e.opcode == 0 {
			return true
		}
	}
	return false
}

func TestSynchronizedChildKeepsPixelsBoundsPositionAndStackUntilParent(t *testing.T) {
	s, err := Open(320, 240)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := newCursorWireClient(t, s)
	a, subA := syncChild(c, c.surface, 2, 2, 8, 8, 0xffff0000)
	b, subB := syncChild(c, c.surface, 22, 2, 4, 4, 0xff0000ff)
	c.send(subB, 5, -1)
	c.send(c.surface, 6, -1)
	c.roundtrip()
	root := cursorMappedID(t, s, 0)
	viewporter := bindWireGlobal(c, "wp_viewporter", 1)
	viewport := c.id()
	c.send(viewporter, 1, -1, viewport, a)
	c.send(viewport, 2, -1, 16, 8)
	c.send(subA, 1, -1, 6, 2)
	c.send(subA, 3, -1, c.surface)
	buffer := c.image(8, 8, 0xff00ff00)
	frame := c.id()
	c.send(a, 3, -1, frame)
	c.send(a, 1, -1, buffer, 0, 0)
	c.send(a, 6, -1)
	c.send(buffer, 0, -1)
	c.roundtrip()
	clientPixel(t, s, 3, 3, [3]byte{255, 0, 0})
	if got := pointerObject(c, root, 12, 4); got != c.surface {
		t.Fatal("cached size/position reached hit testing", got)
	}
	if callbackDone(c, frame) {
		t.Fatal("cached frame acknowledged before presentation")
	}
	// An independently updated sibling must not publish the cached child.
	replacement := c.image(4, 4, 0xffffffff)
	c.send(b, 1, -1, replacement, 0, 0)
	c.send(b, 6, -1)
	c.roundtrip()
	clientPixel(t, s, 3, 3, [3]byte{255, 0, 0})
	clientPixel(t, s, 23, 3, [3]byte{255, 255, 255})
	if got := pointerObject(c, root, 12, 4); got != c.surface {
		t.Fatal("sibling commit published child input bounds", got)
	}
	c.send(c.surface, 6, -1)
	c.roundtrip()
	clientPixel(t, s, 8, 3, [3]byte{0x18, 0x30, 0x50}) // Newly applied child is below opaque parent.
	if !callbackDone(c, frame) {
		t.Fatal("parent commit did not acknowledge cached frame")
	}
	c.send(subA, 2, -1, c.surface)
	c.roundtrip()
	clientPixel(t, s, 8, 3, [3]byte{0x18, 0x30, 0x50})
	c.send(c.surface, 6, -1)
	c.roundtrip()
	clientPixel(t, s, 8, 3, [3]byte{0, 255, 0})
	if got := pointerObject(c, root, 18, 4); got != a {
		t.Fatal("applied viewport not reflected in pointer bounds", got)
	}
	// A null attach is cached too; active input remains until its parent applies.
	c.send(a, 1, -1, uint32(0), 0, 0)
	c.send(a, 6, -1)
	c.roundtrip()
	if got := pointerObject(c, root, 18, 4); got != a {
		t.Fatal("cached unmap withdrew input early", got)
	}
	c.send(c.surface, 6, -1)
	c.roundtrip()
	if got := pointerObject(c, root, 18, 4); got != c.surface {
		t.Fatal("applied unmap retained child hit region", got)
	}
}

func TestNestedSynchronizedCommitCapturesExactChildUpdate(t *testing.T) {
	s, err := Open(320, 240)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := newCursorWireClient(t, s)
	a, subA := syncChild(c, c.surface, 2, 2, 20, 16, 0xffff0000)
	g, subG := syncChild(c, a, 1, 1, 4, 4, 0xff00ff00)
	first := c.id()
	c.send(g, 3, -1, first)
	c.send(g, 6, -1)
	c.send(a, 6, -1) // Captures green, before the later blue child commit.
	blue := c.image(4, 4, 0xff0000ff)
	second := c.id()
	c.send(g, 3, -1, second)
	c.send(g, 1, -1, blue, 0, 0)
	c.send(g, 6, -1)
	c.send(c.surface, 6, -1)
	c.roundtrip()
	clientPixel(t, s, 4, 4, [3]byte{0, 255, 0})
	if !callbackDone(c, first) || callbackDone(c, second) {
		t.Fatal("nested parent acknowledged wrong child generation")
	}
	c.send(c.surface, 6, -1)
	c.roundtrip()
	clientPixel(t, s, 4, 4, [3]byte{0, 255, 0})
	c.send(a, 6, -1)
	c.send(c.surface, 6, -1)
	c.roundtrip()
	clientPixel(t, s, 4, 4, [3]byte{0, 0, 255})
	if !callbackDone(c, second) {
		t.Fatal("new captured child frame was not completed")
	}
	// Desync is inherited: a locally desynchronized grandchild still waits
	// while its ancestor is synchronized, then publishes upon that transition.
	c.send(subG, 5, -1)
	green := c.image(4, 4, 0xff00ff00)
	c.send(g, 1, -1, green, 0, 0)
	c.send(g, 6, -1)
	c.roundtrip()
	clientPixel(t, s, 4, 4, [3]byte{0, 0, 255})
	c.send(subA, 5, -1)
	c.roundtrip()
	clientPixel(t, s, 4, 4, [3]byte{0, 255, 0})
	// Position requests belong to the immediate parent's commit even in desync.
	c.send(subG, 1, -1, 8, 1)
	c.send(g, 6, -1)
	c.roundtrip()
	clientPixel(t, s, 4, 4, [3]byte{0, 255, 0})
	c.send(a, 6, -1)
	c.roundtrip()
	clientPixel(t, s, 4, 4, [3]byte{255, 0, 0})
	clientPixel(t, s, 11, 4, [3]byte{0, 255, 0})
}

func TestParentStackingIsAppliedAtomicallyAndNearestParent(t *testing.T) {
	s, err := Open(320, 240)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := newCursorWireClient(t, s)
	_, subA := syncChild(c, c.surface, 2, 2, 8, 8, 0xffff0000)
	b, subB := syncChild(c, c.surface, 2, 2, 8, 8, 0xff00ff00)
	c.send(c.surface, 6, -1)
	c.roundtrip()
	clientPixel(t, s, 3, 3, [3]byte{0, 255, 0})
	c.send(subA, 2, -1, b)
	c.roundtrip()
	clientPixel(t, s, 3, 3, [3]byte{0, 255, 0})
	c.send(c.surface, 6, -1)
	c.roundtrip()
	clientPixel(t, s, 3, 3, [3]byte{255, 0, 0})
	c.send(subA, 2, -1, c.surface)
	c.send(c.surface, 6, -1)
	c.roundtrip()
	clientPixel(t, s, 3, 3, [3]byte{0, 255, 0})
	c.send(subB, 0, -1)
	c.roundtrip()
	clientPixel(t, s, 3, 3, [3]byte{255, 0, 0})
}

func TestSynchronizedInputRegionSnapshotSurvivesRegionMutationAndDestroy(t *testing.T) {
	s, err := Open(320, 240)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := newCursorWireClient(t, s)
	child, _ := syncChild(c, c.surface, 2, 2, 16, 12, 0xff00ff00)
	c.send(c.surface, 6, -1)
	c.roundtrip()
	root := cursorMappedID(t, s, 0)
	region := c.id()
	c.send(c.compositor, 1, -1, region)
	c.send(region, 1, -1, 0, 0, 16, 12)
	c.send(region, 2, -1, 4, 0, 8, 12)
	c.send(child, 5, -1, region)
	c.send(child, 6, -1)
	c.send(region, 1, -1, 4, 0, 8, 12)
	c.send(region, 0, -1)
	c.roundtrip()
	if got := pointerObject(c, root, 8, 4); got != child {
		t.Fatal("cached input subtraction applied before parent", got)
	}
	c.send(c.surface, 6, -1)
	c.roundtrip()
	if got := pointerObject(c, root, 8, 4); got != c.surface {
		t.Fatal("region snapshot changed after resource mutation/destruction", got)
	}
	if got := pointerObject(c, root, 3, 4); got != child {
		t.Fatal("region union excluded left strip", got)
	}
	c.send(child, 5, -1, uint32(0))
	c.send(child, 6, -1)
	c.send(c.surface, 6, -1)
	c.roundtrip()
	if got := pointerObject(c, root, 8, 4); got != child {
		t.Fatal("null region did not restore default input", got)
	}
}
