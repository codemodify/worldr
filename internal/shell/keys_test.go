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
