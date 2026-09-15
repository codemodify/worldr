package shell

import (
	"strings"
	"syscall"
	"testing"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"golang.org/x/sys/unix"
)

type recWaiter struct {
	hits int
	fd   int
	pt   uint64
}

func (r *recWaiter) HasTimeline() bool { return true }

func (r *recWaiter) WaitTimeline(fd int, point uint64, timeoutNS uint64) error {
	r.hits++
	r.fd, r.pt = fd, point
	return nil
}

func TestAcquireWaiterNilWithoutVK(t *testing.T) {
	if p := (&presenter{name: string(BackendDRM)}); p.acquireWaiter() != nil {
		t.Fatal("drm without vk must be ioctl-only")
	}
	if (*presenter)(nil).acquireWaiter() != nil {
		t.Fatal("nil presenter")
	}
}

func TestAcquireWaiterUsesPresenterVK(t *testing.T) {
	vk := &native.VK{}
	p := &presenter{name: string(BackendDRM), vk: vk}
	if p.acquireWaiter() != vk {
		t.Fatal("drm+vk must wait on the offscreen session")
	}
	p.name = string(BackendVKDisplay)
	if p.acquireWaiter() != vk {
		t.Fatal("vk-display")
	}
	p.name = string(BackendWaylandClient)
	if p.acquireWaiter() != vk {
		t.Fatal("nested")
	}
	p.name = string(BackendHeadless)
	if p.acquireWaiter() != vk {
		t.Fatal("headless")
	}
}

func TestWaitActorAcquireInvokesWaiter(t *testing.T) {
	fd, err := unix.MemfdCreate("t-present-acq", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	w := &recWaiter{}
	waitActorAcquire(&engine.Actor{AcqFD: fd, AcqPoint: 5}, w)
	if w.hits != 1 || w.fd != fd || w.pt != 5 {
		t.Fatalf("present path must Vulkan-wait: %+v", w)
	}
	waitActorAcquire(&engine.Actor{}, w)
	waitActorAcquire(nil, w)
	if w.hits != 1 {
		t.Fatal("no fd must not wait")
	}
}

func TestAttachOffscreenVKIdempotent(t *testing.T) {
	if err := attachOffscreenVK(nil); err != nil {
		t.Fatal(err)
	}
	p := &presenter{}
	existing := &native.VK{}
	p.vk = existing
	if err := attachOffscreenVK(p); err != nil {
		t.Fatal(err)
	}
	if p.vk != existing {
		t.Fatal("must not replace an existing session")
	}
}

func TestAttachOffscreenVKOpensOrExplains(t *testing.T) {
	p := &presenter{note: "drm"}
	err := attachOffscreenVK(p)
	if !native.Available() {
		if err == nil || !strings.Contains(err.Error(), "cgo") {
			t.Fatalf("stub: %v", err)
		}
		return
	}
	if err != nil {
		if p.vk != nil {
			t.Fatal("failed open must leave vk nil")
		}
		return
	}
	if p.vk == nil {
		t.Fatal("success must set vk")
	}
	p.vk.Close()
}
