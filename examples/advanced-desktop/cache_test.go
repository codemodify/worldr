package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"testing"

	xdraw "golang.org/x/image/draw"
)

func TestPhotographCachePreservesExactResampling(t *testing.T) {
	s, _, _ := testStudio(t)
	for _, size := range [][2]int{{1200, 960}, {720, 600}, {1511, 1007}} {
		if err := s.Resize(1, size[0], size[1]); err != nil {
			t.Fatal(err)
		}
		for _, phase := range []float64{0, .001, .32, .321, .72, 1, 0} {
			for slot, box := range [][4]int{{43, 182, 1114, 317}, {43, 585, 120, 91}} {
				cached := image.NewRGBA(image.Rect(0, 0, s.width, s.height))
				fresh := image.NewRGBA(cached.Bounds())
				fill := image.NewUniform(color.RGBA{9, 21, 35, 255})
				draw.Draw(cached, cached.Bounds(), fill, image.Point{}, draw.Src)
				draw.Draw(fresh, fresh.Bounds(), fill, image.Point{}, draw.Src)
				c := &canvas{s: s, img: cached}
				c.photograph(slot, box[0], box[1], box[2], box[3], phase)
				target := s.bounds(box[0], box[1], box[2], box[3])
				xdraw.CatmullRom.Scale(fresh, target, s.architecture, s.photographs[slot].source, xdraw.Over, nil)
				if !bytes.Equal(cached.Pix, fresh.Pix) {
					t.Fatalf("cached photograph changed pixels at size=%v phase=%v slot=%d", size, phase, slot)
				}
				old := s.photographs[slot].pixels
				c.photograph(slot, box[0], box[1], box[2], box[3], phase)
				if s.photographs[slot].pixels != old {
					t.Fatal("identical crop was rescaled")
				}
			}
		}
	}
}

func TestUnchangedResizeDoesNotRepaintStudio(t *testing.T) {
	s, validator, _ := testStudio(t)
	before := s.photographs[0].pixels
	if err := s.Resize(1, s.width, s.height); err != nil {
		t.Fatal(err)
	}
	if len(snapshot(t, s, validator).Textures) != 0 || before != s.photographs[0].pixels {
		t.Fatal("unchanged resize invalidated the image cache or published pixels")
	}
}
