package native

import "testing"

func TestAvailableMatchesBuild(t *testing.T) {
	// Compiling this package is the test. On linux+cgo Available is true
	// even when no GPU is present; OpenVK may still fail at runtime.
	t.Logf("native.Available() = %v", Available())
}

func TestPlanesOnlyClosed(t *testing.T) {
	var d *DRM
	if d.PlanesOnly() {
		t.Fatal("nil")
	}
	d = &DRM{planesOnly: true}
	if !d.PlanesOnly() {
		t.Fatal("flag")
	}
	if _, err := OpenVKOnDRM(nil); err == nil {
		t.Fatal("nil drm")
	}
	if _, err := OpenVKOnDRM(&DRM{}); err == nil {
		t.Fatal("closed drm")
	}
}

func TestListDevicesNoCrash(t *testing.T) {
	s, err := ListDevices()
	if !Available() {
		if err == nil {
			t.Fatal("expected error without linux+cgo")
		}
		return
	}
	if err != nil {
		t.Logf("list devices (ok to fail without ICD/GPU): %v", err)
		return
	}
	t.Logf("devices:\n%s", s)
}
