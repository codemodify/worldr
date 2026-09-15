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
