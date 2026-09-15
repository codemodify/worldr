package shell

import (
	"fmt"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/engine"
)

func TestParseColor(t *testing.T) {
	c, err := ParseColor("#0b1020")
	if err != nil {
		t.Fatal(err)
	}
	if c[3] != 1 {
		t.Fatalf("alpha=%v", c[3])
	}
	p := PackBGRA(c)
	if p&0xff000000 == 0 {
		t.Fatalf("packed %#x missing alpha", p)
	}
}

func TestCheckTakeoverHeadlessOK(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	t.Setenv("XDG_SESSION_TYPE", "")
	if err := CheckTakeover(BackendHeadless, false); err != nil {
		t.Fatal(err)
	}
	if err := CheckTakeover(BackendWaylandClient, false); err != nil {
		t.Fatal(err)
	}
	if err := CheckTakeover(BackendNested, false); err != nil {
		t.Fatal(err)
	}
	if err := CheckTakeover(BackendAuto, false); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	if err := CheckTakeover(BackendAuto, false); err != nil {
		t.Fatal("auto + host Wayland must stay nested, not refuse")
	}
}

func TestCheckTakeoverVKDisplayRefusesSession(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("DISPLAY", "")
	if err := CheckTakeover(BackendVKDisplay, false); err == nil {
		t.Fatal("expected refuse")
	}
	if err := CheckTakeover(BackendVKDisplay, true); err != nil {
		t.Fatal(err)
	}
}

func TestParseEffectsFlag(t *testing.T) {
	o, err := ParseFlags([]string{"-effects=off"})
	if err != nil || o.Effects != engine.TierOff {
		t.Fatalf("%+v %v", o, err)
	}
	o, err = ParseFlags([]string{"-effects=low"})
	if err != nil || o.Effects != engine.TierLow {
		t.Fatalf("low %+v %v", o, err)
	}
	o, err = ParseFlags([]string{})
	if err != nil || o.Effects != engine.TierHigh {
		t.Fatalf("default %+v %v", o, err)
	}
}

func TestParseOverviewDemoFlag(t *testing.T) {
	o, err := ParseFlags([]string{"-overview-demo"})
	if err != nil || !o.OverviewDemo {
		t.Fatalf("%+v %v", o, err)
	}
}

func TestParseScaleFlag(t *testing.T) {
	o, err := ParseFlags([]string{})
	if err != nil || o.Scale != 0 || ResolveOutputScale(o.Scale, 0) != 1 {
		t.Fatalf("default %+v %v", o, err)
	}
	if ResolveOutputScale(0, 1.5) != 1.5 {
		t.Fatal("host auto")
	}
	if ResolveOutputScale(2, 1.5) != 2 {
		t.Fatal("explicit overrides host")
	}
	o, err = ParseFlags([]string{"-scale=1.5"})
	if err != nil || o.Scale != 1.5 || ResolveOutputScale(o.Scale, 2) != 1.5 {
		t.Fatalf("1.5 %+v %v", o, err)
	}
	if _, err := ParseFlags([]string{"-scale=-1"}); err == nil {
		t.Fatal("negative scale")
	}
}

func TestParseOutputsFlags(t *testing.T) {
	o, err := ParseFlags([]string{})
	if err != nil || o.Outputs != 1 || len(o.OutputScales) != 0 {
		t.Fatalf("default %+v %v", o, err)
	}
	o, err = ParseFlags([]string{"-outputs=2", "-output-scales=1,1.5"})
	if err != nil || o.Outputs != 2 || len(o.OutputScales) != 2 || o.OutputScales[1] != 1.5 {
		t.Fatalf("two %+v %v", o, err)
	}
	o, err = ParseFlags([]string{"-outputs=9"})
	if err != nil || o.Outputs != 4 {
		t.Fatalf("clamp %+v %v", o, err)
	}
	if _, err := ParseFlags([]string{"-output-scales=nope"}); err == nil {
		t.Fatal("bad scales")
	}
}

func TestParseXWaylandFlag(t *testing.T) {
	o, err := ParseFlags([]string{"-xwayland", "-backend=nested"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.XWayland || o.Backend != BackendNested {
		t.Fatalf("%+v", o)
	}
}

func TestParseBackendNested(t *testing.T) {
	o, err := ParseFlags([]string{"-backend=nested", "-width=800", "-height=600"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Backend != BackendNested || !o.Compositor {
		t.Fatalf("backend=%s compositor=%v", o.Backend, o.Compositor)
	}
}

func TestParseBackendReject(t *testing.T) {
	_, err := ParseFlags([]string{"-backend=wgpu"})
	if err == nil {
		t.Fatal("expected reject")
	}
}

func TestHintWaylandClientMentionsKWin(t *testing.T) {
	err := hintWaylandClient(fmt.Errorf("write unix @: sendmsg: broken pipe"))
	if err == nil || !strings.Contains(err.Error(), "KWin/Plasma") {
		t.Fatalf("expected KWin hint: %v", err)
	}
}
