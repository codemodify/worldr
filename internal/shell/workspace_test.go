package shell

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/input"
)

func TestHandleWorkspaceKeysCtrlAltArrows(t *testing.T) {
	scene := engine.NewScene()
	scene.SetWorkspaces(3)
	ctrl, alt := true, true
	ptr := &input.Pointer{Keys: []input.Key{{Code: keyRight, Pressed: true}}}
	handleWorkspaceKeys(scene, ptr, time.Unix(1, 0), &ctrl, &alt)
	if scene.ActiveWorkspace() != 1 {
		t.Fatal(scene.ActiveWorkspace())
	}
	ptr = &input.Pointer{Keys: []input.Key{{Code: keyLeft + 8, Pressed: true}}}
	handleWorkspaceKeys(scene, ptr, time.Unix(2, 0), &ctrl, &alt)
	if scene.ActiveWorkspace() != 0 {
		t.Fatal("evdev+8 left")
	}
	ctrl = false
	ptr = &input.Pointer{Keys: []input.Key{{Code: keyRight, Pressed: true}}}
	handleWorkspaceKeys(scene, ptr, time.Unix(3, 0), &ctrl, &alt)
	if scene.ActiveWorkspace() != 0 {
		t.Fatal("need both modifiers")
	}
}

func TestOverviewUsesCurrentDesktopOnly(t *testing.T) {
	scene := engine.NewScene()
	a := &engine.Actor{X: 10, Y: 10, Width: 20, Height: 20, Focused: true}
	scene.Add(a)
	scene.SwitchTo(1, time.Unix(1, 0))
	b := &engine.Actor{X: 80, Y: 10, Width: 20, Height: 20}
	scene.Add(b)
	var ov Overview
	ov.Open(time.Unix(2, 0))
	cells := engine.LayoutGrid(1, 800, 400)
	ok := pickOverview(&ov, scene, cells[0].X+2, cells[0].Y+2, 800, 400, time.Unix(3, 0))
	if !ok {
		t.Fatal("should pick the only window on desktop 1")
	}
	if !b.Focused || a.Focused {
		t.Fatalf("a=%v b=%v", a.Focused, b.Focused)
	}
}

func TestParseWorkspacesFlag(t *testing.T) {
	o, err := ParseFlags([]string{"-workspaces=4"})
	if err != nil || o.Workspaces != 4 {
		t.Fatalf("%+v %v", o, err)
	}
	o, err = ParseFlags([]string{"-workspaces=1"})
	if err != nil || o.Workspaces != 2 {
		t.Fatalf("clamp min %+v %v", o, err)
	}
	o, err = ParseFlags([]string{})
	if err != nil || o.Workspaces != 3 {
		t.Fatalf("default %+v %v", o, err)
	}
}
