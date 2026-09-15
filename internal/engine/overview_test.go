package engine

import "testing"

func TestLayoutGridEmpty(t *testing.T) {
	if LayoutGrid(0, 1280, 720) != nil {
		t.Fatal("empty")
	}
}

func TestLayoutGridOne(t *testing.T) {
	g := LayoutGrid(1, 1280, 720)
	if len(g) != 1 {
		t.Fatalf("n=%d", len(g))
	}
	if g[0].W < 200 || g[0].H < 200 {
		t.Fatalf("tiny cell %+v", g[0])
	}
	if g[0].X < 0 || g[0].Y < 0 || g[0].X+g[0].W > 1280 || g[0].Y+g[0].H > 720 {
		t.Fatalf("out of screen %+v", g[0])
	}
}

func TestLayoutGridFourIsTwoByTwo(t *testing.T) {
	g := LayoutGrid(4, 800, 600)
	if len(g) != 4 {
		t.Fatal(len(g))
	}
	// no overlap
	for i := 0; i < 4; i++ {
		for j := i + 1; j < 4; j++ {
			if overlap(g[i], g[j]) {
				t.Fatalf("overlap %d %d %+v %+v", i, j, g[i], g[j])
			}
		}
		if g[i].W < 80 || g[i].H < 60 {
			t.Fatalf("cell %d %+v", i, g[i])
		}
	}
	// 2x2: first two share Y, first and third share X
	if g[0].Y != g[1].Y || g[0].X == g[1].X {
		t.Fatalf("row0 %+v %+v", g[0], g[1])
	}
	if g[0].X != g[2].X || g[0].Y == g[2].Y {
		t.Fatalf("col0 %+v %+v", g[0], g[2])
	}
}

func TestLayoutGridThreeUsesTwoCols(t *testing.T) {
	g := LayoutGrid(3, 1000, 800)
	if len(g) != 3 {
		t.Fatal(len(g))
	}
	if g[0].Y != g[1].Y {
		t.Fatalf("first row should be two-across: %+v %+v", g[0], g[1])
	}
	if g[2].Y <= g[0].Y {
		t.Fatalf("third should wrap: %+v", g[2])
	}
}

func TestFitInCellLetterbox(t *testing.T) {
	cell := GridCell{X: 0, Y: 0, W: 200, H: 200}
	f := FitInCell(400, 100, cell) // wide
	if f.W > 200 || f.H > 200 {
		t.Fatalf("%+v", f)
	}
	if f.W <= f.H {
		t.Fatalf("should stay wide %+v", f)
	}
	if f.X < 0 || f.Y < 0 {
		t.Fatal(f)
	}
}

func TestLerpCellEnds(t *testing.T) {
	a := GridCell{X: 0, Y: 0, W: 10, H: 10}
	b := GridCell{X: 100, Y: 50, W: 40, H: 20}
	if LerpCell(a, b, 0) != a {
		t.Fatal("t0")
	}
	if LerpCell(a, b, 1) != b {
		t.Fatal("t1")
	}
	m := LerpCell(a, b, 0.5)
	if m.X < 40 || m.X > 60 {
		t.Fatalf("mid %+v", m)
	}
}

func TestHitGrid(t *testing.T) {
	g := LayoutGrid(2, 800, 400)
	if HitGrid(g, -1, 0) != -1 {
		t.Fatal("miss")
	}
	i := HitGrid(g, g[0].X+1, g[0].Y+1)
	if i != 0 {
		t.Fatalf("hit %d", i)
	}
	j := HitGrid(g, g[1].X+1, g[1].Y+1)
	if j != 1 {
		t.Fatalf("hit1 %d", j)
	}
}

func TestLayoutGridNineNoOverlap(t *testing.T) {
	g := LayoutGrid(9, 1920, 1080)
	if len(g) != 9 {
		t.Fatal(len(g))
	}
	for i := 0; i < 9; i++ {
		if g[i].W < 80 || g[i].H < 60 {
			t.Fatalf("cell %d %+v", i, g[i])
		}
		if g[i].X < 0 || g[i].Y < 0 || g[i].X+g[i].W > 1920 || g[i].Y+g[i].H > 1080 {
			t.Fatalf("oob %d %+v", i, g[i])
		}
		for j := i + 1; j < 9; j++ {
			if overlap(g[i], g[j]) {
				t.Fatalf("overlap %d %d", i, j)
			}
		}
	}
	// 3x3: cells 0,1,2 share Y; 0,3,6 share X
	if g[0].Y != g[1].Y || g[1].Y != g[2].Y {
		t.Fatalf("row0 %+v %+v %+v", g[0], g[1], g[2])
	}
	if g[0].X != g[3].X || g[3].X != g[6].X {
		t.Fatalf("col0 %+v %+v %+v", g[0], g[3], g[6])
	}
}

func TestHomeFrameSSD(t *testing.T) {
	a := &Actor{X: 20, Y: 30, Width: 100, Height: 50}
	f := HomeFrame(a, true, 6, 28)
	if f.X != 14 || f.Y != 2 || f.W != 112 || f.H != 84 {
		t.Fatalf("%+v", f)
	}
}

func TestHomeFrameNoChrome(t *testing.T) {
	a := &Actor{X: 20, Y: 30, Width: 100, Height: 50, NoChrome: true}
	f := HomeFrame(a, true, 6, 28)
	if f.X != 20 || f.Y != 30 || f.W != 100 || f.H != 50 {
		t.Fatalf("%+v", f)
	}
}

func overlap(a, b GridCell) bool {
	return a.X < b.X+b.W && a.X+a.W > b.X && a.Y < b.Y+b.H && a.Y+a.H > b.Y
}
