package shell

import "testing"

func TestIsEvdevAcceptsWaylandOffset(t *testing.T) {
	if !isEvdev(1, keyEsc) || !isEvdev(9, keyEsc) {
		t.Fatal("esc")
	}
	if !isEvdev(88, keyF12) || !isEvdev(96, keyF12) {
		t.Fatal("f12")
	}
	if isEvdev(2, keyEsc) {
		t.Fatal("1 is not 2")
	}
}

func TestOverviewToggleKeys(t *testing.T) {
	if !isOverviewToggle(88, false) || !isOverviewToggle(96, false) {
		t.Fatal("F12")
	}
	if isOverviewToggle(15, false) {
		t.Fatal("bare tab is not toggle")
	}
	if !isOverviewToggle(15, true) || !isOverviewToggle(23, true) {
		t.Fatal("Super+Tab")
	}
}

func TestCtrlAltEvdevOffset(t *testing.T) {
	if !isCtrl(29) || !isCtrl(37) || !isAlt(56) || !isAlt(64) {
		t.Fatal("ctrl/alt evdev+8")
	}
}

func TestLauncherToggleKeys(t *testing.T) {
	if !isLauncherToggle(59, false) || !isLauncherToggle(67, false) {
		t.Fatal("F1")
	}
	if isLauncherToggle(57, false) {
		t.Fatal("bare space is not toggle")
	}
	if !isLauncherToggle(57, true) || !isLauncherToggle(65, true) {
		t.Fatal("Super+Space")
	}
}
