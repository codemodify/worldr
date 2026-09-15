package shell

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/engine"
)

func TestLayoutPanelHits(t *testing.T) {
	r := LayoutPanel(800, 600)
	if r.Bar.Y != 600-PanelH || r.Bar.H != PanelH {
		t.Fatalf("bar %+v", r.Bar)
	}
	if HitPanel(r, 10, 10) != PanelHitNone {
		t.Fatal("desktop miss")
	}
	if HitPanel(r, r.Launch.X+2, r.Launch.Y+2) != PanelHitLaunch {
		t.Fatal("apps")
	}
	if HitPanel(r, r.Overview.X+2, r.Overview.Y+2) != PanelHitOverview {
		t.Fatal("grid")
	}
	if HitPanel(r, r.Clock.X+2, r.Clock.Y+2) != PanelHitBar {
		t.Fatal("clock is bar")
	}
	if inCell(r.Launch, r.Overview.X+1, r.Overview.Y+1) {
		t.Fatal("apps and grid overlap")
	}
}

func TestLayoutPanelPagerHits(t *testing.T) {
	r := LayoutPanelWS(800, 600, 3)
	if len(r.Dots) != 3 {
		t.Fatal(len(r.Dots))
	}
	if r.Label.W <= 0 || r.Label.X != r.Pager.X {
		t.Fatalf("N/M label %+v pager %+v", r.Label, r.Pager)
	}
	if r.Dots[0].X < r.Label.X+r.Label.W {
		t.Fatal("dots must sit after the N/M label")
	}
	if HitPager(r, r.Dots[2].X+2, r.Dots[2].Y+2) != 2 {
		t.Fatal("dot 2")
	}
	if HitPanel(r, r.Dots[1].X+2, r.Dots[1].Y+2) != PanelHitPager {
		t.Fatal("zone")
	}
	if inCell(r.Dots[0], r.Overview.X+1, r.Overview.Y+1) {
		t.Fatal("pager/overview overlap")
	}
}

func TestPanelDrawsWorkspaceLabel(t *testing.T) {
	const w, h, stride = 800, 80, 3200
	dst := make([]byte, stride*h)
	clear := PackBGRA([4]float32{0, 0, 0, 1})
	CompositeDesktop(dst, stride, w, h, clear, nil, false, CursorBlit{}, Theater{}, OverviewDraw{},
		ChromeDraw{
			PanelH:   PanelH,
			WS:       engine.WorkspaceDraw{Count: 3, Active: 1, From: 1, To: 1, T: 1},
			Occupied: []bool{true, false, false},
		}, false)
	r := LayoutPanelWS(w, h, 3)
	found := false
	for y := r.Label.Y; y < r.Label.Y+r.Label.H && !found; y++ {
		for x := r.Label.X; x < r.Label.X+r.Label.W; x++ {
			i := y*stride + x*4
			if dst[i] != 0 || dst[i+1] != 0 || dst[i+2] != 0 {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected N/M label pixels on the pager")
	}
}

func TestLayoutPanelEmptyWorkspaceDotsHit(t *testing.T) {
	r := LayoutPanelWS(800, 600, 3)
	if HitPager(r, r.Dots[1].X+2, r.Dots[1].Y+2) != 1 {
		t.Fatal("empty desktop 2 stays clickable")
	}
}

func TestClockStringLocal(t *testing.T) {
	now := time.Date(2026, 9, 15, 14, 5, 9, 0, time.FixedZone("x", 0))
	s := ClockString(now)
	if s != "14:05:09" {
		t.Fatal(s)
	}
}

func TestFocusedTitle(t *testing.T) {
	if FocusedTitle(nil) != "" {
		t.Fatal("empty")
	}
	a := &engine.Actor{Title: "foot", Focused: true}
	b := &engine.Actor{Title: "other"}
	if FocusedTitle([]*engine.Actor{b, a}) != "foot" {
		t.Fatal("title")
	}
	a.Title = ""
	a.AppID = "org.foo"
	if FocusedTitle([]*engine.Actor{a}) != "org.foo" {
		t.Fatal("appid")
	}
}

func TestLayoutLauncherClipsOverflow(t *testing.T) {
	card, rows := LayoutLauncher(80, 800, 400, PanelH)
	if card.Y+card.H > 400-PanelH {
		t.Fatalf("card overlaps panel %+v", card)
	}
	if len(rows) >= 80 || len(rows) == 0 {
		t.Fatalf("expected clipped rows, got %d", len(rows))
	}
	last := rows[len(rows)-1]
	if last.Y+last.H > card.Y+card.H {
		t.Fatalf("row overflows card %+v card=%+v", last, card)
	}
}

func TestLayoutLauncherRows(t *testing.T) {
	card, rows := LayoutLauncher(3, 800, 600, PanelH)
	if len(rows) != 3 {
		t.Fatal(len(rows))
	}
	if card.Y+card.H > 600-PanelH {
		t.Fatalf("card overlaps panel %+v", card)
	}
	idx, inside := HitLauncher(card, rows, rows[1].X+4, rows[1].Y+4)
	if idx != 1 || !inside {
		t.Fatalf("hit %d %v", idx, inside)
	}
	idx, inside = HitLauncher(card, rows, 1, 1)
	if idx != -1 || inside {
		t.Fatalf("miss %d %v", idx, inside)
	}
}

func TestPanelDrawsFocusedIcon(t *testing.T) {
	const w, h, stride = 240, 80, 960
	dst := make([]byte, stride*h)
	clear := PackBGRA([4]float32{0, 0, 0, 1})
	red := []byte{0x00, 0x00, 0xff, 0xff}
	CompositeDesktop(dst, stride, w, h, clear, nil, false, CursorBlit{}, Theater{}, OverviewDraw{},
		ChromeDraw{PanelH: PanelH, Title: "foot", Icon: red, IconW: 1, IconH: 1, IconStride: 4}, false)
	r := LayoutPanelWS(w, h, 0)
	i := (r.Title.Y+4)*stride + r.Title.X*4
	if dst[i] == 0 && dst[i+1] == 0 && dst[i+2] == 0 {
		t.Fatal("expected panel icon in the title slot")
	}
}

func TestCompositeDesktopPanelAlwaysOnBottom(t *testing.T) {
	const w, h, stride = 240, 80, 960
	dst := make([]byte, stride*h)
	clear := PackBGRA([4]float32{0, 0, 0, 1})
	CompositeDesktop(dst, stride, w, h, clear, nil, false, CursorBlit{}, Theater{}, OverviewDraw{},
		ChromeDraw{PanelH: PanelH, Clock: "12:00:00", Brand: "worldr"}, false)
	// Top of the panel (accent line) is cyan, not clear.
	y := h - PanelH
	i := y*stride + 8*4
	if dst[i] == 0 && dst[i+1] == 0 && dst[i+2] == 0 {
		t.Fatal("expected panel pixels at the bottom")
	}
	// Desktop above the panel stays clear.
	top := 4*stride + 8*4
	if dst[top] != 0 || dst[top+1] != 0 || dst[top+2] != 0 {
		t.Fatal("desktop above panel should stay clear")
	}
}
