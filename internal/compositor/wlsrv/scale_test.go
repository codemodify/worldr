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
