package engine

import (
	"testing"
	"time"
)

func TestMeshGridReuses(t *testing.T) {
	now := time.Unix(1, 0)
	a := &Actor{X: 4, Y: 8, Width: 40, Height: 24}
	a.TickWobble(now, TierHigh)
	buf := make([]float32, 0, 8)
	cols, rows, pts := a.MeshGrid(buf)
	if cols != WobbleCols || rows != WobbleRows {
		t.Fatalf("grid %d×%d", cols, rows)
	}
	need := cols * rows * 2
	if cap(pts) < need || len(pts) != need {
		t.Fatalf("len=%d cap=%d need=%d", len(pts), cap(pts), need)
	}
	// Grow once, then reuse the same backing store.
	big := make([]float32, 0, need+16)
	_, _, pts2 := a.MeshGrid(big)
	if cap(pts2) != cap(big) {
		t.Fatalf("should reuse cap %d got %d", cap(big), cap(pts2))
	}
}

func TestGrabKeepsMeshLive(t *testing.T) {
	now := time.Unix(2, 0)
	a := &Actor{X: 0, Y: 0, Width: 60, Height: 30, GrabOn: true, GrabLX: 10, GrabLY: -4}
	a.TickWobble(now, TierHigh)
	if !a.MeshLive() {
		t.Fatal("title-drag pin must keep the mesh live")
	}
	bx, by, bw, bh := a.MeshBox()
	if bw <= a.Width || bh <= a.Height || bx != a.X-meshChromeL || by != a.Y-meshChromeT {
		t.Fatalf("chrome box %d,%d %dx%d", bx, by, bw, bh)
	}
}

func TestBlitMeshFillsShearedQuad(t *testing.T) {
	const sW, sH = 4, 4
	src := make([]byte, sW*sH*4)
	for i := 0; i < len(src); i += 4 {
		src[i], src[i+1], src[i+2], src[i+3] = 0x10, 0x20, 0x30, 0xff
	}
	const dW, dH = 16, 16
	dst := make([]byte, dW*dH*4)
	// 2×2 verts: a square shifted right on the bottom edge.
	pts := []float32{
		2, 2, 6, 2,
		3, 6, 7, 6,
	}
	BlitMesh(dst, dW*4, dW, dH, src, sW*4, sW, sH, 2, 2, pts, 1)
	var n int
	for y := 0; y < dH; y++ {
		for x := 0; x < dW; x++ {
			i := y*(dW*4) + x*4
			if dst[i] == 0x10 && dst[i+1] == 0x20 && dst[i+2] == 0x30 {
				n++
			}
		}
	}
	if n < 8 {
		t.Fatalf("expected sheared blit pixels, got %d", n)
	}
}

func TestMapOutLen(t *testing.T) {
	if MapOutLen(TierHigh) != BurnDuration {
		t.Fatal("high is burn")
	}
	if MapOutLen(TierLow) != MapOutDuration || MapOutLen(TierOff) != MapOutDuration {
		t.Fatal("low/off keep the short fade")
	}
}
