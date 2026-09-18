package projectapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

func TestNoteOpenUsesExplicitWorldrDocumentType(t *testing.T) {
	for _, name := range []string{"lab.worldr-note.md", "PLAN.WORLDR-NOTE.MD"} {
		if !IsNotePath(name) || !mediaPath(name) {
			t.Fatal("Worldr note was not routed to the native editor", name)
		}
		provider, reader := controlledProvider(t)
		listing := nextCall(t, reader)
		listing.answer <- result{request: listing.request, entries: []entry{{name: name, kind: fileEntry}}}
		pollUntil(t, provider, func() bool { return !provider.loadingDirectory })
		var opened string
		provider.SetOpenHandler(func(file *os.File, label string) error {
			opened = label
			return file.Close()
		})
		provider.Focus(1)
		provider.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: true})
		call := nextCall(t, reader)
		if !call.request.openMedia {
			t.Fatal("note did not request an anchored descriptor")
		}
		path := filepath.Join(t.TempDir(), "note")
		if err := os.WriteFile(path, []byte("# Native note\n"), 0600); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		call.answer <- result{request: call.request, file: file}
		pollUntil(t, provider, func() bool { return !provider.loadingFile })
		if opened != name || !strings.Contains(provider.message, "native note editor") {
			t.Fatal("note did not reach native editor", opened, provider.message)
		}
		provider.Close()
	}
	for _, name := range []string{"README.md", "notes.txt", "worldr-note.md", "draft.worldr-note.md.bak"} {
		if IsNotePath(name) {
			t.Fatal("ordinary document was claimed as a Worldr note", name)
		}
	}
}
