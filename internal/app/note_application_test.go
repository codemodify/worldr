package app

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/noteapp"
	"github.com/codemodify/worldr/internal/projectapp"
	"github.com/codemodify/worldr/internal/workspace"
)

func TestFilesOpensExplicitDocumentInNativeNoteEditor(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "field-log.worldr-note.md")
	if err := os.WriteFile(path, []byte("# Field log\nready\n"), 0600); err != nil {
		t.Fatal(err)
	}
	browser, err := projectapp.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	notes := noteapp.NewManager()
	hub := newApplicationHub(browser, notes)
	work, err := workspace.NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Close()
	defer hub.Close()
	work.SetApplications(hub)
	defer work.SetApplications(nil)
	connectFileBrowserWithNativeTools(browser, nil, nil, nil, notes, hub, work)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := hub.Poll(); err != nil {
			t.Fatal(err)
		}
		if state, ok := browser.SessionState(); ok && state.Selected == filepath.Base(path) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	work.Draw(1440, 900)
	browser.Focus(1)
	browser.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: true})
	for time.Now().Before(deadline) {
		if err := hub.Poll(); err != nil {
			t.Fatal(err)
		}
		if surfaces := notes.Surfaces(); len(surfaces) == 1 {
			if surfaces[0].Key != "native:note" || surfaces[0].AppID != "worldr.native-note" || hub.focused != 0 {
				t.Fatal("native note identity or focus changed", surfaces[0], hub.focused)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("Files did not open the Worldr note")
}

func TestWorkspaceSessionRestoresExactUntitledNote(t *testing.T) {
	work, err := workspace.NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Close()
	notes := noteapp.NewManager()
	defer notes.Close()
	if _, err := notes.LaunchApplication("note"); err != nil {
		t.Fatal(err)
	}
	surface := notes.Surfaces()[0]
	notes.Focus(surface.ID)
	input := notes.TextInput(surface.ID)
	notes.Send(surface.ID, experience.Event{Kind: experience.TextCommit, TextContext: input.ContextID, Text: "unsaved 研究\nsecond line"})
	want := notes.SessionStates()
	if len(want) != 1 || !want[0].Dirty || want[0].Source != "" {
		t.Fatal("untitled note did not enter session state", want)
	}
	manifest := (&workspaceSession{work: work, notes: notes}).snapshot()
	restored := noteapp.NewManager()
	hub := newApplicationHub(restored)
	defer hub.Close()
	var output bytes.Buffer
	session := &workspaceSession{work: work, notes: restored}
	if failures := session.restore(manifest, hub, &output); len(failures) != 0 {
		t.Fatal(failures, output.String())
	}
	if got := restored.SessionStates(); !reflect.DeepEqual(got, want) || hub.focused != 0 {
		t.Fatalf("untitled note restore changed content, selection or focus:\n got %#v\nwant %#v", got, want)
	}
}
