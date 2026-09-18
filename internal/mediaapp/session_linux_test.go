//go:build linux && cgo

package mediaapp

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/media"
)

func TestMediaSessionRestoresRealPausedAndPlayingVideo(t *testing.T) {
	if os.Getenv("WORLDR_TEST_MEDIA") != "1" {
		t.Skip("set WORLDR_TEST_MEDIA=1 for real silent libmpv session restore")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "resume fixture.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=96x64:rate=12:duration=4", "-f", "lavfi", "-i", "sine=frequency=330:sample_rate=24000:duration=4", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-threads", "1", "-shortest", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate resume fixture: %v: %s", err, output)
	}
	for _, paused := range []bool{true, false} {
		t.Run(map[bool]string{false: "playing", true: "paused"}[paused], func(t *testing.T) {
			manager := NewManager(media.Options{AudioOutput: "null"})
			defer manager.Close()
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			saved := SessionState{Path: path, Position: 1.25, Paused: paused, Volume: 19, Muted: true}
			if paused {
				saved.Volume = 0
			} else {
				saved.Muted = false
			}
			if _, err := manager.RestoreFile(file, "resumed video.mp4", saved); err != nil {
				t.Fatal(err)
			}
			if initial, ok := manager.SessionState(); !ok || initial != saved {
				t.Fatalf("pending restore lost its desired state: %+v %v", initial, ok)
			}
			poll := func() {
				t.Helper()
				if err := manager.Poll(); err != nil {
					t.Fatal(err)
				}
				if manager.message != "" || manager.state.Error != "" {
					t.Fatalf("restore error: %s / %s", manager.message, manager.state.Error)
				}
				if manager.state.Muted != saved.Muted || math.Abs(manager.state.Volume-saved.Volume) > .01 {
					t.Fatalf("restore exposed default audio controls: %+v", manager.state)
				}
				if manager.resume != nil && !manager.state.Paused {
					t.Fatal("restored video played before its seek completed")
				}
				if manager.focused {
					t.Fatal("asynchronous restore took application focus")
				}
			}
			wait := func(ready func() bool) {
				t.Helper()
				deadline := time.Now().Add(6 * time.Second)
				for time.Now().Before(deadline) {
					poll()
					if ready() {
						return
					}
					time.Sleep(5 * time.Millisecond)
				}
				t.Fatalf("resume did not complete: state=%+v pending=%+v", manager.state, manager.resume)
			}
			wait(func() bool {
				return manager.resume == nil && manager.state.HasVideo && manager.state.Paused == paused && math.Abs(manager.state.Position-saved.Position) < .16
			})
			colored := false
			for i := 0; i < len(manager.video.Pix); i += 4 {
				colored = colored || manager.video.Pix[i] > 100 && manager.video.Pix[i+3] == 255
			}
			if !colored || manager.state.SeekRevision == 0 {
				t.Fatal("restored seek did not decode and render a real frame")
			}
			if paused {
				position := manager.state.Position
				deadline := time.Now().Add(180 * time.Millisecond)
				for time.Now().Before(deadline) {
					poll()
					time.Sleep(5 * time.Millisecond)
				}
				if math.Abs(manager.state.Position-position) > .02 {
					t.Fatal("paused restore advanced playback")
				}
			} else {
				wait(func() bool { return manager.state.Position > saved.Position+.15 })
			}
			captured, ok := manager.SessionState()
			if !ok || captured.Path != path || captured.Paused != paused || captured.Volume != saved.Volume || captured.Muted != saved.Muted || captured.Position < saved.Position-.16 {
				t.Fatalf("real playback did not round-trip session state: %+v %v", captured, ok)
			}
		})
	}
}
