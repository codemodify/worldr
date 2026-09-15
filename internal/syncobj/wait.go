package syncobj

import (
	"fmt"
	"time"
)

// Waiter is an optional Vulkan timeline wait (VK_KHR_external_semaphore_fd).
// Implementations fall through to WaitFence when this returns an error.
type Waiter interface {
	HasTimeline() bool
	WaitTimeline(fd int, point uint64, timeoutNS uint64) error
}

// WaitAcquire waits for an acquire timeline point. Vulkan import +
// vkWaitSemaphores is preferred when w is a live timeline device; DRM
// SYNCOBJ_TIMELINE ioctl is the implicit-sync fallback (Intel).
func WaitAcquire(f Fence, timeout time.Duration, w Waiter) error {
	if !f.Valid() {
		return fmt.Errorf("no syncobj fd")
	}
	if timeout <= 0 {
		timeout = 100 * time.Millisecond
	}
	if w != nil && w.HasTimeline() {
		if err := w.WaitTimeline(f.FD, f.Point, uint64(timeout.Nanoseconds())); err == nil {
			return nil
		}
	}
	return WaitFence(f, timeout)
}
