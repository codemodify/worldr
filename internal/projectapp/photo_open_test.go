package projectapp

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

func photoProvider(t *testing.T, name string) (*Provider, *controlledReader) {
	t.Helper()
	p, reader := controlledProvider(t)
	listing := nextCall(t, reader)
	listing.answer <- result{entries: []entry{{name: name, kind: fileEntry}, {name: "second.webp", kind: fileEntry}, {name: "clip.mp4", kind: fileEntry}}}
	pollUntil(t, p, func() bool { return p.selected == 0 })
	return p, reader
}

func TestPhotoExtensionsMatchSupportedViewerFormats(t *testing.T) {
	for _, extension := range []string{"jpg", "jpeg", "png", "webp", "gif", "bmp"} {
		for _, suffix := range []string{extension, strings.ToUpper(extension)} {
			name := "gallery/Photo." + suffix
			if !IsPhotoPath(name) || !mediaPath(name) || videoPath(name) {
				t.Fatalf("photo filename %q did not retain its distinct viewer classification", name)
			}
		}
	}
	for _, name := range []string{"photo", "jpg", "photo.jpg.txt", "photos.png/notes", "movie.mp4", "image.tiff", "image.svg", "image.avif", "image.heic"} {
		if IsPhotoPath(name) {
			t.Fatalf("unsupported photo filename %q was routed to the photo viewer", name)
		}
	}
}

func TestPhotoFormatsLaunchOnlyOnExplicitActivationFromHostPoll(t *testing.T) {
	for _, extension := range []string{"JPG", "JPEG", "PNG", "WEBP", "GIF", "BMP"} {
		for _, activation := range []string{"enter", "toolbar", "double-click"} {
			t.Run(extension+"/"+activation, func(t *testing.T) {
				name := "λ-photo." + extension
				p, reader := photoProvider(t, name)
				var opened *os.File
				p.SetOpenHandler(func(file *os.File, displayName string) error {
					if displayName != name {
						t.Errorf("callback name = %q, want %q", displayName, name)
					}
					opened = file
					return nil
				})
				p.selectEntry(0)
				if p.loadingFile || len(reader.calls) != 0 || opened != nil || p.preview.text != "" || !strings.Contains(p.message, "Photo file") {
					t.Fatal("photo selection read binary content, launched a viewer, or omitted the photo hint")
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
				if !call.request.openMedia || call.request.directory || call.request.path != name {
					t.Fatal("explicit photo open queued a text preview or wrong file")
				}
				file := mediaFile(t)
				call.answer <- result{file: file}
				await(t, func() bool { return len(p.results) == 1 })
				if opened != nil {
					t.Fatal("photo handler ran outside host Poll")
				}
				if err := p.Poll(); err != nil || opened != file || p.message != "Opened photo in the photo viewer." {
					t.Fatal("photo handoff failed or retained a video status", err, p.message)
				}
				generation := p.generation
				p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: true, Repeat: true})
				if p.generation != generation || p.loadingFile {
					t.Fatal("held Enter repeatedly launched the photo viewer")
				}
				p.Close()
				if _, err := opened.Stat(); err != nil {
					t.Fatal("browser close revoked the photo viewer's descriptor", err)
				}
			})
		}
	}
}

func TestPhotoOpenFailuresCloseFilesAndReportPhotoStatus(t *testing.T) {
	for _, mode := range []string{"unavailable", "handler-error", "reader-error", "missing-descriptor"} {
		t.Run(mode, func(t *testing.T) {
			p, reader := photoProvider(t, "image.png")
			if mode == "handler-error" || mode == "missing-descriptor" {
				p.SetOpenHandler(func(*os.File, string) error { return errors.New("image decoder unavailable") })
			}
			p.openSelected()
			call := nextCall(t, reader)
			answer := result{}
			if mode != "missing-descriptor" {
				answer.file = mediaFile(t)
			}
			if mode == "reader-error" {
				answer.err = errors.New("permission denied")
			}
			call.answer <- answer
			pollUntil(t, p, func() bool { return !p.loadingFile })
			if answer.file != nil {
				assertClosedFile(t, answer.file)
			}
			if p.err != nil || p.message == "" || strings.Contains(p.message, "video") || strings.Contains(p.message, "Opened") {
				t.Fatal("failed photo open crashed the browser or displayed an incorrect status")
			}
			if mode != "reader-error" && !strings.Contains(strings.ToLower(p.message), "photo") {
				t.Fatal("photo failure did not identify its viewer", p.message)
			}
		})
	}
}

func TestPhotoPendingHandoffIsRevokedBySelectionOrClose(t *testing.T) {
	for _, mode := range []string{"another-photo", "video", "closed"} {
		t.Run(mode, func(t *testing.T) {
			p, reader := photoProvider(t, "image.gif")
			called := false
			p.SetOpenHandler(func(*os.File, string) error { called = true; return nil })
			p.openSelected()
			call := nextCall(t, reader)
			file := mediaFile(t)
			call.answer <- result{file: file}
			await(t, func() bool { return len(p.results) == 1 })
			switch mode {
			case "another-photo":
				p.selectEntry(1)
			case "video":
				p.selectEntry(2)
			case "closed":
				p.Close()
			}
			if err := p.Poll(); err != nil || called {
				t.Fatal("canceled photo reached the viewer", err)
			}
			assertClosedFile(t, file)
			if len(reader.calls) != 0 {
				t.Fatal("replacement media selection automatically opened another file")
			}
		})
	}
}
