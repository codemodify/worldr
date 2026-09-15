//go:build !linux

package syncobj

import (
	"fmt"
	"time"
)

func closeFD(fd int) error { return nil }

// TimelineAvailable is false without linux DRM.
func TimelineAvailable() bool { return false }

// WaitFence is a documented no-op off linux.
func WaitFence(f Fence, timeout time.Duration) error {
	return fmt.Errorf("drm syncobj wait needs linux")
}

// SignalFence is a documented no-op off linux.
func SignalFence(f Fence) error {
	return fmt.Errorf("drm syncobj signal needs linux")
}
