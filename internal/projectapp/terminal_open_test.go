package projectapp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

func terminalDirectory(t *testing.T) *os.File {
	t.Helper()
	file, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func terminalShortcut(code uint32) experience.Event {
	return experience.Event{Kind: experience.KeyInput, Keycode: code, Pressed: true, Modifiers: experience.ModControl | experience.ModShift}
}

func TestTerminalHereTargetsSelectedFolderOrCurrentFolderFromPoll(t *testing.T) {
	for _, selected := range []string{"folder", "text", "photo", "video", "empty"} {
		for _, activation := range []string{"shortcut", "button"} {
			t.Run(selected+"/"+activation, func(t *testing.T) {
				p, reader := controlledProvider(t)
				listing := nextCall(t, reader)
				var entries []entry
				switch selected {
				case "folder":
					entries = []entry{{name: "src", kind: directoryEntry}}
				case "text":
					entries = []entry{{name: "README", kind: fileEntry}}
				case "photo":
					entries = []entry{{name: "image.jpg", kind: fileEntry}}
				case "video":
					entries = []entry{{name: "clip.mp4", kind: fileEntry}}
				}
				listing.answer <- result{entries: entries}
				pollUntil(t, p, func() bool { return !p.loadingDirectory })
				if selected == "text" {
					preview := nextCall(t, reader)
					preview.answer <- result{preview: makePreview("retained preview")}
					pollUntil(t, p, func() bool { return !p.loadingFile })
				}
				p.Resize(1, minWidth, minHeight)
				beforeSelection, beforePreview, beforeMessage := p.selected, p.preview.text, p.message
				file := terminalDirectory(t)
				calls, displayPath := 0, ""
				p.SetTerminalHandler(func(directory *os.File, path string) error {
					calls++
					displayPath = path
					if info, err := directory.Stat(); err != nil || !info.IsDir() || directory != file {
						t.Fatal("callback did not borrow the opened directory", err)
					}
					return nil
				})
				p.Focus(1)
				if activation == "shortcut" {
					p.Send(1, terminalShortcut(28))
				} else {
					p.Send(1, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 450, Y: 70})
				}
				call := nextCall(t, reader)
				wantPath := ""
				if selected == "folder" {
					wantPath = "src"
				}
				if !call.request.openTerminal || !call.request.directory || call.request.openMedia || call.request.path != wantPath {
					t.Fatalf("wrong directory request: %+v", call.request)
				}
				generation := p.generation
				p.Send(1, terminalShortcut(28))
				if p.generation != generation {
					t.Fatal("a pending terminal request was duplicated")
				}
				call.answer <- result{file: file}
				await(t, func() bool { return len(p.results) == 1 })
				if calls != 0 {
					t.Fatal("worker invoked the callback before Poll")
				}
				if err := p.Poll(); err != nil {
					t.Fatal(err)
				}
				if calls != 1 || displayPath != filepath.Join("/project", wantPath) {
					t.Fatalf("callback calls=%d path=%q", calls, displayPath)
				}
				assertClosedFile(t, file)
				if p.selected != beforeSelection || len(p.entries) != len(entries) || p.preview.text != beforePreview || p.message != beforeMessage || p.directory != "" {
					t.Fatal("Terminal Here changed the listing, preview, selection, or current directory")
				}
				if p.openingTerminal || !strings.Contains(p.notice, "Opened terminal") {
					t.Fatal("successful launch did not finish with a visible status")
				}
			})
		}
	}
}

