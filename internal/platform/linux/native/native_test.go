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

func TestWaitTimelineClosedSession(t *testing.T) {
	var v *VK
	if err := v.WaitTimeline(3, 1, 1000); err == nil {
		t.Fatal("nil VK must refuse timeline wait")
	}
	v = &VK{}
	if err := v.WaitTimeline(3, 1, 1000); err == nil {
		t.Fatal("closed VK must refuse timeline wait")
	}
	if v.HasTimeline() || v.DisplayPlanes() != 0 {
		t.Fatal("closed has no timeline / planes")
	}
}

func TestOverlayClosedSession(t *testing.T) {
	var d *DRM
	if err := d.OverlayDMABuf(3, 64, 64, 0x34325258, 0, 0, 256, 10, 10); err == nil {
		t.Fatal("nil DRM overlay")
	}
	d = &DRM{}
	if err := d.OverlayDMABuf(3, 64, 64, 0x34325258, 0, 0, 256, 10, 10); err == nil {
		t.Fatal("closed DRM overlay")
	}
	if err := d.CursorARGB(0, 0, 32, 32, []byte{1, 2, 3, 4}, 16); err == nil {
		t.Fatal("closed cursor")
	}
	ov, cu, _, _ := d.PlaneCaps()
	if ov || cu {
		t.Fatal("closed has no planes")
	}
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
	if err := d.ScanoutDMABuf(3, 64, 64, 0x34325258, 0, 0, 256); err == nil {
		t.Fatal("planes-only must refuse primary")
	}
	if _, err := OpenVKOnDRM(nil); err == nil {
		t.Fatal("nil drm")
	}
	if _, err := OpenVKOnDRM(&DRM{}); err == nil {
		t.Fatal("closed drm")
	}
}

func TestOpenDRMPlanesNoCrash(t *testing.T) {
	if !Available() {
		if _, err := OpenDRMPlanes(""); err == nil {
			t.Fatal("stub")
		}
		return
	}
	d, err := OpenDRMPlanes("")
	if err != nil {
		t.Logf("planes sidecar (ok without master): %v", err)
		return
	}
	defer d.Close()
	if !d.PlanesOnly() {
		t.Fatal("must be planes-only")
	}
	if err := d.ScanoutDMABuf(3, 64, 64, 0x34325258, 0, 0, 256); err == nil {
		t.Fatal("sidecar must refuse primary scanout")
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
