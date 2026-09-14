package xwayland

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseDisplayFD(t *testing.T) {
	n, err := ParseDisplayFD(strings.NewReader("2\n"))
	if err != nil || n != 2 {
		t.Fatalf("got %d %v", n, err)
	}
	if _, err := ParseDisplayFD(strings.NewReader("nope\n")); err == nil {
		t.Fatal("expected error")
	}
	if _, err := ParseDisplayFD(strings.NewReader("999\n")); err == nil {
		t.Fatal("expected range error")
	}
}

func TestFilterEnvDropsDISPLAY(t *testing.T) {
	got := filterEnv([]string{"FOO=1", "DISPLAY=:0", "BAR=2"}, "DISPLAY")
	if len(got) != 2 || got[0] != "FOO=1" || got[1] != "BAR=2" {
		t.Fatalf("%v", got)
	}
	got = filterEnv([]string{"WAYLAND_DISPLAY=wayland-0", "XDG_RUNTIME_DIR=/run/user/1"}, "WAYLAND_DISPLAY")
	if len(got) != 1 || got[0] != "XDG_RUNTIME_DIR=/run/user/1" {
		t.Fatalf("%v", got)
	}
}

func TestStartFakeXwayland(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "Xwayland")
	// ExtraFiles[0] is fd 3. Write display number and hang until killed.
	body := "#!/bin/sh\nprintf '7\\n' >&3\nexec sleep 30\n"
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORLDR_XWAYLAND", script)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	in, err := Start("wayland-test", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.Display != ":7" || in.DisplayNum != 7 {
		t.Fatalf("display %s", in.Display)
	}
	// Close should not hang
	done := make(chan struct{})
	go func() {
		in.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close hung")
	}
}

func TestLookPathMissing(t *testing.T) {
	t.Setenv("WORLDR_XWAYLAND", "/no/such/Xwayland")
	if _, err := LookPath(); err == nil {
		t.Fatal("expected error")
	}
}

func TestStartRequiresDisplay(t *testing.T) {
	if _, err := Start("", "/tmp", nil); err == nil {
		t.Fatal("expected error")
	}
}