func TestTerminalShortcutRequiresFocusPressAndBothModifiersWithoutRepeat(t *testing.T) {
	p, reader := videoProvider(t)
	generation := p.generation
	p.Send(1, terminalShortcut(28)) // No keyboard focus yet.
	p.Focus(1)
	for _, change := range []func(*experience.Event){
		func(e *experience.Event) { e.Pressed = false },
		func(e *experience.Event) { e.Repeat = true },
		func(e *experience.Event) { e.Modifiers = experience.ModControl },
		func(e *experience.Event) { e.Modifiers |= experience.ModAlt },
		func(e *experience.Event) { e.Modifiers |= experience.ModSuper },
	} {
		event := terminalShortcut(28)
		change(&event)
		p.Send(1, event)
	}
	if p.generation != generation || len(reader.calls) != 0 {
		t.Fatal("unfocused, repeated, released, or unrelated shortcut opened a terminal")
	}
	p.Send(1, terminalShortcut(96)) // Keypad Enter has the same action.
	call := nextCall(t, reader)
	call.answer <- result{file: terminalDirectory(t)}
	pollUntil(t, p, func() bool { return !p.openingTerminal })
	generation = p.generation
	event := terminalShortcut(96)
	event.Repeat = true
	p.Send(1, event)
	if p.generation != generation {
		t.Fatal("held shortcut launched another terminal after completion")
	}
}

func TestTerminalHereResumesPendingTextPreview(t *testing.T) {
	p, reader := controlledProvider(t)
	listing := nextCall(t, reader)
	listing.answer <- result{entries: []entry{{name: "README", kind: fileEntry}}}
	pollUntil(t, p, func() bool { return p.selected == 0 })
	preview := nextCall(t, reader)
	p.terminalHere()
	call := nextCall(t, reader)
	if !call.request.resumePreview || preview.request.ctx.Err() == nil {
		t.Fatal("terminal did not cancel and remember the pending preview")
	}
	call.answer <- result{file: terminalDirectory(t)}
	pollUntil(t, p, func() bool { return !p.openingTerminal })
	resumed := nextCall(t, reader)
	if resumed.request.path != "README" || resumed.request.directory || resumed.request.openTerminal || !p.loadingFile {
		t.Fatal("interrupted text preview was not resumed")
	}
	resumed.answer <- result{preview: makePreview("preview completed")}
	pollUntil(t, p, func() bool { return !p.loadingFile })
	if p.preview.text != "preview completed" || !strings.Contains(p.notice, "unavailable") || p.selected != 0 {
		t.Fatal("preview completion discarded terminal status or selection")
	}
}

func TestTerminalHereErrorsAreVisibleAndAlwaysCloseBorrowedDirectory(t *testing.T) {
	for _, mode := range []string{"no-handler", "handler-error", "reader-error", "nil-file", "close-in-handler"} {
		t.Run(mode, func(t *testing.T) {
			p, reader := videoProvider(t)
			called := false
			if mode != "no-handler" {
				p.SetTerminalHandler(func(*os.File, string) error {
					called = true
					if mode == "close-in-handler" {
						return p.Close()
					}
					return errors.New("terminal capacity exceeded\nretry after closing a terminal")
				})
			}
			p.terminalHere()
			call := nextCall(t, reader)
			file := terminalDirectory(t)
			answer := result{file: file}
			if mode == "reader-error" {
				answer.err = errors.New("permission denied")
			} else if mode == "nil-file" {
				answer.file = nil
				_ = file.Close()
			}
			call.answer <- answer
			pollUntil(t, p, func() bool { return !p.openingTerminal })
			assertClosedFile(t, file)
			if called != (mode == "handler-error" || mode == "close-in-handler") {
				t.Fatal("failed read or unavailable callback was invoked")
			}
			if mode != "close-in-handler" && (p.notice == "" || strings.Contains(p.notice, "\n") || strings.Contains(p.notice, "Opened terminal")) {
				t.Fatal("error did not produce a safe visible status", p.notice)
			}
		})
	}
}

func TestTerminalHereRejectsSelectedSymlinkAndPreservesPendingListing(t *testing.T) {
	p, reader := controlledProvider(t)
	listing := nextCall(t, reader)
	generation := p.generation
	p.terminalHere()
	if generation != p.generation || listing.request.ctx.Err() != nil || !strings.Contains(p.notice, "Wait") {
		t.Fatal("Terminal Here canceled the pending initial listing")
	}
	listing.answer <- result{entries: []entry{{name: "link", kind: symlinkEntry}}}
	pollUntil(t, p, func() bool { return p.selected == 0 })
	generation = p.generation
	p.terminalHere()
	if p.generation != generation || !strings.Contains(p.notice, "symbolic links") || !strings.Contains(p.notice, "Select") {
		t.Fatal("selected symlink was followed or lacked an actionable refusal")
	}
}

