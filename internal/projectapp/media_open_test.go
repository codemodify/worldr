package projectapp

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

func mediaFile(t *testing.T) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "video-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func videoProvider(t *testing.T) (*Provider, *controlledReader) {
	t.Helper()
	p, reader := controlledProvider(t)
	listing := nextCall(t, reader)
	listing.answer <- result{entries: []entry{{name: "clip.MP4", kind: fileEntry}, {name: "folder", kind: directoryEntry}}}
	pollUntil(t, p, func() bool { return p.selected == 0 })
	return p, reader
}

func assertClosedFile(t *testing.T, file *os.File) {
	t.Helper()
	if _, err := file.Stat(); err == nil {
		t.Fatalf("unclaimed media descriptor remained open: %v", err)
	}
}

func TestVideoExtensionsAreCaseInsensitiveAndBoundedToSupportedNames(t *testing.T) {
	for _, extension := range []string{"mp4", "mkv", "webm", "mov", "m4v", "avi", "ogv", "ogg", "mpeg", "mpg", "ts", "m2ts"} {
		if !videoPath("dir/movie."+extension) || !videoPath("dir/movie."+strings.ToUpper(extension)) {
			t.Fatalf("video extension %q was not recognized", extension)
		}
	}
	for _, path := range []string{"README", "movie.mp4.txt", "video.png", "mp4", "movie.webm/notes"} {
		if videoPath(path) {
			t.Fatalf("non-video name %q was treated as a video", path)
		}
	}
}

func TestVideoLaunchNeedsExplicitOpenAndTransfersOwnershipOnlyFromPoll(t *testing.T) {
	for _, activation := range []string{"enter", "toolbar", "double-click"} {
		t.Run(activation, func(t *testing.T) {
			p, reader := videoProvider(t)
			var opened *os.File
			var name string
			p.SetOpenHandler(func(file *os.File, displayName string) error { opened, name = file, displayName; return nil })
			p.selectEntry(0)
			if p.loadingFile || len(reader.calls) != 0 || opened != nil || !strings.Contains(p.message, "Video file") {
				t.Fatal("selecting a video started a read/launch or omitted its hint")
			}
			p.Focus(1)
			switch activation {
			case "enter":
				p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: true})
			case "toolbar":
				p.Send(1, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 205, Y: 70})
			case "double-click":
				for _, stamp := range []uint32{100, 200} {
					p.Send(1, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 20, Y: contentTop + 3, Time: stamp})
				}
			}
			call := nextCall(t, reader)
			if !call.request.openMedia || call.request.directory || call.request.path != "clip.MP4" {
				t.Fatal("explicit video open queued a text preview or wrong path")
			}
			file := mediaFile(t)
			call.answer <- result{file: file}
			await(t, func() bool { return len(p.results) == 1 })
			if opened != nil {
				t.Fatal("worker invoked the media handler outside host Poll")
			}
			if err := p.Poll(); err != nil || opened != file || name != "clip.MP4" {
				t.Fatal("host Poll did not transfer the descriptor and basename", err)
			}
			generation := p.generation
			p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: true, Repeat: true})
			if p.generation != generation || p.loadingFile {
				t.Fatal("held Enter repeatedly launched the video")
			}
			p.Close()
			if _, err := opened.Stat(); err != nil {
				t.Fatal("closing browser revoked a successfully transferred descriptor", err)
			}
		})
	}
}

func TestVideoOpenFailureAndUnavailableHandlerCloseDescriptors(t *testing.T) {
	for _, mode := range []string{"unavailable", "handler-error", "reader-error"} {
		t.Run(mode, func(t *testing.T) {
			p, reader := videoProvider(t)
			if mode == "handler-error" {
				p.SetOpenHandler(func(*os.File, string) error { return errors.New("decoder unavailable") })
			}
			p.openSelected()
			call := nextCall(t, reader)
			file := mediaFile(t)
			answer := result{file: file}
			if mode == "reader-error" {
				answer.err = errors.New("permission denied")
			}
			call.answer <- answer
			pollUntil(t, p, func() bool { return !p.loadingFile })
			assertClosedFile(t, file)
			if p.err != nil || p.message == "" || strings.Contains(p.message, "Opened video") {
				t.Fatal("video failure crashed the host or lacked a visible error")
			}
		})
	}
}

func TestVideoStaleAndBufferedCloseResultsReleaseDescriptors(t *testing.T) {
	for _, mode := range []string{"selection-changed", "closed"} {
		t.Run(mode, func(t *testing.T) {
			p, reader := videoProvider(t)
			called := false
			p.SetOpenHandler(func(*os.File, string) error { called = true; return nil })
			p.openSelected()
			call := nextCall(t, reader)
			file := mediaFile(t)
			call.answer <- result{file: file}
			await(t, func() bool { return len(p.results) == 1 })
			if mode == "closed" {
				p.Close()
			} else {
				p.selectEntry(1)
			}
			if err := p.Poll(); err != nil || called {
				t.Fatal("canceled media result reached the handler", err)
			}
			assertClosedFile(t, file)
		})
	}
}

type lateMediaReader struct {
	started, release, closed chan struct{}
	file                     *os.File
}

func (r *lateMediaReader) read(req request) result {
	if req.directory {
		return result{request: req, entries: []entry{{name: "clip.webm", kind: fileEntry}, {name: "folder", kind: directoryEntry}}}
	}
	close(r.started)
	<-r.release // A filesystem call may finish after its context was canceled.
	return result{request: req, file: r.file}
}
func (r *lateMediaReader) close() error { close(r.closed); return nil }

func TestVideoWorkerClosesLateDescriptorAfterCancellation(t *testing.T) {
	for _, mode := range []string{"selection-changed", "closed"} {
		t.Run(mode, func(t *testing.T) {
			reader := &lateMediaReader{started: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{}), file: mediaFile(t)}
			p, err := newProvider(reader, "/project")
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			pollUntil(t, p, func() bool { return p.selected == 0 })
			p.openSelected()
			select {
			case <-reader.started:
			case <-time.After(time.Second):
				t.Fatal("worker did not start its media request")
			}
			if mode == "closed" {
				p.Close()
			} else {
				p.selectEntry(1)
			}
			close(reader.release)
			await(t, func() bool { _, err := reader.file.Stat(); return err != nil })
			if len(p.results) != 0 {
				t.Fatal("worker published a canceled media result")
			}
			p.Close()
			select {
			case <-p.done:
			case <-time.After(time.Second):
				t.Fatal("media worker failed to release its root reader")
			}
		})
	}
}
