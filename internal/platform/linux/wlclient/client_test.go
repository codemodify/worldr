//go:build linux

package wlclient

import (
	"errors"
	"io"
	"strings"
	"syscall"
	"testing"
)

func TestDisplaySocketAbsolute(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "/tmp/wayland-abs")
	got, err := displaySocket()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/wayland-abs" {
		t.Fatalf("got %s", got)
	}
}

func TestDisplaySocketJoin(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	got, err := displaySocket()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/run/user/1000/wayland-0" {
		t.Fatalf("got %s", got)
	}
}

func TestDisplaySocketMissing(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	if _, err := displaySocket(); err == nil {
		t.Fatal("expected error")
	}
}

func TestBindVersionCaps(t *testing.T) {
	if v := bindVersion("wl_compositor", 10); v != 6 {
		t.Fatalf("compositor %d", v)
	}
	if v := bindVersion("xdg_wm_base", 3); v != 3 {
		t.Fatalf("xdg %d", v)
	}
	if v := bindVersion("wl_shm", 1); v != 1 {
		t.Fatalf("shm %d", v)
	}
}

func TestWrapHostCloseMentionsKWin(t *testing.T) {
	err := wrapHostClose(syscall.EPIPE)
	if err == nil || !strings.Contains(err.Error(), "KWin/Plasma") {
		t.Fatalf("hint missing: %v", err)
	}
	if !isBrokenPipe(syscall.EPIPE) {
		t.Fatal("EPIPE should match")
	}
	if isBrokenPipe(io.EOF) {
		t.Fatal("EOF is not a pipe error")
	}
	if !errors.Is(wrapHostClose(syscall.EPIPE), syscall.EPIPE) {
		t.Fatal("should unwrap")
	}
}
