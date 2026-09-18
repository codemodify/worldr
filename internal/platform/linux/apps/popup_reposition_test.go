//go:build linux && cgo

package apps

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func popupConfigure(t *testing.T, events []cursorWireEvent, id uint32) (int, int, int, int, int) {
	t.Helper()
	for i, event := range events {
		if event.id != id || event.opcode != 0 || len(event.data) != 16 {
			continue
		}
		return i,
			int(int32(binary.LittleEndian.Uint32(event.data))),
			int(int32(binary.LittleEndian.Uint32(event.data[4:]))),
			int(int32(binary.LittleEndian.Uint32(event.data[8:]))),
			int(int32(binary.LittleEndian.Uint32(event.data[12:])))
	}
	t.Fatalf("no xdg_popup.configure for object %d", id)
	return 0, 0, 0, 0, 0
}

func xdgConfigure(t *testing.T, events []cursorWireEvent, id uint32) (int, uint32) {
	t.Helper()
	for i, event := range events {
		if event.id == id && event.opcode == 0 && len(event.data) == 4 {
			return i, binary.LittleEndian.Uint32(event.data)
		}
	}
	t.Fatalf("no xdg_surface.configure for object %d", id)
	return 0, 0
}

func popupPixel(t *testing.T, s *Server, root uint64, x, y int) []byte {
	t.Helper()
	surfaces, err := s.Poll()
	if err != nil {
		t.Fatal(err)
	}
	for _, surface := range surfaces {
		if surface.ID != root {
			continue
		}
		if x < 0 || y < 0 || x >= surface.Width || y >= surface.Height {
			t.Fatalf("pixel %d,%d outside %dx%d surface", x, y, surface.Width, surface.Height)
		}
		offset := (y*surface.Width + x) * 4
		return surface.Pixels[offset : offset+4]
	}
	t.Fatalf("root surface %d is not mapped", root)
	return nil
}

func lastConfigureSerial(t *testing.T, events []cursorWireEvent, id uint32) uint32 {
	t.Helper()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].id == id && events[i].opcode == 0 && len(events[i].data) == 4 {
			return binary.LittleEndian.Uint32(events[i].data)
		}
	}
	t.Fatalf("no configure serial for xdg_surface %d", id)
	return 0
}

func expectWMBaseError(t *testing.T, c *cursorWireClient, start int, code uint32) {
	t.Helper()
	if err := c.sync(); err == nil {
		t.Fatalf("request requiring xdg_wm_base error %d was accepted", code)
	}
	for _, event := range c.events[start:] {
		if event.id != 1 || event.opcode != 0 || len(event.data) < 8 {
			continue
		}
		object := binary.LittleEndian.Uint32(event.data)
		got := binary.LittleEndian.Uint32(event.data[4:])
		if object != c.wm || got != code {
			t.Fatalf("protocol error object/code = %d/%d, want %d/%d", object, got, c.wm, code)
		}
		return
	}
	t.Fatal("request did not deliver wl_display.error")
}

