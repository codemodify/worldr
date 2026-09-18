//go:build linux && cgo

package apps

import (
	"errors"
	"os"
	"testing"
)

func TestX11AssociationRequiresExactPeerAndPreservesSurfaceIdentity(t *testing.T) {
	s, err := Open(320, 240)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := newCursorWireClient(t, s)
	raw := c.cursor()
	b := c.image(40, 30, 0xff506070)
	c.send(raw, 1, -1, b, 0, 0)
	c.send(raw, 6, -1)
	c.roundtrip()
	if surfaces, _ := s.Poll(); len(surfaces) != 1 {
		t.Fatal("unassociated raw surface became a window", surfaces)
	}
	pid := uint32(os.Getpid())
	for _, pair := range [][2]uint32{{pid + 1, raw}, {pid, raw + 1000}} {
		if _, err := s.AssociateX11(pair[0], pair[1], 99, "X11", "fixture"); !errors.Is(err, ErrX11SurfacePending) {
			t.Fatal("association ignored exact credentials", err)
		}
	}
	if _, err := s.AssociateX11(pid, c.surface, 99, "X11", "fixture"); err == nil {
		t.Fatal("xdg role stolen")
	}
	id, err := s.AssociateX11(pid, raw, 99, "X11", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	check := func(title string) {
		t.Helper()
		surfaces, err := s.Poll()
		if err != nil || len(surfaces) != 2 {
			t.Fatal(surfaces, err)
		}
		for _, v := range surfaces {
			if v.ID == id {
				if v.Title != title || v.AppID != "fixture" || v.Width != 40 || s.X11Window(id) != 99 {
					t.Fatal(v)
				}
				return
			}
		}
		t.Fatal("associated surface disappeared")
	}
	check("X11")
	if err := s.Focus(id); err != nil {
		t.Fatal(err)
	}
	if err := s.Pointer(id, 2, 3); err != nil {
		t.Fatal(err)
	}
	b = c.image(40, 30, 0xff102030)
	c.send(raw, 1, -1, b, 0, 0)
	c.send(raw, 6, -1)
	c.roundtrip()
	check("X11")
	if err := s.UpdateX11(99, "Renamed", "fixture"); err != nil {
		t.Fatal(err)
	}
	check("Renamed")
	if err := s.WithdrawX11(99); err != nil {
		t.Fatal(err)
	}
	if surfaces, _ := s.Poll(); len(surfaces) != 1 || s.X11Window(id) != 0 {
		t.Fatal("withdrawal retained mapped X11 root", surfaces)
	}
	remapped, err := s.AssociateX11(pid, raw, 99, "X11", "fixture")
	if err != nil || remapped != id {
		t.Fatal("remap changed retained identity", remapped, id, err)
	}
	check("X11")
	c.send(raw, 0, -1)
	c.roundtrip()
	if surfaces, _ := s.Poll(); len(surfaces) != 1 {
		t.Fatal("destroyed raw surface retained", surfaces)
	}
}
