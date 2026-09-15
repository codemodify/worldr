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

func TestCompositeDesktopPanelAlwaysOnBottom(t *testing.T) {
	const w, h, stride = 240, 80, 960
	dst := make([]byte, stride*h)
	clear := PackBGRA([4]float32{0, 0, 0, 1})
	CompositeDesktop(dst, stride, w, h, clear, nil, false, CursorBlit{}, Theater{}, OverviewDraw{},
		ChromeDraw{PanelH: PanelH, Clock: "12:00:00", Brand: "worldr"})
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
