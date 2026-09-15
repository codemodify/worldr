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
