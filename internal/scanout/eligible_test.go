package scanout

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/engine"
)

func fullActor(w, h int) *engine.Actor {
	return &engine.Actor{
		X: 0, Y: 0, Width: w, Height: h,
		BufW: w, BufH: h,
		ScanFD: 7, ScanFourcc: FourccXRGB8888, ScanStride: uint32(w * 4),
		Workspace: 0,
	}
}

func TestKMSBackend(t *testing.T) {
	if !KMSBackend("vk-display") || !KMSBackend("drm") {
		t.Fatal("vk-display and drm are KMS")
	}
	for _, n := range []string{"wayland-client", "nested", "headless", ""} {
		if KMSBackend(n) {
			t.Fatalf("%s must not be KMS", n)
		}
	}
}

func TestScanoutFourcc(t *testing.T) {
	if !ScanoutFourcc(FourccARGB8888) || !ScanoutFourcc(FourccXRGB8888) {
		t.Fatal("ARGB/XRGB")
	}
	if ScanoutFourcc(0x34324241) { // ABGR8888
		t.Fatal("ABGR is CPU-only")
	}
	if !LinearMod(0) || !LinearMod(ModInvalid) || LinearMod(0x0100000000000009) {
		t.Fatal("linear vs 4-tiled")
	}
}

func TestCoversOutput(t *testing.T) {
	a := fullActor(1920, 1080)
	if !CoversOutput(a, 1920, 1080) {
		t.Fatal("exact cover")
	}
	a.X = 10
	if CoversOutput(a, 1920, 1080) {
		t.Fatal("offset")
	}
	a.X = 0
	a.Width = 800
	if CoversOutput(a, 1920, 1080) {
		t.Fatal("windowed")
	}
	a.Width = 1920
	a.BufW, a.BufH = 3840, 2160
	if CoversOutput(a, 1920, 1080) {
		t.Fatal("scaled buffer")
	}
	if CoversOutput(nil, 1920, 1080) || CoversOutput(a, 0, 0) {
		t.Fatal("empty")
	}
}

func TestEvaluateOK(t *testing.T) {
	a := fullActor(800, 600)
	r := Evaluate(Frame{
		Backend: "drm", ScreenW: 800, ScreenH: 600,
		Actors: []*engine.Actor{a},
		WS:     engine.WorkspaceDraw{Count: 1, Active: 0, From: 0, To: 0, T: 1},
	})
	if !r.OK || r.Actor != a || r.Reason != "ok" {
		t.Fatalf("%+v", r)
	}
	if !r.IntelFirst {
		t.Fatal("unknown vendor is Intel-first try")
	}
}

func TestEvaluateVKDisplayEligible(t *testing.T) {
	a := fullActor(800, 600)
	a.ScanFourcc = FourccARGB8888
	r := Evaluate(Frame{
		Backend: "vk-display", ScreenW: 800, ScreenH: 600, VendorID: VendorIntel,
		Actors: []*engine.Actor{a},
		WS:     engine.WorkspaceDraw{Count: 1, Active: 0, T: 1},
	})
	if !r.OK || !r.IntelFirst {
		t.Fatalf("%+v", r)
	}
}

func TestEvaluateNVIDIABestEffort(t *testing.T) {
	a := fullActor(800, 600)
	r := Evaluate(Frame{
		Backend: "drm", ScreenW: 800, ScreenH: 600, VendorID: VendorNVIDIA,
		Actors: []*engine.Actor{a},
		WS:     engine.WorkspaceDraw{Count: 1, Active: 0, T: 1},
	})
	if !r.OK {
		t.Fatalf("NVIDIA still tries: %+v", r)
	}
	if r.IntelFirst {
		t.Fatal("NVIDIA is best-effort, not Intel-first")
	}
	r = Evaluate(Frame{
		Backend: "drm", ScreenW: 800, ScreenH: 600, VendorID: VendorAMD,
		Actors: []*engine.Actor{a},
		WS:     engine.WorkspaceDraw{Count: 1, Active: 0, T: 1},
	})
	if r.IntelFirst || !r.OK {
		t.Fatalf("AMD best-effort: %+v", r)
	}
}

