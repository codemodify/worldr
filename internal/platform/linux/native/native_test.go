package native

import "testing"

func TestAvailableMatchesBuild(t *testing.T) {
	// Compiling this package is the test. On linux+cgo Available is true
	// even when no GPU is present; OpenVK may still fail at runtime.
	t.Logf("native.Available() = %v", Available())
}

func TestScanoutClosedSession(t *testing.T) {
	var d *DRM
	if err := d.ScanoutDMABuf(3, 64, 64, 0x34325258, 0, 0, 256); err == nil {
		t.Fatal("nil DRM must refuse scanout")
	}
	d = &DRM{}
	if err := d.ScanoutDMABuf(3, 64, 64, 0x34325258, 0, 0, 256); err == nil {
		t.Fatal("closed DRM must refuse scanout")
	}
	if d.ScanoutActive() {
		t.Fatal("closed is not active")
	}
	if err := d.RestoreScanout(); err == nil && Available() {
		t.Fatal("closed restore")
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
