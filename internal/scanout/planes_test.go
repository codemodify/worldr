package scanout

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/engine"
)

func winActor(x, y, w, h int) *engine.Actor {
	return &engine.Actor{
		X: x, Y: y, Width: w, Height: h,
		BufW: w, BufH: h,
		ScanFD: 9, ScanFourcc: FourccARGB8888, ScanStride: uint32(w * 4),
		Workspace: 0,
	}
}

func TestCursorPlaneOK(t *testing.T) {
	if !CursorPlaneOK("drm", true, 64, 64) || !CursorPlaneOK("vk-display", true, 256, 256) {
		t.Fatal("kms cursor")
	}
	for _, tc := range []struct {
		be      string
		vis     bool
		w, h    int
		wantOff bool
	}{
		{"wayland-client", true, 64, 64, true},
		{"nested", true, 32, 32, true},
		{"headless", true, 32, 32, true},
		{"drm", false, 64, 64, true},
		{"drm", true, 0, 64, true},
		{"drm", true, 257, 64, true},
		{"drm", true, 64, 300, true},
	} {
		if CursorPlaneOK(tc.be, tc.vis, tc.w, tc.h) != !tc.wantOff {
			t.Fatalf("%+v", tc)
		}
	}
}

func TestOverlayActorOK(t *testing.T) {
	if !OverlayActorOK("drm", true, false, false, 400, 300) {
		t.Fatal("windowed drm")
	}
	if !OverlayActorOK("vk-display", true, false, false, 100, 80) {
		t.Fatal("vk-display eligible")
	}
	if OverlayActorOK("wayland-client", true, false, false, 400, 300) {
		t.Fatal("nested")
	}
	if OverlayActorOK("drm", false, false, false, 400, 300) {
		t.Fatal("shm")
	}
	if OverlayActorOK("drm", true, true, false, 1920, 1080) {
		t.Fatal("fullscreen is primary, not overlay")
	}
	if OverlayActorOK("drm", true, false, true, 400, 300) {
		t.Fatal("scaled")
	}
	if OverlayActorOK("drm", true, false, false, 0, 10) {
		t.Fatal("empty")
	}
}

func TestVKDisplayCanOverlay(t *testing.T) {
	if !VKDisplayCanOverlay("vk-display", 2) || VKDisplayCanOverlay("vk-display", 1) {
		t.Fatal("extra planes")
	}
	if VKDisplayCanOverlay("drm", 4) || VKDisplayCanOverlay("nested", 8) {
		t.Fatal("only vk-display")
	}
}

func TestEvaluatePlanesPrimaryAndCursor(t *testing.T) {
	a := fullActor(800, 600)
	got := EvaluatePlanes(PlaneFrame{
		Frame: Frame{
			Backend: "drm", ScreenW: 800, ScreenH: 600,
			Actors: []*engine.Actor{a},
			WS:     engine.WorkspaceDraw{Count: 1, Active: 0, T: 1},
		},
		CursorVisible: true, CursorW: 64, CursorH: 64,
	})
	if got.Primary != a || !got.Cursor || got.Overlay != nil || !got.NeedDRM || got.Reason != "primary" {
		t.Fatalf("%+v", got)
	}
}

func TestEvaluatePlanesOverlayWindowed(t *testing.T) {
	a := winActor(40, 50, 320, 200)
	got := EvaluatePlanes(PlaneFrame{
		Frame: Frame{
			Backend: "drm", ScreenW: 800, ScreenH: 600,
			Actors: []*engine.Actor{a},
			WS:     engine.WorkspaceDraw{Count: 1, Active: 0, T: 1},
		},
	})
	if got.Overlay != a || got.Primary != nil || !got.NeedDRM || got.Reason != "overlay" {
		t.Fatalf("%+v", got)
	}
}

func TestEvaluatePlanesNestedSafe(t *testing.T) {
	a := winActor(10, 10, 100, 80)
	got := EvaluatePlanes(PlaneFrame{
		Frame: Frame{
			Backend: "wayland-client", ScreenW: 800, ScreenH: 600,
			Actors: []*engine.Actor{a},
			WS:     engine.WorkspaceDraw{Count: 1, T: 1},
		},
		CursorVisible: true, CursorW: 32, CursorH: 32,
	})
	if got.Overlay != nil || got.Primary != nil || got.Cursor || got.NeedDRM || got.Reason != "not kms" {
		t.Fatalf("nested must compose: %+v", got)
	}
}

func TestEvaluatePlanesChrome(t *testing.T) {
	a := winActor(10, 10, 100, 80)
	got := EvaluatePlanes(PlaneFrame{
		Frame: Frame{
			Backend: "vk-display", ScreenW: 800, ScreenH: 600,
			Actors:   []*engine.Actor{a},
			Overview: true,
			WS:       engine.WorkspaceDraw{Count: 1, T: 1},
		},
		CursorVisible: true, CursorW: 32, CursorH: 32, DisplayPlanes: 3,
	})
	if got.Overlay != nil || got.Cursor || got.Reason != "chrome" {
		t.Fatalf("%+v", got)
	}
}

func TestEvaluatePlanesTheaterSkipsOverlay(t *testing.T) {
	a := winActor(10, 10, 100, 80)
	a.Born = time.Now()
	got := EvaluatePlanes(PlaneFrame{
		Frame: Frame{
			Backend: "drm", ScreenW: 800, ScreenH: 600,
			Actors:  []*engine.Actor{a},
			Theater: engine.TierHigh,
			Now:     a.Born,
			WS:      engine.WorkspaceDraw{Count: 1, T: 1},
		},
	})
	if got.Overlay != nil {
		t.Fatalf("animating overlay: %+v", got)
	}
}

func TestEvaluatePlanesVKDisplayNeedDRM(t *testing.T) {
	a := winActor(20, 20, 200, 120)
	got := EvaluatePlanes(PlaneFrame{
		Frame: Frame{
			Backend: "vk-display", ScreenW: 800, ScreenH: 600, VendorID: VendorIntel,
			Actors: []*engine.Actor{a},
			WS:     engine.WorkspaceDraw{Count: 1, T: 1},
		},
		DisplayPlanes: 3,
	})
	if got.Overlay != a || !got.NeedDRM {
		t.Fatalf("eligible but NeedDRM: %+v", got)
	}
	empty := EvaluatePlanes(PlaneFrame{
		Frame: Frame{
			Backend: "vk-display", ScreenW: 800, ScreenH: 600,
			WS: engine.WorkspaceDraw{Count: 1, T: 1},
		},
		DisplayPlanes: 4,
	})
	if empty.Reason != "vk-display extra planes (need drm master)" {
		t.Fatalf("%+v", empty)
	}
}

func TestEvaluatePlanesShmNoOverlay(t *testing.T) {
	a := &engine.Actor{X: 10, Y: 10, Width: 100, Height: 80, BufW: 100, BufH: 80}
	got := EvaluatePlanes(PlaneFrame{
		Frame: Frame{
			Backend: "drm", ScreenW: 800, ScreenH: 600,
			Actors: []*engine.Actor{a},
			WS:     engine.WorkspaceDraw{Count: 1, T: 1},
		},
	})
	if got.Overlay != nil || got.Reason != "compose" {
		t.Fatalf("%+v", got)
	}
}
