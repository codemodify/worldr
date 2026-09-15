package syncobj

import (
	"fmt"
	"testing"
	"time"
)

type fakeWaiter struct {
	ok   bool
	hits int
	fd   int
	pt   uint64
}

func (f *fakeWaiter) HasTimeline() bool { return f.ok }

func (f *fakeWaiter) WaitTimeline(fd int, point uint64, timeoutNS uint64) error {
	f.hits++
	f.fd, f.pt = fd, point
	if !f.ok {
		return fmt.Errorf("no timeline")
	}
	if timeoutNS == 0 {
		return fmt.Errorf("zero timeout")
	}
	return nil
}

func TestWaitAcquireVulkanWins(t *testing.T) {
	w := &fakeWaiter{ok: true}
	if err := WaitAcquire(Fence{FD: 5, Point: 9}, 10*time.Millisecond, w); err != nil {
		t.Fatal(err)
	}
	if w.hits != 1 || w.fd != 5 || w.pt != 9 {
		t.Fatalf("%+v", w)
	}
}

func TestWaitAcquireFallsBackWhenVulkanOff(t *testing.T) {
	w := &fakeWaiter{ok: false}
	err := WaitAcquire(Fence{FD: 5, Point: 1}, time.Millisecond, w)
	if err == nil {
		t.Fatal("expected DRM fallback error without a real syncobj")
	}
	if w.hits != 0 {
		t.Fatal("HasTimeline false must not call WaitTimeline")
	}
}

func TestWaitAcquireEmpty(t *testing.T) {
	if err := WaitAcquire(Fence{}, 0, nil); err == nil {
		t.Fatal("empty")
	}
}
