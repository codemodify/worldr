//go:build linux && cgo

package xwayland

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/platform/linux/apps"
)

func TestDisplayNumberAndAuthority(t *testing.T) {
	for _, s := range []string{"", "1", "-1\n", "1x\n", "65536\n", strings.Repeat("9", 128) + "\n"} {
		if _, err := readDisplay(strings.NewReader(s)); err == nil {
			t.Errorf("accepted invalid display %q", s)
		}
	}
	if n, err := readDisplay(strings.NewReader("123\n")); err != nil || n != "123" {
		t.Fatalf("display %q: %v", n, err)
	}
	path := filepath.Join(t.TempDir(), "auth")
	cookie := bytes.Repeat([]byte{0xA5}, 16)
	if err := writeAuthority(path, "27", cookie); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("authority permissions: %v %v", info, err)
	}
	data, _ := os.ReadFile(path)
	r := bytes.NewReader(data)
	var family uint16
	binary.Read(r, binary.BigEndian, &family)
	if family != 65535 {
		t.Fatal("unexpected family")
	}
	for _, want := range [][]byte{nil, []byte("27"), []byte("MIT-MAGIC-COOKIE-1"), cookie} {
		var n uint16
		binary.Read(r, binary.BigEndian, &n)
		p := make([]byte, n)
		io.ReadFull(r, p)
		if !bytes.Equal(p, want) {
			t.Fatalf("authority field %x, want %x", p, want)
		}
	}
}

