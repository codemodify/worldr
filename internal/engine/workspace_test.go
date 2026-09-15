package engine

import (
	"testing"
	"time"
)

func TestClampWorkspaces(t *testing.T) {
	if ClampWorkspaces(0) != 3 || ClampWorkspaces(-1) != 3 {
		t.Fatal("default")
	}
	if ClampWorkspaces(1) != 2 || ClampWorkspaces(2) != 2 {
		t.Fatal("min")
	}
	if ClampWorkspaces(3) != 3 || ClampWorkspaces(4) != 4 {
		t.Fatal("mid")
	}
	if ClampWorkspaces(9) != 4 {
		t.Fatal("max")
	}
}

func TestWrapWorkspace(t *testing.T) {
	if WrapWorkspace(-1, 3) != 2 || WrapWorkspace(3, 3) != 0 || WrapWorkspace(1, 3) != 1 {
		t.Fatal("wrap")
	}
}

func TestSwitchDirShortest(t *testing.T) {
	if SwitchDir(0, 1, 3) != 1 {
		t.Fatal("right")
	}
	if SwitchDir(1, 0, 3) != -1 {
		t.Fatal("left")
	}
	if SwitchDir(0, 2, 3) != -1 {
		t.Fatal("wrap left is shorter")
	}
	if SwitchDir(2, 0, 3) != 1 {
		t.Fatal("wrap right")
	}
}

func TestSlideOffsetsEnds(t *testing.T) {
	fo, to := SlideOffsets(1, 1000, 0)
	if fo != 0 || to != 1000 {
		t.Fatalf("t0 %d %d", fo, to)
	}
	fo, to = SlideOffsets(1, 1000, 1)
	if fo != -1000 || to != 0 {
		t.Fatalf("t1 %d %d", fo, to)
	}
	fo, to = SlideOffsets(-1, 1000, 1)
	if fo != 1000 || to != 0 {
		t.Fatalf("left t1 %d %d", fo, to)
	}
}

func TestAddAssignsActiveWorkspace(t *testing.T) {
	s := NewScene()
	s.SetWorkspaces(3)
	a := &Actor{Width: 10, Height: 10}
	s.Add(a)
	if a.Workspace != 0 {
		t.Fatal(a.Workspace)
	}
	if !s.SwitchTo(2, time.Unix(1, 0)) {
		t.Fatal("switch")
	}
	b := &Actor{Width: 10, Height: 10}
	s.Add(b)
	if b.Workspace != 2 || a.Workspace != 0 {
		t.Fatalf("a=%d b=%d", a.Workspace, b.Workspace)
	}
	on2 := s.ActorsOn(2)
	if len(on2) != 1 || on2[0] != b {
		t.Fatal("ActorsOn")
	}
	if len(s.ActorsOn(1)) != 0 {
		t.Fatal("empty 1")
	}
}

func TestFocusAtIgnoresOtherDesktop(t *testing.T) {
	s := NewScene()
	a := &Actor{X: 10, Y: 10, Width: 20, Height: 20}
	s.Add(a)
	s.SwitchTo(1, time.Unix(1, 0))
	if s.FocusAt(15, 15, 0, 0) != nil {
		t.Fatal("should miss actor on desktop 0")
	}
	b := &Actor{X: 10, Y: 10, Width: 20, Height: 20}
	s.Add(b)
	if s.FocusAt(15, 15, 0, 0) != b {
		t.Fatal("current desktop")
	}
	if a.Focused {
		t.Fatal("other desktop must not stay focused after switch+click")
	}
}

func TestStepWorkspaceWraps(t *testing.T) {
	s := NewScene()
	s.SetWorkspaces(3)
	if s.ActiveWorkspace() != 0 {
		t.Fatal("start")
	}
	s.StepWorkspace(-1, time.Unix(1, 0))
	if s.ActiveWorkspace() != 2 {
		t.Fatal(s.ActiveWorkspace())
	}
	s.StepWorkspace(1, time.Unix(2, 0))
	if s.ActiveWorkspace() != 0 {
		t.Fatal(s.ActiveWorkspace())
	}
}

func TestWorkspacePoseSettledAndSlide(t *testing.T) {
	s := NewScene()
	now := time.Unix(10, 0)
	p := s.WorkspacePose(now)
	if p.Count != 3 || p.Active != 0 || p.T != 1 || p.From != p.To {
		t.Fatalf("%+v", p)
	}
	s.SwitchTo(1, now)
	mid := s.WorkspacePose(now.Add(WorkspaceDuration / 2))
	if mid.From != 0 || mid.To != 1 || mid.Dir != 1 || mid.T <= 0.5 {
		t.Fatalf("mid %+v", mid)
	}
	done := s.WorkspacePose(now.Add(WorkspaceDuration))
	if done.Active != 1 || done.T != 1 || done.From != done.To {
		t.Fatalf("done %+v", done)
	}
	ox, show := done.OffsetFor(1, 800)
	if !show || ox != 0 {
		t.Fatal(ox, show)
	}
	_, show0 := done.OffsetFor(0, 800)
	if show0 {
		t.Fatal("hide old desktop when settled")
	}
}

func TestOccupied(t *testing.T) {
	s := NewScene()
	s.Add(&Actor{})
	s.SwitchTo(2, time.Unix(1, 0))
	s.Add(&Actor{})
	occ := s.Occupied()
	if len(occ) != 3 || !occ[0] || occ[1] || !occ[2] {
		t.Fatalf("%v", occ)
	}
}
