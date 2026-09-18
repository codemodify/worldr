//go:build linux && cgo

package xwayland

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/platform/linux/dmabuf"
)

func TestXwaylandTransportRequiresExactGPUCapabilities(t *testing.T) {
	compatible := []dmabuf.Format{
		{FourCC: dmabuf.XRGB8888, Modifier: 0x0100000000000002},
		{FourCC: dmabuf.ARGB8888, Modifier: 0x0100000000000002},
	}
	for name, test := range map[string]struct {
		formats            []dmabuf.Format
		node               string
		glamor, accessible bool
		want               xwaylandTransport
	}{
		"all capabilities":       {compatible, "/dev/dri/renderD128", true, true, transportGlamor},
		"linear is explicit":     {[]dmabuf.Format{{FourCC: dmabuf.XRGB8888}, {FourCC: dmabuf.ARGB8888}}, "/dev/dri/renderD128", true, true, transportGlamor},
		"missing render node":    {compatible, "", true, true, transportSHM},
		"unsupported executable": {compatible, "/dev/dri/renderD128", false, true, transportSHM},
		"inaccessible node":      {compatible, "/dev/dri/renderD128", true, false, transportSHM},
		"missing alpha format":   {compatible[:1], "/dev/dri/renderD128", true, true, transportSHM},
		"different modifiers": {[]dmabuf.Format{{FourCC: dmabuf.XRGB8888, Modifier: 7},
			dmabuf.Format{FourCC: dmabuf.ARGB8888, Modifier: 8}}, "/dev/dri/renderD128", true, true, transportSHM},
		"implicit modifier": {[]dmabuf.Format{{FourCC: dmabuf.XRGB8888, Modifier: dmabuf.Invalid},
			dmabuf.Format{FourCC: dmabuf.ARGB8888, Modifier: dmabuf.Invalid}}, "/dev/dri/renderD128", true, true, transportSHM},
	} {
		t.Run(name, func(t *testing.T) {
			if got := chooseXwaylandTransport(test.formats, test.node, test.glamor, test.accessible); got != test.want {
				t.Fatalf("transport %d, want %d", got, test.want)
			}
		})
	}
}

func TestXwaylandArgumentsAndFallbackDetection(t *testing.T) {
	software := xwaylandArguments("/tmp/auth", transportSHM)
	glamor := xwaylandArguments("/tmp/auth", transportGlamor)
	if !slices.Contains(software, "-shm") || slices.Contains(software, "-glamor") {
		t.Fatalf("software arguments: %q", software)
	}
	if slices.Contains(glamor, "-shm") || !slices.Contains(glamor, "-glamor") {
		t.Fatalf("glamor arguments: %q", glamor)
	}
	if !glamorFellBack("Xwayland glamor: failed to setup GBM backend, falling back to sw accel") ||
		!glamorFellBack("Failed to initialize glamor, falling back to sw") ||
		glamorFellBack("glamor initialized on /dev/dri/renderD128") {
		t.Fatal("glamor fallback log detection")
	}
}

func TestAcceleratedEnvironmentPinsFeedbackDevice(t *testing.T) {
	parent := []string{"DRI_PRIME=1", "KEEP=yes"}
	accelerated := xwaylandEnvironment(parent, "wayland", ":4", "auth", true)
	software := xwaylandEnvironment(parent, "wayland", ":4", "auth", false)
	if strings.Contains(strings.Join(accelerated, "\n"), "DRI_PRIME=") {
		t.Fatalf("accelerated environment retained DRI_PRIME: %q", accelerated)
	}
	if !slices.Contains(software, "DRI_PRIME=1") {
		t.Fatalf("software environment changed unrelated GPU selection: %q", software)
	}
	path := t.TempDir() + "/renderD128"
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if renderNodeUsable(path) {
		t.Fatal("regular file accepted as DRM render node")
	}
}
