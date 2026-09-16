package engine

import (
	"testing"
	"time"
)

func TestBurnShadeFront(t *testing.T) {
	kind, _ := BurnShade(4, 0, 16, 16, 0)
	if kind != 2 {
		t.Fatalf("t0 top should be intact, got %d", kind)
	}
	kind, _ = BurnShade(4, 15, 16, 16, 1)
	if kind != 0 {
		t.Fatalf("t1 bottom should be gone, got %d", kind)
	}
	var sawEmber, sawGone, sawIntact bool
	for y := 0; y < 32; y++ {
		k, _ := BurnShade(3, y, 16, 32, 0.5)
		switch k {
		case 0:
			sawGone = true
		case 1:
			sawEmber = true
		case 2:
			sawIntact = true
		}
	}
	if !sawEmber || !sawGone || !sawIntact {
		t.Fatalf("mid burn needs gone/ember/intact (gone=%v ember=%v intact=%v)", sawGone, sawEmber, sawIntact)
	}
}

func TestTickBurnSeedsSparks(t *testing.T) {
	now := time.Unix(9, 0)
	a := &Actor{X: 10, Y: 20, Width: 40, Height: 30, UnmapAt: now}
	a.TickBurn(now, TierHigh)
	if a.burn == nil || !a.burn.seeded {
		t.Fatal("should seed")
	}
	first := a.burn.sparks[0]
	a.TickBurn(now.Add(16*time.Millisecond), TierHigh)
	if a.burn.sparks[0] == first {
		t.Fatal("sparks should integrate")
	}
	a.TickBurn(now, TierLow)
	// low is a no-op; leftover state is fine
}

func TestDrawBurnLeavesTop(t *testing.T) {
	const sW, sH = 12, 16
	src := make([]byte, sW*sH*4)
	for i := 0; i < len(src); i += 4 {
		src[i], src[i+1], src[i+2], src[i+3] = 0x40, 0x50, 0x60, 0xff
	}
	const dW, dH = 20, 24
	dst := make([]byte, dW*dH*4)
	a := &Actor{X: 2, Y: 4, Width: sW, Height: sH, UnmapAt: time.Unix(1, 0)}
	a.TickBurn(a.UnmapAt.Add(BurnDuration/2), TierHigh)
	DrawBurn(dst, dW*4, dW, dH, 2, 4, src, sW*4, sW, sH, 0.55, a)
	top := 4*(dW*4) + 6*4
	if dst[top] == 0 && dst[top+1] == 0 && dst[top+2] == 0 {
		t.Fatal("top of the window should still be there mid-burn")
	}
	bot := (4+sH-1)*(dW*4) + 6*4
	if dst[bot] == 0x40 && dst[bot+1] == 0x50 && dst[bot+2] == 0x60 {
		t.Fatal("bottom should have dissolved")
	}
}
