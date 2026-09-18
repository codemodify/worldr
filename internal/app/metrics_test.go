package app

import (
	"errors"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"io"
	"testing"
	"time"
)

func TestMetricsCountWholeRunWithBoundedPercentileBins(t *testing.T) {
	var m durationMetric
	for i := 0; i < 9500; i++ {
		m.add(12 * time.Millisecond)
	}
	for i := 0; i < 400; i++ {
		m.add(20 * time.Millisecond)
	}
	for i := 0; i < 100; i++ {
		m.add(1500 * time.Millisecond)
	}
	r := m.report()
	if r.Samples != 10000 || r.P50MS < 12 || r.P50MS > 12.1 || r.P95MS > 12.1 || r.P99MS < 20 || r.P99MS > 20.1 || r.MaxMS != 1500 {
		t.Fatalf("incorrect sustained timing summary: %+v", r)
	}
}
func TestRecoveryBudgetStopsPersistentFailuresAndAllowsLaterFault(t *testing.T) {
	var guard graphicsRecovery
	now := time.Now()
	if !guard.allow(now) || !guard.allow(now.Add(time.Second)) || guard.allow(now.Add(2*time.Second)) || !guard.allow(now.Add(61*time.Second)) {
		t.Fatal("recovery retry limit failed")
	}
	for _, err := range []error{native.ErrDeviceLost, native.ErrNeedsRecovery, native.ErrGPUTimeout} {
		if !recoverableGraphics(err) {
			t.Fatal("device error not recoverable", err)
		}
	}
	if recoverableGraphics(native.ErrOutOfMemory) || recoverableGraphics(errors.New("application failed")) {
		t.Fatal("ordinary failure triggered device rebuild")
	}
}
func TestPerformanceOptionsProtectWorkspaceDocument(t *testing.T) {
	for _, args := range [][]string{{"--metrics=/tmp/a.json", "--state=/tmp/a.json"}, {"--gpu-memory-mib=1"}, {"--gpu-memory-mib=8193"}} {
		if _, err := Parse(args, io.Discard); err == nil {
			t.Fatal("accepted invalid performance options", args)
		}
	}
	if _, err := Parse([]string{"--metrics=/tmp/worldr-metrics.json", "--gpu-memory-mib=256"}, io.Discard); err != nil {
		t.Fatal(err)
	}
}
