package wlsrv

import "testing"

func TestScaleTo120ths(t *testing.T) {
	cases := []struct {
		in   float64
		want uint32
	}{
		{1, 120},
		{1.0, 120},
		{1.25, 150},
		{1.5, 180},
		{2, 240},
		{0, 120},
		{-1, 120},
	}
	for _, tc := range cases {
		if got := ScaleTo120ths(tc.in); got != tc.want {
			t.Fatalf("ScaleTo120ths(%v)=%d want %d", tc.in, got, tc.want)
		}
	}
}

func TestIntegerScaleFrom120ths(t *testing.T) {
	cases := []struct {
		in   uint32
		want int32
	}{
		{120, 1},
		{150, 1}, // 1.25 → nearest 1
		{180, 2}, // 1.5 → 2
		{240, 2},
		{0, 1},
	}
	for _, tc := range cases {
		if got := IntegerScaleFrom120ths(tc.in); got != tc.want {
			t.Fatalf("IntegerScaleFrom120ths(%d)=%d want %d", tc.in, got, tc.want)
		}
	}
}

func TestLogicalSize(t *testing.T) {
	w, h := LogicalSize(200, 100, 0, 0, 1)
	if w != 200 || h != 100 {
		t.Fatalf("scale1 %dx%d", w, h)
	}
	w, h = LogicalSize(200, 100, 0, 0, 2)
	if w != 100 || h != 50 {
		t.Fatalf("bufscale2 %dx%d", w, h)
	}
	w, h = LogicalSize(250, 150, 200, 120, 2)
	if w != 200 || h != 120 {
		t.Fatalf("viewport dest wins %dx%d", w, h)
	}
	w, h = LogicalSize(10, 10, 0, 0, 0)
	if w != 10 || h != 10 {
		t.Fatalf("default bufscale %dx%d", w, h)
	}
}

func TestLogicalSizeScaledFractional(t *testing.T) {
	// 1.75: 350×210 buffer, no dest, no integer scale → 200×120 logical
	w, h := LogicalSizeScaled(350, 210, 0, 0, 1, 210)
	if w != 200 || h != 120 {
		t.Fatalf("1.75 infer %dx%d", w, h)
	}
	w, h = LogicalSizeScaled(350, 210, 200, 120, 1, 210)
	if w != 200 || h != 120 {
		t.Fatalf("dest still wins %dx%d", w, h)
	}
	w, h = LogicalSizeScaled(400, 200, 0, 0, 2, 210)
	if w != 200 || h != 100 {
		t.Fatalf("integer bufscale beats frac %dx%d", w, h)
	}
}

func TestApplyWindowGeometryFractional(t *testing.T) {
	// dest 200×120, buffer 350×210 (1.75), CSD inset (16,8,168,104)
	logW, logH, sx, sy, sw, sh := ApplyWindowGeometry(350, 210, 200, 120, 1, 210, GeoRect{
		X: 16, Y: 8, W: 168, H: 104, Set: true,
	})
	if logW != 168 || logH != 104 {
		t.Fatalf("logical %dx%d want 168x104", logW, logH)
	}
	if sx != 28 || sy != 14 || sw != 294 || sh != 182 {
		t.Fatalf("src crop %d,%d %dx%d want 28,14 294x182", sx, sy, sw, sh)
	}
}

func TestApplyWindowGeometryNoGeo(t *testing.T) {
	logW, logH, sx, sy, sw, sh := ApplyWindowGeometry(200, 100, 0, 0, 2, 0, GeoRect{})
	if logW != 100 || logH != 50 || sx != 0 || sy != 0 || sw != 200 || sh != 100 {
		t.Fatalf("no geo %dx%d src %d,%d %dx%d", logW, logH, sx, sy, sw, sh)
	}
}

func TestApplyWindowGeometryRejectsEmpty(t *testing.T) {
	logW, logH, _, _, sw, sh := ApplyWindowGeometry(200, 100, 200, 100, 1, 120, GeoRect{W: 0, H: 10, Set: true})
	if logW != 200 || logH != 100 || sw != 200 || sh != 100 {
		t.Fatalf("empty geo must not crop")
	}
}

func TestCropBGRA(t *testing.T) {
	// 4×2, crop (1,0,2,1)
	src := make([]byte, 4*2*4)
	src[1*4+0], src[1*4+1], src[1*4+2], src[1*4+3] = 1, 2, 3, 4
	src[2*4+0], src[2*4+1], src[2*4+2], src[2*4+3] = 5, 6, 7, 8
	out, stride := CropBGRA(src, 16, 4, 2, 1, 0, 2, 1)
	if stride != 8 || len(out) != 8 {
		t.Fatalf("stride %d len %d", stride, len(out))
	}
	if out[0] != 1 || out[4] != 5 {
		t.Fatalf("crop pixels %v", out)
	}
}

func TestCombineHostScale(t *testing.T) {
	if CombineHostScale(0, 0) != 1 {
		t.Fatal("default")
	}
	if CombineHostScale(0, 2) != 2 {
		t.Fatal("integer output")
	}
	if CombineHostScale(180, 1) != 1.5 {
		t.Fatal("frac wins over integer")
	}
	if CombineHostScale(150, 0) != 1.25 {
		t.Fatal("1.25")
	}
}

func TestServerDefaultScaleIsOne(t *testing.T) {
	s := &Server{}
	if s.PreferredScale120ths() != 120 || s.IntegerOutputScale() != 1 {
		t.Fatalf("default %d %d", s.PreferredScale120ths(), s.IntegerOutputScale())
	}
	s.SetOutputScale(1.5)
	if s.PreferredScale120ths() != 180 || s.IntegerOutputScale() != 2 {
		t.Fatalf("1.5 %d %d", s.PreferredScale120ths(), s.IntegerOutputScale())
	}
	s.SetOutputScale(1.25)
	if s.PreferredScale120ths() != 150 || s.IntegerOutputScale() != 1 {
		t.Fatalf("1.25 %d %d", s.PreferredScale120ths(), s.IntegerOutputScale())
	}
}
