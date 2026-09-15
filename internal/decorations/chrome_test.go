package decorations

import (
	"testing"

	"github.com/codemodify/worldr/internal/engine"
)

func TestDrawSkipsNoChrome(t *testing.T) {
	const w, h, stride = 80, 60, 320
	dst := make([]byte, stride*h)
	a := &engine.Actor{X: 20, Y: 32, Width: 20, Height: 16, Focused: true, NoChrome: true}
	Draw(dst, stride, w, h, a)
	for _, b := range dst {
		if b != 0 {
			t.Fatal("popup/subsurface must not paint SSD")
		}
	}
	if HitTitle(a, 20, 10) {
		t.Fatal("NoChrome has no title hit")
	}
}

func TestChromeGeometry(t *testing.T) {
	if TitleH != 28 || AccentH != 4 || Border != 6 {
		t.Fatalf("ssd v0 chrome TitleH=%d AccentH=%d Border=%d", TitleH, AccentH, Border)
	}
	l, r, top, b := Insets()
	if l != Border || r != Border || top != TitleH || b != Border {
		t.Fatalf("insets %d %d %d %d", l, r, top, b)
	}
}

func TestDrawFocusedGlowOutsideFrame(t *testing.T) {
	const w, h, stride = 80, 60, 320
	dst := make([]byte, stride*h)
	a := &engine.Actor{X: 20, Y: 32, Width: 20, Height: 16, Focused: true}
	Draw(dst, stride, w, h, a)
	// Pixel just outside the left frame should be the glow, not empty.
	gx := a.X - Border - 1
	gy := a.Y
	i := gy*stride + gx*4
	if dst[i] == 0 && dst[i+1] == 0 && dst[i+2] == 0 {
		t.Fatal("expected focused glow just outside the frame")
	}
	// Accent stripe at the top of the title bar is the cyan highlight.
	ax, ay := a.X, a.Y-TitleH
	ai := ay*stride + ax*4
	want := TitleStripe()
	if dst[ai] != byte(want) || dst[ai+1] != byte(want>>8) || dst[ai+2] != byte(want>>16) {
		t.Fatalf("accent %x %x %x want %#x", dst[ai], dst[ai+1], dst[ai+2], want)
	}
}
