//go:build linux && cgo

package media

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func mediaIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("WORLDR_TEST_MEDIA") != "1" {
		t.Skip("set WORLDR_TEST_MEDIA=1 for real libmpv decoding and silent audio playback")
	}
}

func mediaFixture(t *testing.T) *os.File {
	t.Helper()
	return mediaCodecFixture(t, "mkv", "ffv1", "pcm_s16le")
}

func mediaCodecFixture(t *testing.T, extension, videoCodec, audioCodec string) *os.File {
	t.Helper()
	mediaIntegration(t)
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal("media integration requires ffmpeg:", err)
	}
	path := filepath.Join(t.TempDir(), "colors."+extension)
	command := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=96x64:rate=12:duration=3", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=3", "-c:v", videoCodec, "-c:a", audioCodec, "-threads", "1", "-shortest", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate video: %v: %s", err, output)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return file
}

func TestPlayerCommonVideoContainers(t *testing.T) {
	for _, fixture := range []struct{ extension, video, audio string }{
		{"mp4", "libx264", "aac"}, {"webm", "libvpx-vp9", "libopus"},
	} {
		t.Run(fixture.extension, func(t *testing.T) {
			file := mediaCodecFixture(t, fixture.extension, fixture.video, fixture.audio)
			player := testPlayer(t)
			if err := player.Load(file); err != nil {
				t.Fatal(err)
			}
			pixels := make([]byte, 96*64*4)
			waitMedia(t, player, pixels, func(state State) bool {
				return state.Loaded && state.HasVideo && state.Duration > 2.5 && state.Position > .1
			})
			before := bytes.Clone(pixels)
			if err := player.Seek(1.5); err != nil {
				t.Fatal(err)
			}
			waitMedia(t, player, pixels, func(state State) bool {
				return state.Position >= 1.4 && !bytes.Equal(before, pixels)
			})
		})
	}
}

