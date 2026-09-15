package shell

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/engine"
)

func TestCompositeDesktopFakeActor(t *testing.T) {
	const w, h, stride = 64, 48, 256
	dst := make([]byte, stride*h)
	clear := PackBGRA([4]float32{0.04, 0.06, 0.12, 1}) // #0b1020-ish
	actor := &engine.Actor{
		X: 10, Y: 12, Width: 4, Height: 2, Stride: 16,
		Pixels: []byte{
			0x11, 0x22, 0x33, 0xff, 0x11, 0x22, 0x33, 0xff, 0x11, 0x22, 0x33, 0xff, 0x11, 0x22, 0x33, 0xff,
			0x44, 0x55, 0x66, 0xff, 0x44, 0x55, 0x66, 0xff, 0x44, 0x55, 0x66, 0xff, 0x44, 0x55, 0x66, 0xff,
		},
		Title: "fake-foot", Focused: true,
	}
	CompositeDesktop(dst, stride, w, h, clear, []*engine.Actor{actor}, true, CursorBlit{}, Theater{}, OverviewDraw{}, ChromeDraw{})

	// Actor pixel at (10,12)
	i := 12*stride + 10*4
	if dst[i] != 0x11 || dst[i+1] != 0x22 || dst[i+2] != 0x33 {
		t.Fatalf("actor pixel %x %x %x", dst[i], dst[i+1], dst[i+2])
	}
	// SSD title bar above the actor should not be the clear color
	title := (12-4)*stride + 10*4 // a few pixels into the title (TitleH=28, this is still in chrome)
	_ = title
	cy := actor.Y - 2
	ci := cy*stride + actor.X*4
	if cy >= 0 && dst[ci] == dst[0] && dst[ci+1] == dst[1] && dst[ci+2] == dst[2] {
		t.Fatal("expected SSD chrome above the actor, found clear color")
	}
}

func TestCompositeDesktopCursor(t *testing.T) {
	const w, h, stride = 32, 32, 128
	dst := make([]byte, stride*h)
	CompositeDesktop(dst, stride, w, h, 0xff000000, nil, false, CursorBlit{
		X: 4, Y: 4, Visible: true,
	}, Theater{}, OverviewDraw{}, ChromeDraw{})
	var n int
	for _, b := range dst {
		if b != 0 {
			n++
		}
	}
	if n < 8 {
		t.Fatalf("expected software cursor pixels, marked %d", n)
	}
}

func TestCompositeDesktopMapInFades(t *testing.T) {
	const w, h, stride = 64, 48, 256
	clear := PackBGRA([4]float32{0, 0, 0, 1})
	pix := make([]byte, 16)
	for i := 0; i < 16; i += 4 {
		pix[i], pix[i+1], pix[i+2], pix[i+3] = 0xff, 0xff, 0xff, 0xff
	}
	now := time.Unix(10, 0)
	actor := &engine.Actor{
		X: 20, Y: 20, Width: 4, Height: 1, Stride: 16, Pixels: pix,
		Born: now,
	}
	dst0 := make([]byte, stride*h)
	CompositeDesktop(dst0, stride, w, h, clear, []*engine.Actor{actor}, false, CursorBlit{},
		Theater{Now: now, Tier: engine.TierHigh}, OverviewDraw{}, ChromeDraw{})
	i := 20*stride + 20*4
	// t=0 map-in: alpha 0 — pixel stays clear (black)
	if dst0[i] != 0 || dst0[i+1] != 0 || dst0[i+2] != 0 {
		t.Fatalf("t0 should be faded out, got %x %x %x", dst0[i], dst0[i+1], dst0[i+2])
	}
	dst1 := make([]byte, stride*h)
	CompositeDesktop(dst1, stride, w, h, clear, []*engine.Actor{actor}, false, CursorBlit{},
		Theater{Now: now.Add(engine.MapInDuration), Tier: engine.TierHigh}, OverviewDraw{}, ChromeDraw{})
	if dst1[i] < 0xf0 {
		t.Fatalf("settled map-in should be opaque, got %x", dst1[i])
	}
}

func TestCompositeDesktopOverviewMovesActor(t *testing.T) {
	const w, h, stride = 128, 96, 512
	clear := PackBGRA([4]float32{0, 0, 0, 1})
	pix := []byte{0x10, 0x20, 0x30, 0xff}
	actor := &engine.Actor{X: 4, Y: 4, Width: 1, Height: 1, Stride: 4, Pixels: pix}
	dst := make([]byte, stride*h)
	CompositeDesktop(dst, stride, w, h, clear, []*engine.Actor{actor}, false, CursorBlit{},
		Theater{}, OverviewDraw{T: 1}, ChromeDraw{})
	// Home pixel should no longer be the actor (grid letterboxes toward center).
	home := 4*stride + 4*4
	if dst[home] == 0x10 && dst[home+1] == 0x20 && dst[home+2] == 0x30 {
		t.Fatal("overview T=1 should have moved the actor off its home pixel")
	}
	var found bool
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*stride + x*4
			if dst[i] == 0x10 && dst[i+1] == 0x20 && dst[i+2] == 0x30 {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected actor pixels somewhere in the grid")
	}
}
