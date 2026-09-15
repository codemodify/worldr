package shell

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/input"
)

func TestOverviewProgressEase(t *testing.T) {
	now := time.Unix(10, 0)
	var o Overview
	if o.Progress(now) != 0 {
		t.Fatal("idle")
	}
	o.Toggle(now)
	if o.Progress(now) != 0 {
		t.Fatal("t0 enter is 0")
	}
	mid := o.Progress(now.Add(engine.OverviewDuration / 2))
	if mid <= 0.5 {
		t.Fatalf("ease-out mid %v", mid)
	}
	if o.Progress(now.Add(engine.OverviewDuration)) != 1 {
		t.Fatal("enter done")
	}
	o.Toggle(now.Add(engine.OverviewDuration))
	leave0 := now.Add(engine.OverviewDuration)
	if o.Progress(leave0) != 1 {
		t.Fatal("leave t0 is 1")
	}
	if o.Progress(leave0.Add(engine.OverviewDuration)) != 0 {
		t.Fatal("leave done")
	}
}

func TestHandleOverviewKeysF12AndEsc(t *testing.T) {
	scene := engine.NewScene()
	a := &engine.Actor{X: 10, Y: 10, Width: 20, Height: 20, Focused: true}
	scene.Add(a)
	var ov Overview
	meta := false
	ptr := &input.Pointer{Keys: []input.Key{{Code: keyF12, Pressed: true}}}
	now := time.Unix(1, 0)
	handleOverviewKeys(&ov, ptr, scene, now, 800, 600, &meta, false)
	if !ov.Want {
		t.Fatal("F12 should open")
	}
	ptr = &input.Pointer{Quit: true, Keys: []input.Key{{Code: keyEsc, Pressed: true}}}
	handleOverviewKeys(&ov, ptr, scene, now.Add(time.Second), 800, 600, &meta, false)
	if ov.Want {
		t.Fatal("Esc should close overview")
	}
	if ptr.Quit {
		t.Fatal("Esc must not quit while leaving overview")
	}
}

func TestOverviewMoveSelectWraps(t *testing.T) {
	var o Overview
	o.moveSelect(1, 3)
	if o.Select != 1 {
		t.Fatal(o.Select)
	}
	o.moveSelect(1, 3)
	o.moveSelect(1, 3)
	if o.Select != 0 {
		t.Fatal(o.Select)
	}
	o.moveSelect(-1, 3)
	if o.Select != 2 {
		t.Fatal(o.Select)
	}
}

func TestHandleOverviewKeysSuperTabAndEnter(t *testing.T) {
	scene := engine.NewScene()
	a := &engine.Actor{X: 10, Y: 10, Width: 20, Height: 20, Focused: true}
	b := &engine.Actor{X: 80, Y: 10, Width: 20, Height: 20}
	scene.Add(a)
	scene.Add(b)
	var ov Overview
	meta := true
	now := time.Unix(1, 0)
	ptr := &input.Pointer{Keys: []input.Key{{Code: keyTab, Pressed: true}}}
	handleOverviewKeys(&ov, ptr, scene, now, 800, 600, &meta, false)
	if !ov.Want {
		t.Fatal("Super+Tab should open")
	}
	if ov.Select != 0 {
		t.Fatalf("select focused %d", ov.Select)
	}
	meta = false
	ptr = &input.Pointer{Keys: []input.Key{{Code: keyRight, Pressed: true}}}
	handleOverviewKeys(&ov, ptr, scene, now.Add(time.Millisecond), 800, 600, &meta, false)
	if ov.Select != 1 || !b.Focused || a.Focused {
		t.Fatalf("arrow should move focus select=%d a=%v b=%v", ov.Select, a.Focused, b.Focused)
	}
	ptr = &input.Pointer{Keys: []input.Key{{Code: keyEnter, Pressed: true}}}
	handleOverviewKeys(&ov, ptr, scene, now.Add(2*time.Millisecond), 800, 600, &meta, false)
	if ov.Want {
		t.Fatal("Enter should exit")
	}
	if !b.Focused {
		t.Fatal("Enter should keep the picked actor focused")
	}
}

func TestHandleOverviewKeysF12WaylandOffset(t *testing.T) {
	scene := engine.NewScene()
	scene.Add(&engine.Actor{Width: 10, Height: 10})
	var ov Overview
	meta := false
	ptr := &input.Pointer{Keys: []input.Key{{Code: keyF12 + 8, Pressed: true}}}
	handleOverviewKeys(&ov, ptr, scene, time.Unix(1, 0), 800, 600, &meta, false)
	if !ov.Want {
		t.Fatal("nested evdev+8 F12 should toggle")
	}
}

func TestPickOverviewHitsSecondTile(t *testing.T) {
	scene := engine.NewScene()
	a := &engine.Actor{X: 10, Y: 10, Width: 40, Height: 40, Focused: true}
	b := &engine.Actor{X: 80, Y: 10, Width: 40, Height: 40}
	scene.Add(a)
	scene.Add(b)
	var ov Overview
	ov.Open(time.Unix(1, 0))
	cells := engine.LayoutGrid(2, 800, 400)
	if len(cells) != 2 {
		t.Fatal(len(cells))
	}
	ok := pickOverview(&ov, scene, cells[1].X+2, cells[1].Y+2, 800, 400, time.Unix(2, 0))
	if !ok {
		t.Fatal("expected hit")
	}
	if ov.Want {
		t.Fatal("pick should close")
	}
	if !b.Focused || a.Focused {
		t.Fatalf("focus a=%v b=%v", a.Focused, b.Focused)
	}
}
