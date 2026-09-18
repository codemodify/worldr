//go:build linux && cgo

package app

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

func TestApplicationConfigureFailureClosesBeforeLaunch(t *testing.T) {
	want := errors.New("configuration fixture")
	var socket string
	_, err := openApplicationController(io.Discard, func(a *applicationController) error {
		socket = a.socket
		if a.x11 != nil || len(a.children) > 0 {
			t.Fatal("configuration ran after external process startup")
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("configuration error lost: %v", err)
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatal("configuration failure left a private display socket")
	}
}

func TestRealX11ControllerProfileAndNativeShellEnvironment(t *testing.T) {
	if os.Getenv("WORLDR_TEST_XWAYLAND") != "1" {
		t.Skip("set WORLDR_TEST_XWAYLAND=1 for isolated real X11 acceptance")
	}
	for _, name := range []string{"Xwayland", "xmessage"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip(err)
		}
	}
	var output bytes.Buffer
	configured := false
	a, err := launchApplication(Options{Launches: []ApplicationLaunch{{ID: "legacy-note", Command: "xmessage", X11: true, Args: []string{"-title", "Controller X11", "-geometry", "320x160", "controller integration"}}}}, &output, func(a *applicationController) error { configured = a.x11 == nil && len(a.children) == 0; return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if !configured {
		t.Fatal("renderer configuration ran after clients")
	}
	until := func(check func([]experience.ApplicationSurface) bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if err := a.Poll(); err != nil {
				t.Fatal(err)
			}
			if check(a.Surfaces()) {
				return
			}
			time.Sleep(3 * time.Millisecond)
		}
		t.Fatalf("X11 controller timed out: %s", output.String())
	}
	var id uint64
	until(func(s []experience.ApplicationSurface) bool {
		if len(s) == 1 {
			id = s[0].ID
			return true
		}
		return false
	})
	s := a.Surfaces()[0]
	if s.Key != "legacy-note/window-1" || s.Title != "Controller X11" {
		t.Fatalf("stable X11 profile identity: %+v", s)
	}
	w, ok := a.x11.Window(id)
	if !ok || w.PID != uint32(a.children[0].cmd.Process.Pid) {
		t.Fatalf("XRes local client PID: %+v", w)
	}
	env := a.terminalEnvironment(os.Environ())
	for _, key := range []string{"DISPLAY=", "XAUTHORITY=", "WAYLAND_DISPLAY="} {
		found := false
		for _, entry := range env {
			if strings.HasPrefix(entry, key) && len(entry) > len(key) {
				found = true
			}
		}
		if !found {
			t.Fatalf("native terminal lacks private %s", key)
		}
	}
	// A GUI command launched by a shell receives the same private display and
	// is discoverable even though it is not one of the CLI launch processes.
	child := exec.Command("/bin/sh", "-c", "exec xmessage -title 'From native environment' -geometry 300x140 'private shell display'")
	child.Env = env
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	defer func() {
		child.Process.Kill()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}()
	until(func(s []experience.ApplicationSurface) bool { return len(s) == 2 })
	a.Focus(id)
	a.Resize(id, 440, 190)
	until(func(s []experience.ApplicationSurface) bool {
		for _, v := range s {
			if v.ID == id {
				w, h := v.Texture.Size()
				return w == 440 && h == 190
			}
		}
		return false
	})
	a.CloseApplication(id)
	until(func(s []experience.ApplicationSurface) bool { return len(s) == 1 && s[0].ID != id })
	remaining := a.Surfaces()[0].ID
	a.CloseApplication(remaining)
	until(func(s []experience.ApplicationSurface) bool { return len(s) == 0 })
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}