func TestTerminalHereWaitStatusClearsWhenListingOrMediaOpenFinishes(t *testing.T) {
	for _, operation := range []string{"empty-listing", "media-open"} {
		t.Run(operation, func(t *testing.T) {
			var p *Provider
			var reader *controlledReader
			var call readerCall
			answer := result{}
			if operation == "empty-listing" {
				p, reader = controlledProvider(t)
				call = nextCall(t, reader)
			} else {
				p, reader = videoProvider(t)
				p.openSelected()
				call = nextCall(t, reader)
				answer.file = mediaFile(t)
			}
			generation := p.generation
			p.terminalHere()
			if p.generation != generation || call.request.ctx.Err() != nil || !strings.Contains(p.notice, "Wait") {
				t.Fatal("terminal action canceled the pending listing/media open")
			}
			call.answer <- answer
			pollUntil(t, p, func() bool { return !p.loadingDirectory && !p.loadingFile })
			if p.notice != "" || p.openingTerminal {
				t.Fatal("completed operation left a misleading wait status")
			}
		})
	}
}

func TestTerminalHereClosesBufferedDirectoryAfterCancellation(t *testing.T) {
	for _, mode := range []string{"selection", "refresh", "close"} {
		t.Run(mode, func(t *testing.T) {
			p, reader := videoProvider(t)
			p.SetTerminalHandler(func(*os.File, string) error { t.Fatal("stale callback invoked"); return nil })
			p.terminalHere()
			call := nextCall(t, reader)
			file := terminalDirectory(t)
			call.answer <- result{file: file}
			await(t, func() bool { return len(p.results) == 1 })
			switch mode {
			case "selection":
				p.selectEntry(1)
			case "refresh":
				p.refresh()
			case "close":
				p.Close()
			}
			if err := p.Poll(); err != nil {
				t.Fatal(err)
			}
			assertClosedFile(t, file)
			if p.openingTerminal {
				t.Fatal("canceled terminal action retained its busy state")
			}
		})
	}
}

type lateTerminalReader struct {
	started, release chan struct{}
	file             *os.File
}

func (r *lateTerminalReader) read(req request) result {
	if !req.openTerminal {
		return result{request: req, entries: []entry{{name: "clip.mp4", kind: fileEntry}, {name: "folder", kind: directoryEntry}}}
	}
	close(r.started)
	<-r.release
	return result{request: req, file: r.file}
}
func (*lateTerminalReader) close() error { return nil }

func TestTerminalHereClosesLateDirectoryAfterCancellation(t *testing.T) {
	for _, mode := range []string{"selection", "close"} {
		t.Run(mode, func(t *testing.T) {
			reader := &lateTerminalReader{started: make(chan struct{}), release: make(chan struct{}), file: terminalDirectory(t)}
			p, err := newProvider(reader, "/project")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { p.Close() })
			pollUntil(t, p, func() bool { return p.selected == 0 })
			p.SetTerminalHandler(func(*os.File, string) error { t.Fatal("late callback invoked"); return nil })
			p.terminalHere()
			select {
			case <-reader.started:
			case <-time.After(time.Second):
				t.Fatal("directory open never started")
			}
			if mode == "selection" {
				p.selectEntry(1)
			} else {
				p.Close()
			}
			close(reader.release)
			await(t, func() bool { _, err := reader.file.Stat(); return errors.Is(err, os.ErrClosed) })
			p.Poll()
			p.Close()
			select {
			case <-p.done:
			case <-time.After(time.Second):
				t.Fatal("worker did not finish")
			}
		})
	}
}