func TestRealXwaylandXmessage(t *testing.T) {
	if os.Getenv("WORLDR_TEST_XWAYLAND") != "1" {
		t.Skip("set WORLDR_TEST_XWAYLAND=1 for isolated Xwayland/X11 acceptance")
	}
	if _, err := exec.LookPath("Xwayland"); err != nil {
		t.Skip(err)
	}
	path, err := exec.LookPath("xmessage")
	if err != nil {
		t.Skip(err)
	}
	server, err := apps.Open(900, 560)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	bridge, err := Open(server, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	// A socket exists globally for ordinary Xlib clients, but an unauthenticated
	// connection must fail even for a peer running as the same Unix user.
	conn, err := net.DialTimeout("unix", "/tmp/.X11-unix/X"+strings.TrimPrefix(bridge.Display(), ":"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(time.Second))
	setup := make([]byte, 12)
	setup[0] = 'l'
	binary.LittleEndian.PutUint16(setup[2:], 11)
	conn.Write(setup)
	var header [8]byte
	_, err = io.ReadFull(conn, header[:])
	conn.Close()
	if err != nil || header[0] != 0 {
		t.Fatalf("unauthenticated X11 setup status=%d error=%v", header[0], err)
	}
	cmd := exec.Command(path, "-name", "worldr-x11-test", "-title", "Isolated X11 acceptance", "-geometry", "320x140", "private Xwayland")
	cmd.Env = bridge.Environment(os.Environ())
	var log boundedLog
	cmd.Stdout, cmd.Stderr = &log, &log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}()
	var first apps.Surface
	poll := func() []apps.Surface {
		t.Helper()
		if err := bridge.Poll(); err != nil {
			t.Fatal(err)
		}
		surfaces, err := server.Poll()
		if err != nil {
			t.Fatal(err)
		}
		return surfaces
	}
	until := func(check func([]apps.Surface) bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if check(poll()) {
				return
			}
			time.Sleep(3 * time.Millisecond)
		}
		t.Fatalf("X11 acceptance timed out; xmessage=%q Xwayland=%q windows=%+v", log.String(), bridge.log.String(), bridge.windows)
	}
	until(func(s []apps.Surface) bool {
		if len(s) > 0 {
			first = s[0]
			return true
		}
		return false
	})
	w, ok := bridge.Window(first.ID)
	if !ok || w.ID == 0 || w.ObjectID == 0 || w.Title != "Isolated X11 acceptance" || w.AppID == "" || first.PID != uint32(bridge.cmd.Process.Pid) {
		t.Fatalf("association %+v, surface %+v", w, first)
	}
	if first.Width != 320 || first.Height != 140 || len(first.Pixels) != 320*140*4 {
		t.Fatalf("X11 pixels %dx%d bytes=%d", first.Width, first.Height, len(first.Pixels))
	}
	var changed bool
	for i := 4; i < len(first.Pixels); i += 4 {
		if !bytes.Equal(first.Pixels[:4], first.Pixels[i:i+4]) {
			changed = true
			break
		}
	}
	if !changed {
		t.Fatal("X11 retained image is blank")
	}
	if err := bridge.Focus(first.ID); err != nil {
		t.Fatal(err)
	}
	if err := server.Focus(first.ID); err != nil {
		t.Fatal(err)
	}
	wire := openWireClient(t, bridge)
	until(func([]apps.Surface) bool { return wire.focus() == w.ID })
	// Identically sized windows must retain their own title and exact surface
	// association. Mapping and _NET_ACTIVE_WINDOW requests cannot steal focus.
	second := exec.Command(path, "-name", "worldr-x11-second", "-title", "Second X11 window", "-geometry", "320x140", "-default", "okay", "same size, distinct association")
	second.Env = bridge.Environment(os.Environ())
	second.Stdout, second.Stderr = &log, &log
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	secondDone := make(chan error, 1)
	go func() { secondDone <- second.Wait() }()
	defer func() {
		second.Process.Kill()
		select {
		case <-secondDone:
		case <-time.After(time.Second):
		}
	}()
	var other Window
	until(func(s []apps.Surface) bool {
		for _, surface := range s {
			if surface.Title == "Second X11 window" {
				other, _ = bridge.Window(surface.ID)
			}
		}
		return len(s) == 2 && other.ID != 0
	})
	if other.ID == w.ID || other.ObjectID == w.ObjectID || other.SurfaceID == first.ID {
		t.Fatal("two X11 windows shared an association")
	}
	wire.activate(other.ID)
	for deadline := time.Now().Add(40 * time.Millisecond); time.Now().Before(deadline); {
		poll()
		time.Sleep(3 * time.Millisecond)
	}
	if got := wire.focus(); got != w.ID {
		t.Fatalf("mapping/client request stole focus: %x, want %x", got, w.ID)
	}
	wire.title(w.ID, "Renamed α window")
	until(func(s []apps.Surface) bool {
		for _, surface := range s {
			if surface.ID == first.ID && surface.Title == "Renamed α window" {
				return true
			}
		}
		return false
	})
	wire.mapped(other.ID, false)
	until(func(s []apps.Surface) bool { return len(s) == 1 })
	if _, ok := bridge.Window(other.SurfaceID); ok {
		t.Fatal("withdrawn X11 window retained its route")
	}
	wire.mapped(other.ID, true)
	until(func(s []apps.Surface) bool {
		for _, surface := range s {
			if surface.Title == "Second X11 window" {
				other, _ = bridge.Window(surface.ID)
			}
		}
		return len(s) == 2
	})
	if err := bridge.Focus(other.SurfaceID); err != nil {
		t.Fatal(err)
	}
	if err := server.Focus(other.SurfaceID); err != nil {
		t.Fatal(err)
	}
	until(func([]apps.Surface) bool { return wire.focus() == other.ID })
	if err := server.Key(28, true, 10, 0, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := server.Key(28, false, 11, 0, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	until(func(s []apps.Surface) bool { return len(s) == 1 })
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("keyboard-activated default button: %v %s", err, log.String())
		}
		secondDone <- nil
	case <-time.After(time.Second):
		t.Fatal("X11 keyboard input did not activate default button")
	}
	if err := bridge.Resize(first.ID, 420, 180); err != nil {
		t.Fatal(err)
	}
	until(func(s []apps.Surface) bool { return len(s) == 1 && s[0].Width == 420 && s[0].Height == 180 })
	if err := bridge.CloseSurface(first.ID); err != nil {
		t.Fatal(err)
	}
	until(func(s []apps.Surface) bool { return len(s) == 0 })
	select {
	case err := <-done:
		// xmessage documents exit status 1 when its WM close action is used.
		var exit *exec.ExitError
		if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
			t.Fatalf("WM_DELETE_WINDOW client exit: %v %s", err, log.String())
		}
		done <- nil
	case <-time.After(time.Second):
		t.Fatal("X11 client did not exit on WM_DELETE_WINDOW")
	}
	dir := bridge.dir
	if err := bridge.Close(); err != nil {
		t.Fatal(err)
	}
	if err := bridge.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("private authority directory survived close")
	}
	if bridge.Display() != "" || bridge.Poll() != ErrClosed {
		t.Fatal("closed bridge still live")
	}
}

func TestStartupFailureCleansPrivateFiles(t *testing.T) {
	server, err := apps.Open(300, 200)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "Xwayland"), []byte("#!/bin/sh\nexit 37\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	for attempt := 0; attempt < 3; attempt++ {
		start := time.Now()
		if b, err := Open(server, io.Discard); err == nil {
			b.Close()
			t.Fatal("failed Xwayland was accepted")
		}
		if time.Since(start) > 3*time.Second {
			t.Fatal("exited Xwayland was not noticed promptly")
		}
		files, err := os.ReadDir(temp)
		if err != nil || len(files) != 0 {
			t.Fatalf("startup left private files: %v %v", files, err)
		}
	}
	server.Close()
	if _, err := Open(server, io.Discard); !errors.Is(err, apps.ErrClosed) {
		t.Fatalf("closed server startup: %v", err)
	}
}
