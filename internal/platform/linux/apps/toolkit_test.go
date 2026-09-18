//go:build linux && cgo

package apps

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// This diagnostic is opt-in: a mapped first frame alone does not establish
// general toolkit compatibility, popup support, or normal browser operation.
func TestToolkitProbe(t *testing.T) {
	if os.Getenv("WORLDR_TEST_TOOLKITS") != "1" {
		t.Skip("set WORLDR_TEST_TOOLKITS=1 for isolated toolkit probes")
	}
	for _, name := range []string{"chromium", "konsole"} {
		t.Run(name, func(t *testing.T) {
			path, err := exec.LookPath(name)
			if err != nil {
				t.Skip(err)
			}
			s, err := Open(900, 560)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			dir := t.TempDir()
			page := filepath.Join(dir, "probe.html")
			if err := os.WriteFile(page, []byte(`<title>WORLDR COMPAT PROBE</title><body style="background:#15753a;color:white;font:32px sans-serif"><h1>WORLDR COMPAT PROBE</h1><input autofocus oninput="document.title='typed:'+this.value"><p>Private local page</p></body>`), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"--ozone-platform=wayland", "--disable-gpu", "--user-data-dir=" + filepath.Join(dir, "chromium"), "--no-first-run", "--no-default-browser-check", "--disable-background-networking", "--disable-component-update", "--disable-sync", "--disable-default-apps", "--disable-features=MediaRouter", "--app=file://" + page}
			if name == "konsole" {
				args = []string{"--separate", "--nofork", "--workdir", dir, "-e", "sh", "-c", "printf 'WORLDR COMPAT PROBE\\n'; read -r answer; printf '%s' \"$answer\" > answer; sleep 10"}
			}
			cmd := exec.Command(path, args...)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Env = append(os.Environ(), "WAYLAND_DISPLAY="+s.Socket(), "QT_QPA_PLATFORM=wayland", "HOME="+dir, "XDG_CONFIG_HOME="+filepath.Join(dir, "config"), "XDG_CACHE_HOME="+filepath.Join(dir, "cache"), "XDG_DATA_HOME="+filepath.Join(dir, "data"), "DBUS_SESSION_BUS_ADDRESS=unix:path="+filepath.Join(dir, "no-session-bus"))
			var log bytes.Buffer
			cmd.Stdout = &log
			cmd.Stderr = &log
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			ended := false
			defer func() {
				syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				if !ended {
					<-done
				}
				if t.Failed() {
					t.Log(log.String())
				}
			}()
			deadline := time.Now().Add(10 * time.Second)
			var mapped Surface
			var mappedAt time.Time
			typed := false
			for time.Now().Before(deadline) {
				surfaces, err := s.Poll()
				if err != nil {
					t.Fatal(err)
				}
				if len(surfaces) > 0 && mapped.ID == 0 {
					v := surfaces[0]
					t.Logf("mapped id=%d app_id=%q pid=%d title=%q extent=%dx%d revision=%d", v.ID, v.AppID, v.PID, v.Title, v.Width, v.Height, v.Revision)
					mapped = v
					mappedAt = time.Now()
					if err := s.Focus(v.ID); err != nil {
						t.Fatal(err)
					}
				}
				if mapped.ID != 0 && !typed && time.Since(mappedAt) > 300*time.Millisecond {
					for _, code := range []uint32{17, 24, 19, 38, 32, 19} {
						if err := s.Key(code, true, 1, 0, 0, 0, 0); err != nil {
							t.Fatal(err)
						}
						if err := s.Key(code, false, 2, 0, 0, 0, 0); err != nil {
							t.Fatal(err)
						}
					}
					if name == "konsole" {
						s.Key(28, true, 3, 0, 0, 0, 0)
						s.Key(28, false, 4, 0, 0, 0, 0)
					}
					typed = true
				}
				if typed {
					if name == "chromium" && len(surfaces) > 0 && surfaces[0].Title == "typed:worldr" {
						t.Log("local page received actual keyboard input")
						return
					}
					if name == "konsole" {
						if b, err := os.ReadFile(filepath.Join(dir, "answer")); err == nil && string(b) == "worldr" {
							t.Log("Konsole shell received actual keyboard input")
							return
						}
					}
				}
				select {
				case err := <-done:
					ended = true
					t.Fatalf("client exited before mapping: %v", err)
				default:
				}
				time.Sleep(5 * time.Millisecond)
			}
			t.Fatal("client did not map and receive keyboard input within ten seconds")
		})
	}
}
