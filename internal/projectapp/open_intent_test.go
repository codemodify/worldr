package projectapp

import (
	"os"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

func TestOpenIntentFiltersNativeDocumentsAndRetainsFolders(t *testing.T) {
	for _, test := range []struct {
		name   string
		intent OpenIntent
		file   string
	}{
		{name: "photo", intent: OpenPhoto, file: "frame.PNG"},
		{name: "video", intent: OpenVideo, file: "capture.webm"},
		{name: "model", intent: OpenModel, file: "mount.obj"},
		{name: "dataset", intent: OpenDataset, file: "signals.csv"},
	} {
		t.Run(test.name, func(t *testing.T) {
			p, reader := controlledProvider(t)
			p.SetOpenIntent(test.intent)
			listing := nextCall(t, reader)
			listing.answer <- result{entries: []entry{
				{name: "nested", kind: directoryEntry},
				{name: test.file, kind: fileEntry},
				{name: "notes.txt", kind: fileEntry},
				{name: "other-link", kind: symlinkEntry},
			}}
			pollUntil(t, p, func() bool { return !p.loadingDirectory })
			if p.openIntent != test.intent || len(p.entries) != 2 || p.entries[0].name != "nested" || p.entries[1].name != test.file {
				t.Fatalf("chooser entries = %#v, intent = %v", p.entries, p.openIntent)
			}
			if p.openIntent.title() == "" || !strings.Contains(p.openIntent.instruction(), "folders remain available") {
				t.Fatal("chooser did not expose a visible purpose and navigation instruction")
			}
		})
	}
}

func TestOpenIntentClearsAfterSuccessfulHandoff(t *testing.T) {
	p, reader := controlledProvider(t)
	p.SetOpenIntent(OpenPhoto)
	listing := nextCall(t, reader)
	listing.answer <- result{entries: []entry{
		{name: "image.png", kind: fileEntry},
		{name: "movie.mp4", kind: fileEntry},
	}}
	pollUntil(t, p, func() bool { return p.selected == 0 })
	opened := false
	p.SetOpenHandler(func(file *os.File, name string) error {
		opened = name == "image.png"
		return file.Close()
	})
	p.openSelected()
	call := nextCall(t, reader)
	call.answer <- result{file: mediaFile(t)}
	pollUntil(t, p, func() bool { return !p.loadingFile })
	if !opened || p.openIntent != OpenAny || len(p.entries) != 2 || !strings.Contains(p.message, "photo viewer") {
		t.Fatal("successful handoff did not leave the browser in its ordinary mode", opened, p.openIntent, p.message)
	}
}

func TestOpenIntentEscapeRestoresOrdinaryFiles(t *testing.T) {
	p, reader := controlledProvider(t)
	p.SetOpenIntent(OpenDataset)
	listing := nextCall(t, reader)
	listing.answer <- result{entries: []entry{
		{name: "signals.csv", kind: fileEntry},
		{name: "README.md", kind: fileEntry},
	}}
	pollUntil(t, p, func() bool { return len(p.entries) == 1 })
	p.Focus(1)
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 1, Pressed: true})
	if p.openIntent != OpenAny || len(p.entries) != 2 {
		t.Fatal("Escape did not restore the ordinary Files listing")
	}
}