func TestEvaluateRejects(t *testing.T) {
	a := fullActor(800, 600)
	ws := engine.WorkspaceDraw{Count: 1, Active: 0, T: 1}
	base := func() Frame {
		return Frame{Backend: "drm", ScreenW: 800, ScreenH: 600, Actors: []*engine.Actor{a}, WS: ws}
	}

	cases := []struct {
		mut    func(*Frame)
		reason string
	}{
		{func(f *Frame) { f.Backend = "wayland-client" }, "nested present"},
		{func(f *Frame) { f.Backend = "nested" }, "nested present"},
		{func(f *Frame) { f.Backend = "headless" }, "headless present"},
		{func(f *Frame) { f.Backend = "auto" }, "not kms backend"},
		{func(f *Frame) { f.Overview = true }, "overview"},
		{func(f *Frame) { f.Launcher = true }, "launcher"},
		{func(f *Frame) {
			f.WS = engine.WorkspaceDraw{Count: 3, Active: 1, From: 0, To: 1, Dir: 1, T: 0.4}
		}, "workspace slide"},
		{func(f *Frame) { f.Actors = []*engine.Actor{a, fullActor(800, 600)} }, "not single visible"},
		{func(f *Frame) {
			pop := &engine.Actor{X: 10, Y: 10, Width: 40, Height: 20, NoChrome: true, Workspace: 0}
			f.Actors = []*engine.Actor{a, pop}
		}, "not single visible"},
		{func(f *Frame) {
			w := *a
			w.Width, w.Height, w.BufW, w.BufH = 400, 300, 400, 300
			f.Actors = []*engine.Actor{&w}
		}, "no fullscreen dmabuf"},
		{func(f *Frame) {
			s := *a
			s.ScanFD = 0
			f.Actors = []*engine.Actor{&s}
		}, "no fullscreen dmabuf"},
		{func(f *Frame) {
			s := *a
			s.ScanFourcc = 0x34324241
			f.Actors = []*engine.Actor{&s}
		}, "no fullscreen dmabuf"},
		{func(f *Frame) { f.Actors = nil }, "not single visible"},
		{func(f *Frame) {
			shm := &engine.Actor{X: 0, Y: 0, Width: 800, Height: 600, BufW: 800, BufH: 600}
			f.Actors = []*engine.Actor{shm}
		}, "no fullscreen dmabuf"},
	}
	for _, tc := range cases {
		f := base()
		tc.mut(&f)
		r := Evaluate(f)
		if r.OK || r.Reason != tc.reason {
			t.Fatalf("want %q, got %+v", tc.reason, r)
		}
		if r.Actor != nil {
			t.Fatalf("actor set on fail %q", tc.reason)
		}
	}
}

func TestEvaluateTheater(t *testing.T) {
	a := fullActor(800, 600)
	a.Born = time.Now()
	r := Evaluate(Frame{
		Backend: "drm", ScreenW: 800, ScreenH: 600,
		Actors:  []*engine.Actor{a},
		Theater: engine.TierHigh,
		Now:     a.Born,
		WS:      engine.WorkspaceDraw{Count: 1, Active: 0, T: 1},
	})
	if r.OK || r.Reason != "theater" {
		t.Fatalf("%+v", r)
	}
	r = Evaluate(Frame{
		Backend: "drm", ScreenW: 800, ScreenH: 600,
		Actors:  []*engine.Actor{a},
		Theater: engine.TierOff,
		Now:     a.Born,
		WS:      engine.WorkspaceDraw{Count: 1, Active: 0, T: 1},
	})
	if !r.OK {
		t.Fatalf("theater off is identity: %+v", r)
	}
}

func TestEvaluateOtherWorkspaceHidden(t *testing.T) {
	a := fullActor(800, 600)
	a.Workspace = 0
	other := fullActor(800, 600)
	other.Workspace = 1
	other.ScanFD = 8
	r := Evaluate(Frame{
		Backend: "vk-display", ScreenW: 800, ScreenH: 600,
		Actors: []*engine.Actor{a, other},
		WS:     engine.WorkspaceDraw{Count: 3, Active: 0, From: 0, To: 0, T: 1},
	})
	if !r.OK || r.Actor != a {
		t.Fatalf("hidden other workspace: %+v", r)
	}
}

func TestSliding(t *testing.T) {
	if Sliding(engine.WorkspaceDraw{Count: 3, Active: 1, From: 1, To: 1, T: 1}) {
		t.Fatal("settled")
	}
	if !Sliding(engine.WorkspaceDraw{Count: 3, From: 0, To: 1, Dir: 1, T: 0.5}) {
		t.Fatal("mid-slide")
	}
}
