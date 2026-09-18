//go:build linux && cgo

package apps

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrivateSocketLifecycle(t *testing.T) {
	s, err := Open(640, 400)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	info, err := os.Stat(filepath.Dir(s.Socket()))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("socket directory permissions: %v", info.Mode())
	}
	if !filepath.IsAbs(s.Socket()) {
		t.Fatal("socket path must be absolute")
	}
	if _, err := s.Poll(); err != nil {
		t.Fatal(err)
	}
	if err := s.Focus(17); err == nil {
		t.Fatal("unknown focus accepted")
	}
	if err := s.Focus(0); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseSurface(17); err != nil {
		t.Fatalf("closing an absent surface: %v", err)
	}
	if err := s.SetRepeat(0, 500); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRepeat(-1, 500); err == nil {
		t.Fatal("negative repeat accepted")
	}
	if err := s.SetKeymap("invalid"); err == nil {
		t.Fatal("invalid keymap accepted")
	}
	path := s.Socket()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("private socket remains: %v", err)
	}
	if _, err := s.Poll(); err != ErrClosed {
		t.Fatalf("poll after close: %v", err)
	}
	if err := s.CloseSurface(17); err != ErrClosed {
		t.Fatalf("close surface after shutdown: %v", err)
	}
}

func TestFootPixelsKeyboardResizeAndDisconnect(t *testing.T) {
	if os.Getenv("WORLDR_TEST_APPS") != "1" {
		t.Skip("set WORLDR_TEST_APPS=1 for a real foot client")
	}
	foot, err := exec.LookPath("foot")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(640, 400)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	dir := t.TempDir()
	answer := filepath.Join(dir, "answer")
	size := filepath.Join(dir, "size")
	paste := filepath.Join(dir, "paste")
	bridge := filepath.Join(dir, "bridge")
	cmd := exec.Command(foot, "--config=/dev/null", "--app-id=worldr.adapter.test", "--title=worldr adapter test", "sh", "-c", "printf 'WORLD RENDERER TEST\\n'; read -r answer; printf '%s' \"$answer\" > \"$WORLDR_ANSWER\"; stty size > \"$WORLDR_SIZE\"; printf '\\033]52;c;c3BhdGlhbA==\\007'; read -r paste; printf '%s' \"$paste\" > \"$WORLDR_PASTE\"; read -r bridge; printf '%s' \"$bridge\" > \"$WORLDR_BRIDGE\"; sleep 15")
	cmd.Env = append(os.Environ(), "WAYLAND_DISPLAY="+s.Socket(), "WORLDR_ANSWER="+answer, "WORLDR_SIZE="+size, "WORLDR_PASTE="+paste, "WORLDR_BRIDGE="+bridge)
	var log bytes.Buffer
	cmd.Stdout = &log
	cmd.Stderr = &log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	finished := false
	defer func() {
		if !finished {
			cmd.Process.Kill()
			<-wait
		}
		if t.Failed() {
			t.Log(log.String())
		}
	}()
	pollUntil := func(predicate func([]Surface) bool) []Surface {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			surfaces, err := s.Poll()
			if err != nil {
				t.Fatal(err)
			}
			if predicate(surfaces) {
				return surfaces
			}
			select {
			case err := <-wait:
				finished = true
				t.Fatalf("foot exited early: %v\n%s", err, log.String())
			default:
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for foot\n%s", log.String())
		return nil
	}
	first := pollUntil(func(v []Surface) bool {
		if len(v) != 1 {
			return false
		}
		colors := map[[3]byte]bool{}
		for i := 0; i < len(v[0].Pixels); i += 4 {
			p := v[0].Pixels
			colors[[3]byte{p[i], p[i+1], p[i+2]}] = true
			if len(colors) >= 3 {
				return true
			}
		}
		return false
	})[0]
	if first.Title != "worldr adapter test" {
		t.Fatalf("title: %q", first.Title)
	}
	if first.AppID != "worldr.adapter.test" || first.PID != uint32(cmd.Process.Pid) {
		t.Fatalf("application identity: app_id=%q pid=%d, want pid=%d", first.AppID, first.PID, cmd.Process.Pid)
	}
	// Foot rounds the requested extent down to its character cell grid.
	if first.Width < 600 || first.Width > 640 || first.Height < 360 || first.Height > 400 {
		t.Fatalf("initial extent: %dx%d", first.Width, first.Height)
	}
	initial := append([]byte(nil), first.Pixels...)
	colors := map[[3]byte]bool{}
	for i := 0; i < len(initial); i += 4 {
		colors[[3]byte{initial[i], initial[i+1], initial[i+2]}] = true
		if initial[i+3] != 255 {
			t.Fatal("non-opaque pixel")
		}
	}
	if len(colors) < 3 {
		t.Fatalf("expected terminal glyphs, got %d colors", len(colors))
	}
	if err := s.Focus(first.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Pointer(first.ID, 20, 20); err != nil {
		t.Fatal(err)
	}
	if err := s.Resize(first.ID, 800, 500); err != nil {
		t.Fatal(err)
	}
	resized := pollUntil(func(v []Surface) bool {
		return len(v) == 1 && v[0].Width >= 760 && v[0].Width <= 800 && v[0].Height >= 460 && v[0].Height <= 500
	})[0]
	if resized.Revision <= first.Revision {
		t.Fatal("resize did not advance revision")
	}
	if !bytes.Equal(first.Pixels, initial) {
		t.Fatal("an earlier snapshot was mutated")
	}
	// Linux evdev codes for worldr + Enter. The application receives real keys,
	// and its shell writes a canary, proving that rendering alone is insufficient.
	for _, key := range []uint32{17, 24, 19, 38, 32, 19, 28} {
		if err := s.Key(key, true, 1, 0, 0, 0, 0); err != nil {
			t.Fatal(err)
		}
		if err := s.Key(key, false, 2, 0, 0, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	pollUntil(func(v []Surface) bool { b, err := os.ReadFile(answer); return err == nil && string(b) == "worldr" })
	var rows, columns int
	b, err := os.ReadFile(size)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d %d", &rows, &columns); err != nil || rows < 10 || columns < 30 {
		t.Fatalf("terminal PTY size %q: %v", b, err)
	}
	// OSC 52 sets a real selection in foot. Ctrl+Shift+V requests its data via
	// the Wayland offer's pipe, exercising both sides of clipboard forwarding.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := s.Poll(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, e := range []struct {
		code uint32
		down bool
		mods uint32
	}{{29, true, 4}, {42, true, 5}, {47, true, 5}, {47, false, 5}, {42, false, 4}, {29, false, 0}} {
		if err := s.Key(e.code, e.down, 3, e.mods, 0, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	deadline = time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := s.Poll(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := s.Key(28, true, 4, 0, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Key(28, false, 5, 0, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	pollUntil(func(v []Surface) bool { b, err := os.ReadFile(paste); return err == nil && string(b) == "spatial" })
	// Export foot's selection through a borrowed descriptor without having the
	// adapter read or retain any clipboard bytes.
	offer := s.ClipboardOffer()
	if offer.ID == 0 || offer.ExternalID != 0 || len(offer.MIMEs) == 0 {
		t.Fatalf("native selection metadata: %+v", offer)
	}
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Close()
	if err := rd.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.ReceiveClipboard(offer.ID, offer.MIMEs[0], int(wr.Fd())); err != nil {
		t.Fatal(err)
	}
	wr.Close()
	type readResult struct {
		b   []byte
		err error
	}
	readDone := make(chan readResult, 1)
	go func() { b, err := io.ReadAll(rd); readDone <- readResult{b, err} }()
	pollUntil(func([]Surface) bool {
		select {
		case r := <-readDone:
			if r.err != nil || string(r.b) != "spatial" {
				t.Fatalf("exported clipboard %q: %v", r.b, r.err)
			}
			return true
		default:
			return false
		}
	})
	// Import an external source. Foot asks for it via Ctrl+Shift+V; only then
	// does the adapter transfer the requested FD back to the bridge owner.
	if err := s.OfferClipboard(77, []string{"text/plain;charset=utf-8"}); err != nil {
		t.Fatal(err)
	}
	external := s.ClipboardOffer()
	if external.ID == 0 || external.ExternalID != 77 || external.Revision <= offer.Revision {
		t.Fatalf("external selection metadata: %+v", external)
	}
	if err := s.ReceiveClipboard(offer.ID, offer.MIMEs[0], int(rd.Fd())); err == nil {
		t.Fatal("stale clipboard offer accepted")
	}
	deadline = time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := s.Poll(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, e := range []struct {
		code uint32
		down bool
		mods uint32
	}{{29, true, 4}, {42, true, 5}, {47, true, 5}, {47, false, 5}, {42, false, 4}, {29, false, 0}} {
		if err := s.Key(e.code, e.down, 6, e.mods, 0, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	pollUntil(func([]Surface) bool {
		requests := s.PollClipboardRequests()
		for _, r := range requests {
			f := os.NewFile(uintptr(r.FD), "clipboard relay")
			if r.ExternalID != 77 || r.MIME != "text/plain;charset=utf-8" {
				f.Close()
				t.Fatalf("unexpected external request %+v", r)
			}
			_, err := f.WriteString("bridged")
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		return len(requests) > 0
	})
	deadline = time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := s.Poll(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := s.Key(28, true, 7, 0, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Key(28, false, 8, 0, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	pollUntil(func([]Surface) bool { b, err := os.ReadFile(bridge); return err == nil && string(b) == "bridged" })
	if err := s.Focus(0); err != nil {
		t.Fatal(err)
	}
	if err := s.Pointer(0, 0, 0); err != nil {
		t.Fatal(err)
	}
	cmd.Process.Kill()
	<-wait
	finished = true
	pollUntil(func(v []Surface) bool { return len(v) == 0 })
}
