//go:build linux && cgo

package apps

import (
	"bytes"
	"testing"
)

func damageSurface(t *testing.T, s *Server) Surface {
	t.Helper()
	surfaces, err := s.Poll()
	if err != nil || len(surfaces) != 1 {
		t.Fatalf("polling damaged surface: %v, surfaces=%d", err, len(surfaces))
	}
	return surfaces[0]
}

func damagePixel(t *testing.T, surface Surface, x, y int, want []byte) {
	t.Helper()
	if x < 0 || y < 0 || x >= surface.Width || y >= surface.Height {
		t.Fatalf("pixel %d,%d outside %dx%d", x, y, surface.Width, surface.Height)
	}
	offset := (y*surface.Width + x) * 4
	if got := surface.Pixels[offset : offset+4]; !bytes.Equal(got, want) {
		t.Fatalf("pixel %d,%d = %x, want %x", x, y, got, want)
	}
}

func TestSHMBufferAndSurfaceDamagePreserveImmutableUndamagedPixels(t *testing.T) {
	s, err := Open(32, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := newCursorWireClient(t, s)
	first := damageSurface(t, s)
	firstPixels := append([]byte(nil), first.Pixels...)
	dark := []byte{0x18, 0x30, 0x50, 0xff}
	green := []byte{0x20, 0xd0, 0x40, 0xff}
	blue := []byte{0x10, 0x40, 0xe0, 0xff}
	red := []byte{0xe0, 0x20, 0x10, 0xff}

	// A new buffer may contain unrelated bytes outside the advertised damage.
	// Only the clipped buffer-space rectangle becomes part of the next snapshot.
	buffer := c.image(32, 24, 0xff20d040)
	c.send(c.surface, 1, -1, buffer, 0, 0)
	c.send(c.surface, 9, -1, 4, 3, 3, 2)
	c.send(c.surface, 6, -1)
	c.roundtrip()
	second := damageSurface(t, s)
	if second.Revision <= first.Revision {
		t.Fatal("damaged commit did not advance the surface revision")
	}
	damagePixel(t, second, 4, 3, green)
	damagePixel(t, second, 6, 4, green)
	damagePixel(t, second, 3, 3, dark)
	damagePixel(t, second, 7, 4, dark)
	if !bytes.Equal(first.Pixels, firstPixels) {
		t.Fatal("incremental commit mutated an earlier immutable snapshot")
	}

	// Negative buffer coordinates are clipped before source access. The union is
	// still applied atomically at commit and leaves the previous green patch.
	buffer = c.image(32, 24, 0xff1040e0)
	c.send(c.surface, 1, -1, buffer, 0, 0)
	c.send(c.surface, 9, -1, -2, -2, 4, 4)
	c.send(c.surface, 6, -1)
	c.roundtrip()
	third := damageSurface(t, s)
	damagePixel(t, third, 0, 0, blue)
	damagePixel(t, third, 1, 1, blue)
	damagePixel(t, third, 2, 1, dark)
	damagePixel(t, third, 4, 3, green)

	// Surface-space damage is converted with the scale that becomes effective on
	// the same commit. A 2x2 logical rectangle updates a 4x4 buffer rectangle.
	buffer = c.image(32, 24, 0xffe02010)
	c.send(c.surface, 8, -1, 2)
	c.send(c.surface, 1, -1, buffer, 0, 0)
	c.send(c.surface, 2, -1, 5, 4, 2, 2)
	c.send(c.surface, 6, -1)
	c.roundtrip()
	fourth := damageSurface(t, s)
	if fourth.LogicalWidth != 16 || fourth.LogicalHeight != 12 {
		t.Fatalf("scaled logical size = %dx%d", fourth.LogicalWidth, fourth.LogicalHeight)
	}
	damagePixel(t, fourth, 10, 8, red)
	damagePixel(t, fourth, 13, 11, red)
	damagePixel(t, fourth, 9, 8, dark)
	damagePixel(t, fourth, 4, 3, green)

	// Damage is consumed by each commit. For compatibility with clients that
	// attach without an explicit request, the following replacement is copied in
	// full rather than accidentally reusing the preceding rectangle.
	buffer = c.image(32, 24, 0xffffffff)
	c.send(c.surface, 1, -1, buffer, 0, 0)
	c.send(c.surface, 6, -1)
	c.roundtrip()
	full := damageSurface(t, s)
	damagePixel(t, full, 0, 0, []byte{0xff, 0xff, 0xff, 0xff})
	damagePixel(t, full, 31, 23, []byte{0xff, 0xff, 0xff, 0xff})
}