func testPlayer(t *testing.T) *Player {
	t.Helper()
	mediaIntegration(t)
	player, err := New(Options{AudioOutput: "null"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { player.Close() })
	return player
}

func waitMedia(t *testing.T, player *Player, pixels []byte, predicate func(State) bool) State {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	var state State
	for time.Now().Before(deadline) {
		var err error
		state, err = player.Poll()
		if err != nil {
			t.Fatal(err)
		}
		if state.Error != "" {
			t.Fatal("decoder:", state.Error)
		}
		if _, err := player.Render(pixels, 96, 64, 96*4); err != nil {
			t.Fatal(err)
		}
		if predicate(state) {
			return state
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("media condition timed out: %+v", state)
	return state
}

func TestPlayerVideoAudioControlsAndFrameLifetime(t *testing.T) {
	file := mediaFixture(t)
	player := testPlayer(t)
	if err := player.Load(file); err != nil {
		t.Fatal(err)
	}
	if err := player.Load(file); err == nil {
		t.Fatal("accepted a second asynchronous file load")
	}
	// The descriptor is the source. Renaming its original pathname must not
	// turn the queued load into a race or accidentally open a replacement file.
	if err := os.Rename(file.Name(), file.Name()+".renamed"); err != nil {
		t.Fatal(err)
	}
	pixels := make([]byte, 96*64*4)
	state := waitMedia(t, player, pixels, func(state State) bool {
		return state.Loaded && state.HasVideo && state.Position > .15 && state.Duration > 2.5
	})
	if state.Width != 96 || state.Height != 64 {
		t.Fatalf("unexpected source geometry: %+v", state)
	}
	colored := false
	for i := 0; i < len(pixels); i += 4 {
		if pixels[i+3] != 255 {
			t.Fatalf("video pixel %d has nonopaque alpha %d", i/4, pixels[i+3])
		}
		colored = colored || pixels[i] > 50 && pixels[i] > pixels[i+1]+20
	}
	if !colored {
		t.Fatal("software renderer produced no decoded color bars")
	}
	if err := player.Pause(true); err != nil {
		t.Fatal(err)
	}
	if err := player.SetVolume(23); err != nil {
		t.Fatal(err)
	}
	if err := player.SetMute(true); err != nil {
		t.Fatal(err)
	}
	state = waitMedia(t, player, pixels, func(state State) bool { return state.Paused && state.Muted && math.Abs(state.Volume-23) < .01 })
	start := state.Position
	before := bytes.Clone(pixels)
	until := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(until) {
		state, _ = player.Poll()
		if _, err := player.Render(pixels, 96, 64, 96*4); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if math.Abs(state.Position-start) > .04 {
		t.Fatalf("paused time advanced from %.3f to %.3f", start, state.Position)
	}
	if err := player.Seek(1.5); err != nil {
		t.Fatal(err)
	}
	waitMedia(t, player, pixels, func(state State) bool {
		return state.Paused && math.Abs(state.Position-1.5) < .15 && !bytes.Equal(before, pixels)
	})
	// A paused frame must repaint into a differently sized/strided surface,
	// retaining black letterboxing and making every pixel opaque.
	wide := make([]byte, 200*4*64)
	changed, err := player.Render(wide, 192, 64, 200*4)
	if err != nil || !changed {
		t.Fatalf("paused resize did not redraw: %v, %v", changed, err)
	}
	if wide[0] != 0 || wide[1] != 0 || wide[2] != 0 || wide[3] != 255 {
		t.Fatalf("letterbox pixel is %v", wide[:4])
	}
	if err := player.Seek(2.8); err != nil {
		t.Fatal(err)
	}
	if err := player.SetMute(false); err != nil {
		t.Fatal(err)
	}
	if err := player.Pause(false); err != nil {
		t.Fatal(err)
	}
	state = waitMedia(t, player, pixels, func(state State) bool { return state.Ended && !state.Muted })
	if !state.Loaded || !state.HasVideo {
		t.Fatalf("last frame discarded at EOF: %+v", state)
	}
	if err := player.Seek(0); err != nil {
		t.Fatal(err)
	}
	if err := player.Pause(false); err != nil {
		t.Fatal(err)
	}
	waitMedia(t, player, pixels, func(state State) bool { return !state.Ended && !state.Paused && state.Position < .5 })
	if err := player.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Stat(); err != nil {
		t.Fatalf("Close took ownership of caller's file: %v", err)
	}
	if err := player.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := player.Poll(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Poll after Close: %v", err)
	}
	if err := player.Pause(false); !errors.Is(err, ErrClosed) {
		t.Fatalf("Pause after Close: %v", err)
	}
}

func TestPlayerRejectsBadInputsAndPlaylistReferences(t *testing.T) {
	player := testPlayer(t)
	if err := player.Load(nil); err == nil {
		t.Fatal("accepted nil file")
	}
	directory, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := player.Load(directory); err == nil {
		t.Fatal("accepted directory")
	}
	for _, volume := range []float64{-1, 101, math.NaN(), math.Inf(1)} {
		if err := player.SetVolume(volume); err == nil {
			t.Fatalf("accepted volume %v", volume)
		}
	}
	for _, position := range []float64{-1, math.NaN(), math.Inf(1)} {
		if err := player.Seek(position); err == nil {
			t.Fatalf("accepted seek %v", position)
		}
	}
	unaligned := make([]byte, 8)
	for uintptr(unsafe.Pointer(&unaligned[0]))%4 == 0 {
		unaligned = unaligned[1:]
	}
	for _, buffer := range []struct {
		pixels                []byte
		width, height, stride int
	}{
		{nil, 1, 1, 4}, {make([]byte, 4), 1, 1, 5},
		{make([]byte, 4), 5000, 1, 20000}, {unaligned, 1, 1, 4},
	} {
		if _, err := player.Render(buffer.pixels, buffer.width, buffer.height, buffer.stride); err == nil {
			t.Fatal("accepted invalid render surface")
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	path := filepath.Join(t.TempDir(), "pretend-video.mkv")
	playlist := fmt.Sprintf("#EXTM3U\n#EXTINF:1,Unexpected\nhttp://%s/unexpected.mp4\n", listener.Addr())
	if err := os.WriteFile(path, []byte(playlist), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := player.Load(file); err != nil {
		t.Fatal(err)
	}
	pixels := make([]byte, 96*64*4)
	deadline := time.Now().Add(4 * time.Second)
	var state State
	for time.Now().Before(deadline) {
		state, err = player.Poll()
		if err != nil {
			t.Fatal(err)
		}
		if state.Error != "" {
			break
		}
		if _, err := player.Render(pixels, 96, 64, 96*4); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if state.Error == "" || state.Loaded {
		t.Fatalf("playlist did not fail visibly: %+v", state)
	}
	listener.(*net.TCPListener).SetDeadline(time.Now().Add(20 * time.Millisecond))
	connection, err := listener.Accept()
	if err == nil {
		connection.Close()
		t.Fatal("playlist followed a network reference")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("network check: %v", err)
	}
}

func TestPlayerRejectsInvalidInitialAudioState(t *testing.T) {
	for _, volume := range []float64{-1, 101, math.NaN(), math.Inf(1)} {
		player, err := New(Options{AudioOutput: "null", InitialState: &InitialState{Paused: true, Volume: volume}})
		if player != nil {
			_ = player.Close()
		}
		if err == nil {
			t.Fatalf("accepted invalid initial audio volume %v", volume)
		}
	}
}
