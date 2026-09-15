package shell

import (
	"fmt"
	"strings"
	"testing"
)

func TestAutoPresentOrderNestedWhenWayland(t *testing.T) {
	o := AutoPresentOrder(true, false, false, true)
	if len(o) < 1 || o[0] != BackendWaylandClient {
		t.Fatalf("nested first %+v", o)
	}
	for _, b := range o {
		if b == BackendVKDisplay || b == BackendDRM {
			t.Fatal("must not steal display when WAYLAND_DISPLAY is set")
		}
	}
}

func TestAutoPresentOrderTTYPrefersVKDisplay(t *testing.T) {
	o := AutoPresentOrder(false, false, false, true)
	if len(o) < 2 || o[0] != BackendVKDisplay || o[1] != BackendDRM {
		t.Fatalf("tty %+v", o)
	}
	for _, b := range o {
		if b == BackendWaylandClient {
			t.Fatal("no host Wayland on a spare TTY")
		}
	}
}

func TestAutoPresentOrderNoDRMHeadless(t *testing.T) {
	o := AutoPresentOrder(false, false, false, false)
	if len(o) != 1 || o[0] != BackendHeadless {
		t.Fatalf("%+v", o)
	}
}

func TestAutoPresentOrderX11NoSteal(t *testing.T) {
	o := AutoPresentOrder(false, true, false, true)
	if len(o) != 1 || o[0] != BackendHeadless {
		t.Fatalf("%+v", o)
	}
}

func TestAutoPresentOrderTakeoverUsesDRM(t *testing.T) {
	o := AutoPresentOrder(true, true, true, true)
	if o[0] != BackendVKDisplay {
		t.Fatalf("takeover %+v", o)
	}
}

func TestCheckTakeoverAllowsSpareTTY(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	t.Setenv("XDG_SESSION_TYPE", "tty")
	if err := CheckTakeover(BackendVKDisplay, false); err != nil {
		t.Fatal(err)
	}
	if err := CheckTakeover(BackendDRM, false); err != nil {
		t.Fatal(err)
	}
}

func TestCheckTakeoverRefusesXDGSessionType(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	t.Setenv("XDG_SESSION_TYPE", "wayland")
	if err := CheckTakeover(BackendVKDisplay, false); err == nil {
		t.Fatal("expected refuse")
	}
	if err := CheckTakeover(BackendVKDisplay, true); err != nil {
		t.Fatal(err)
	}
}

func TestCheckTakeoverDRMRefusesSession(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("DISPLAY", "")
	t.Setenv("XDG_SESSION_TYPE", "")
	if err := CheckTakeover(BackendDRM, false); err == nil {
		t.Fatal("expected refuse")
	}
}

func TestHintVKDisplayMentionsTryTTY(t *testing.T) {
	err := hintVKDisplay(fmt.Errorf("vkCreateDisplayPlaneSurfaceKHR failed"))
	if err == nil || !strings.Contains(err.Error(), "try-tty.sh") || !strings.Contains(err.Error(), "F3") {
		t.Fatalf("expected TTY script hint: %v", err)
	}
}

func TestHintDRMMentionsTryTTY(t *testing.T) {
	err := hintDRM(fmt.Errorf("drmSetMaster failed"))
	if err == nil || !strings.Contains(err.Error(), "try-tty.sh") {
		t.Fatalf("%v", err)
	}
}

func TestSeatStringNeverEmpty(t *testing.T) {
	s := ProbeSeat()
	if s.String() == "" {
		t.Fatal("empty")
	}
}
