package syncobj

import "testing"

func TestPointRoundTrip(t *testing.T) {
	if Point(0, 0) != 0 || Point(1, 2) != 1<<32+2 {
		t.Fatal(Point(1, 2))
	}
	hi, lo := SplitPoint(Point(7, 9))
	if hi != 7 || lo != 9 {
		t.Fatalf("%d %d", hi, lo)
	}
	if (Fence{}).Valid() || (Fence{FD: -1}).Valid() {
		t.Fatal("invalid")
	}
	if !(Fence{FD: 3, Point: 1}).Valid() {
		t.Fatal("valid")
	}
}

func TestFenceCloseNil(t *testing.T) {
	var f *Fence
	f.CloseFD()
	z := Fence{}
	z.CloseFD()
}

func TestWaitSignalNoFD(t *testing.T) {
	if err := WaitFence(Fence{}, 0); err == nil {
		t.Fatal("empty wait")
	}
	if err := SignalFence(Fence{}); err == nil {
		t.Fatal("empty signal")
	}
}
