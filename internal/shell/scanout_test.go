package shell

import (
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/scanout"
)

func TestPresenterTryScanoutVKDisplayFallsBack(t *testing.T) {
	p := &presenter{name: string(BackendVKDisplay), w: 800, h: 600}
	a := &engine.Actor{
		X: 0, Y: 0, Width: 800, Height: 600,
		ScanFD: 4, ScanFourcc: scanout.FourccXRGB8888, ScanStride: 3200,
	}
	err := p.tryScanout(a)
	if err == nil || !strings.Contains(err.Error(), "DRM master") {
		t.Fatalf("vk-display must fall back: %v", err)
	}
}

func TestPresenterTryScanoutNestedUnchanged(t *testing.T) {
	p := &presenter{name: string(BackendWaylandClient), w: 800, h: 600}
	a := &engine.Actor{
		X: 0, Y: 0, Width: 800, Height: 600,
		ScanFD: 4, ScanFourcc: scanout.FourccXRGB8888,
	}
	r := scanout.Evaluate(scanout.Frame{
		Backend: p.name, ScreenW: 800, ScreenH: 600,
		Actors: []*engine.Actor{a},
		WS:     engine.WorkspaceDraw{Count: 1, T: 1},
	})
	if r.OK {
		t.Fatal("nested must stay on host shm blit")
	}
	if err := p.tryScanout(a); err == nil || !strings.Contains(err.Error(), "no KMS") {
		t.Fatalf("nested has no KMS session: %v", err)
	}
}

func TestPresenterTryScanoutNeedsFD(t *testing.T) {
	p := &presenter{name: string(BackendDRM), w: 800, h: 600}
	if err := p.tryScanout(&engine.Actor{Width: 800, Height: 600}); err == nil {
		t.Fatal("shm has no scan fd")
	}
}

func TestPresenterTryOverlayVKDisplayFallsBack(t *testing.T) {
	p := &presenter{name: string(BackendVKDisplay), w: 800, h: 600}
	a := &engine.Actor{
		X: 40, Y: 50, Width: 320, Height: 200,
		ScanFD: 4, ScanFourcc: scanout.FourccXRGB8888, ScanStride: 1280,
	}
	err := p.tryOverlay(a)
	if err == nil || !strings.Contains(err.Error(), "DRM master") {
		t.Fatalf("vk-display overlay must fall back: %v", err)
	}
}

func TestPresenterTryOverlayNestedUnchanged(t *testing.T) {
	p := &presenter{name: string(BackendWaylandClient), w: 800, h: 600}
	a := &engine.Actor{X: 10, Y: 10, Width: 100, Height: 80, ScanFD: 4, ScanFourcc: scanout.FourccARGB8888}
	got := scanout.EvaluatePlanes(scanout.PlaneFrame{
		Frame: scanout.Frame{
			Backend: p.name, ScreenW: 800, ScreenH: 600,
			Actors: []*engine.Actor{a},
			WS:     engine.WorkspaceDraw{Count: 1, T: 1},
		},
		CursorVisible: true, CursorW: 32, CursorH: 32,
	})
	if got.Overlay != nil || got.Cursor || got.NeedDRM {
		t.Fatalf("nested must stay compose: %+v", got)
	}
	if err := p.tryOverlay(a); err == nil || !strings.Contains(err.Error(), "no KMS") {
		t.Fatalf("nested has no overlay session: %v", err)
	}
}

func TestPresenterTryCursorNeedsDRM(t *testing.T) {
	p := &presenter{name: string(BackendVKDisplay), w: 800, h: 600}
	err := p.tryCursor(CursorBlit{Visible: true, W: 32, H: 32, Pix: make([]byte, 32*32*4), Stride: 128})
	if err == nil || !strings.Contains(err.Error(), "no KMS") {
		t.Fatalf("vk-display cursor without drm: %v", err)
	}
}