func TestPopupV3RepositionAndReactiveConstraint(t *testing.T) {
	s, err := Open(32, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := newCursorWireClient(t, s)
	if c.wmVersion != 3 {
		t.Fatalf("xdg_wm_base version = %d, want 3", c.wmVersion)
	}
	root := cursorMappedID(t, s, 0)

	surface, xdg, positioner, popup := c.id(), c.id(), c.id(), c.id()
	c.send(c.compositor, 0, -1, surface)
	c.send(c.wm, 2, -1, xdg, surface)
	c.send(c.wm, 1, -1, positioner)
	c.send(positioner, 1, -1, 8, 6)
	c.send(positioner, 2, -1, 4, 4, 1, 1)
	c.send(positioner, 3, -1, uint32(8)) // bottom-right anchor
	c.send(positioner, 4, -1, uint32(8)) // bottom-right gravity
	c.send(positioner, 5, -1, uint32(3)) // slide on both axes
	c.send(positioner, 7, -1)            // reactive
	c.send(xdg, 2, -1, popup, c.xdg, positioner)
	c.send(positioner, 0, -1) // get_popup must retain a value copy
	c.send(surface, 6, -1)
	start := len(c.events)
	c.roundtrip()
	initialEvents := c.events[start:]
	_, x, y, width, height := popupConfigure(t, initialEvents, popup)
	if x != 5 || y != 5 || width != 8 || height != 6 {
		t.Fatalf("initial popup configure = %d,%d %dx%d", x, y, width, height)
	}
	_, serial := xdgConfigure(t, initialEvents, xdg)
	c.send(xdg, 4, -1, serial)
	popupBuffer := c.image(8, 6, 0xff20d040)
	c.send(surface, 1, -1, popupBuffer, 0, 0)
	c.send(surface, 6, -1)
	c.roundtrip()
	popupColor := []byte{0x20, 0xd0, 0x40, 0xff}
	rootColor := []byte{0x18, 0x30, 0x50, 0xff}
	if got := popupPixel(t, s, root, 5, 5); !bytes.Equal(got, popupColor) {
		t.Fatalf("initial popup pixel = %x", got)
	}
	// The popup follows a committed parent window-geometry origin without a
	// protocol configure because its parent-relative rectangle is unchanged.
	c.send(c.xdg, 3, -1, 2, 3, 30, 21)
	c.send(c.surface, 6, -1)
	start = len(c.events)
	c.roundtrip()
	for _, event := range c.events[start:] {
		if event.id == popup && event.opcode == 0 {
			t.Fatal("geometry-origin rebase emitted a redundant popup configure")
		}
	}
	if got := popupPixel(t, s, root, 5, 5); !bytes.Equal(got, rootColor) {
		t.Fatalf("old popup origin remained painted: %x", got)
	}
	if got := popupPixel(t, s, root, 7, 8); !bytes.Equal(got, popupColor) {
		t.Fatalf("popup did not follow parent geometry origin: %x", got)
	}
	c.send(c.xdg, 3, -1, 0, 0, 32, 24)
	c.send(c.surface, 6, -1)
	start = len(c.events)
	c.roundtrip()
	for _, event := range c.events[start:] {
		if event.id == popup && event.opcode == 0 {
			t.Fatal("geometry-origin reset emitted a redundant popup configure")
		}
	}
	if got := popupPixel(t, s, root, 5, 5); !bytes.Equal(got, popupColor) {
		t.Fatalf("popup did not follow reset parent geometry origin: %x", got)
	}

	// Ask the parent to resize, then use its configure serial and future size
	// to place the popup against those future constraint bounds.
	if err := s.Resize(root, 28, 20); err != nil {
		t.Fatal(err)
	}
	start = len(c.events)
	c.roundtrip()
	parentSerial := lastConfigureSerial(t, c.events[start:], c.xdg)

	positioner = c.id()
	c.send(c.wm, 1, -1, positioner)
	c.send(positioner, 1, -1, 8, 6)
	c.send(positioner, 2, -1, 27, 17, 1, 1)
	c.send(positioner, 3, -1, uint32(8))
	c.send(positioner, 4, -1, uint32(8))
	c.send(positioner, 5, -1, uint32(3))
	c.send(positioner, 7, -1)
	c.send(positioner, 8, -1, 28, 20)
	c.send(positioner, 9, -1, parentSerial)
	const token = uint32(0xcafe)
	c.send(popup, 2, -1, positioner, token)
	c.send(positioner, 0, -1)
	start = len(c.events)
	c.roundtrip()
	repositionEvents := c.events[start:]
	repositionIndex := -1
	for i, event := range repositionEvents {
		if event.id == popup && event.opcode == 2 && len(event.data) == 4 &&
			binary.LittleEndian.Uint32(event.data) == token {
			repositionIndex = i
			break
		}
	}
	if repositionIndex < 0 {
		t.Fatalf("missing repositioned token: %+v", repositionEvents)
	}
	popupIndex, x, y, width, height := popupConfigure(t, repositionEvents, popup)
	xdgIndex, serial := xdgConfigure(t, repositionEvents, xdg)
	if popupIndex != repositionIndex+1 || xdgIndex != popupIndex+1 {
		t.Fatalf("reposition event order: repositioned=%d popup=%d xdg=%d events=%+v", repositionIndex, popupIndex, xdgIndex, repositionEvents)
	}
	if x != 20 || y != 14 || width != 8 || height != 6 {
		t.Fatalf("future-size popup configure = %d,%d %dx%d", x, y, width, height)
	}
	if got := popupPixel(t, s, root, 5, 5); !bytes.Equal(got, popupColor) {
		t.Fatalf("reposition applied before ack: old pixel = %x", got)
	}
	if got := popupPixel(t, s, root, 20, 14); !bytes.Equal(got, rootColor) {
		t.Fatalf("reposition applied before ack: new pixel = %x", got)
	}
	c.send(xdg, 4, -1, serial)
	c.roundtrip()
	if got := popupPixel(t, s, root, 20, 14); !bytes.Equal(got, popupColor) {
		t.Fatalf("acknowledged reposition pixel = %x", got)
	}

	// Commit the anticipated parent size. Because the hinted and actual
	// placements agree, the reactive positioner must not produce a duplicate.
	c.send(c.xdg, 4, -1, parentSerial)
	rootBuffer := c.image(28, 20, 0xff183050)
	c.send(c.xdg, 3, -1, 0, 0, 28, 20)
	c.send(c.surface, 1, -1, rootBuffer, 0, 0)
	c.send(c.surface, 6, -1)
	start = len(c.events)
	c.roundtrip()
	for _, event := range c.events[start:] {
		if event.id == popup && event.opcode == 0 {
			t.Fatal("matching parent-size commit emitted a redundant popup configure")
		}
	}
	if got := popupPixel(t, s, root, 20, 14); !bytes.Equal(got, popupColor) {
		t.Fatalf("popup moved after matching parent commit: %x", got)
	}

	// A later committed parent shrink changes the constraint result. The new
	// placement remains latent until its xdg_surface.configure is acknowledged.
	if err := s.Resize(root, 20, 16); err != nil {
		t.Fatal(err)
	}
	start = len(c.events)
	c.roundtrip()
	parentSerial = lastConfigureSerial(t, c.events[start:], c.xdg)
	c.send(c.xdg, 4, -1, parentSerial)
	rootBuffer = c.image(20, 16, 0xff183050)
	c.send(c.xdg, 3, -1, 0, 0, 20, 16)
	c.send(c.surface, 1, -1, rootBuffer, 0, 0)
	c.send(c.surface, 6, -1)
	start = len(c.events)
	c.roundtrip()
	reactiveEvents := c.events[start:]
	popupIndex, x, y, width, height = popupConfigure(t, reactiveEvents, popup)
	xdgIndex, serial = xdgConfigure(t, reactiveEvents, xdg)
	if popupIndex >= xdgIndex {
		t.Fatalf("reactive event order: popup=%d xdg=%d", popupIndex, xdgIndex)
	}
	if x != 12 || y != 10 || width != 8 || height != 6 {
		t.Fatalf("reactive popup configure = %d,%d %dx%d", x, y, width, height)
	}
	for _, event := range reactiveEvents {
		if event.id == popup && event.opcode == 2 {
			t.Fatal("reactive configure incorrectly sent repositioned")
		}
	}
	if got := popupPixel(t, s, root, 12, 10); !bytes.Equal(got, rootColor) {
		t.Fatalf("reactive placement applied before ack: %x", got)
	}
	c.send(xdg, 4, -1, serial)
	c.roundtrip()
	if got := popupPixel(t, s, root, 12, 10); !bytes.Equal(got, popupColor) {
		t.Fatalf("acknowledged reactive placement pixel = %x", got)
	}

	// xdg-shell assigns incomplete positioners to xdg_wm_base's
	// invalid_positioner error, including for the v3 reposition request.
	incomplete := c.id()
	c.send(c.wm, 1, -1, incomplete)
	c.send(popup, 2, -1, incomplete, uint32(7))
	start = len(c.events)
	expectWMBaseError(t, c, start, 5)
}

func TestPopupV3RejectsInvalidParentOnWMBase(t *testing.T) {
	s, err := Open(32, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := newCursorWireClient(t, s)

	surface, xdg, positioner, popup := c.id(), c.id(), c.id(), c.id()
	c.send(c.compositor, 0, -1, surface)
	c.send(c.wm, 2, -1, xdg, surface)
	c.send(c.wm, 1, -1, positioner)
	c.send(positioner, 1, -1, 8, 6)
	c.send(positioner, 2, -1, 1, 1, 1, 1)
	// A popup cannot name its own xdg_surface as its parent. The error belongs
	// to the WM base object, not the child xdg_surface.
	c.send(xdg, 2, -1, popup, xdg, positioner)
	start := len(c.events)
	expectWMBaseError(t, c, start, 3)
}
