//go:build linux && cgo

package apps

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCloseFootSurfacePreservesSibling(t *testing.T) {
	if os.Getenv("WORLDR_TEST_APPS") != "1" {
		t.Skip("set WORLDR_TEST_APPS=1 for real foot clients")
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
	type client struct {
		cmd    *exec.Cmd
		done   chan error
		exited bool
		answer string
	}
	var clients []*client
	for i := 0; i < 2; i++ {
		dir := t.TempDir()
		log, err := os.Create(filepath.Join(dir, "foot.log"))
		if err != nil {
			t.Fatal(err)
		}
		answer := filepath.Join(dir, "answer")
		cmd := exec.Command(foot, "--config=/dev/null", fmt.Sprintf("--title=close-test-%d", i), "sh", "-c", `printf 'ready\n'; read -r reply; printf '%s' "$reply" > "$WORLDR_ANSWER"; read -r rest`)
		cmd.Env = append(os.Environ(), "WAYLAND_DISPLAY="+s.Socket(), "WORLDR_ANSWER="+answer)
		cmd.Stdout, cmd.Stderr = log, log
		if err := cmd.Start(); err != nil {
			log.Close()
			t.Fatal(err)
		}
		c := &client{cmd: cmd, done: make(chan error, 1), answer: answer}
		clients = append(clients, c)
		go func() { c.done <- cmd.Wait() }()
		t.Cleanup(func() {
			if !c.exited {
				cmd.Process.Kill()
				<-c.done
			}
			log.Close()
			if t.Failed() {
				b, _ := os.ReadFile(log.Name())
				t.Log(string(b))
			}
		})
	}
	pollUntil := func(why string, predicate func([]Surface) bool) []Surface {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			v, err := s.Poll()
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range clients {
				select {
				case <-c.done:
					c.exited = true
				default:
				}
			}
			if predicate(v) {
				return v
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("timed out: %s", why)
		return nil
	}
	initial := pollUntil("both foot windows map", func(v []Surface) bool { return len(v) == 2 })
	var closing, sibling Surface
	for _, v := range initial {
		if v.PID == uint32(clients[0].cmd.Process.Pid) {
			closing = v
		} else if v.PID == uint32(clients[1].cmd.Process.Pid) {
			sibling = v
		}
	}
	if closing.ID == 0 || sibling.ID == 0 {
		t.Fatal("client identities did not match their surfaces")
	}
	if err := s.Focus(closing.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Pointer(closing.ID, 20, 20); err != nil {
		t.Fatal(err)
	}
	if err := s.Button(272, true, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseSurface(closing.ID); err != nil {
		t.Fatal(err)
	}
	pollUntil("selected window exits and sibling stays mapped", func(v []Surface) bool {
		return len(v) == 1 && v[0].ID == sibling.ID && clients[0].exited
	})
	if clients[1].exited {
		t.Fatal("closing one window exited its sibling")
	}
	if err := s.CloseSurface(closing.ID); err != nil {
		t.Fatalf("repeated close of removed window: %v", err)
	}
	if err := s.Focus(sibling.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Pointer(sibling.ID, 20, 20); err != nil {
		t.Fatal(err)
	}
	if err := s.Button(272, false, 2); err != nil {
		t.Fatal(err)
	}
	for _, key := range []uint32{24, 37, 28} { // ok + Enter
		if err := s.Key(key, true, 3, 0, 0, 0, 0); err != nil {
			t.Fatal(err)
		}
		if err := s.Key(key, false, 4, 0, 0, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	pollUntil("sibling shell still receives real keyboard input", func(v []Surface) bool {
		b, err := os.ReadFile(clients[1].answer)
		return len(v) == 1 && v[0].ID == sibling.ID && err == nil && string(b) == "ok"
	})
	if err := s.CloseSurface(sibling.ID); err != nil {
		t.Fatal(err)
	}
	pollUntil("remaining window closes normally", func(v []Surface) bool { return len(v) == 0 && clients[1].exited })
}
