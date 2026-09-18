package noteapp

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

func noteFixture(t *testing.T) (*Manager, *viewer, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "observations.worldr-note.md")
	if err := os.WriteFile(path, []byte("# Log\nfirst"), 0640); err != nil {
		t.Fatal(err)
	}
	manager := NewManager()
	t.Cleanup(func() { _ = manager.Close() })
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.OpenFile(file, filepath.Base(path)); err != nil {
		file.Close()
		t.Fatal(err)
	}
	return manager, manager.slots[0], path
}

func waitNote(t *testing.T, manager *Manager, view *viewer) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := manager.Poll(); err != nil {
			t.Fatal(err)
		}
		if !view.saving {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("note save did not finish")
}

func TestFileNoteEditsSavesAtomicallyAndRestoresExactBuffer(t *testing.T) {
	manager, view, path := noteFixture(t)
	old, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	manager.Focus(view.id)
	view.editor.setCaret(len(view.editor.text), false)
	context := manager.TextInput(view.id).ContextID
	manager.Send(view.id, experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: true})
	manager.Send(view.id, experience.Event{Kind: experience.TextCommit, TextContext: context, Text: "観測 λ"})
	if view.editor.text != "# Log\nfirst\n観測 λ" || !view.state.Dirty {
		t.Fatal("multiline IME edit did not reach the document", view.editor.text)
	}
	manager.Send(view.id, experience.Event{Kind: experience.KeyInput, Keycode: 31, Pressed: true, Modifiers: experience.ModControl})
	waitNote(t, manager, view)
	data, err := os.ReadFile(path)
	if err != nil || string(data) != view.editor.text || view.state.Dirty {
		t.Fatal("atomic save did not commit exact note", string(data), err, view.message)
	}
	oldData, err := io.ReadAll(old)
	if err != nil || string(oldData) != "# Log\nfirst" {
		t.Fatal("save modified the old inode instead of atomically replacing it", string(oldData), err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0640 {
		t.Fatal("save did not preserve file permissions", info, err)
	}

	manager.Send(view.id, experience.Event{Kind: experience.TextCommit, TextContext: context, Text: " unsaved"})
	view.state.TopLine, view.state.Left = 1, 2
	saved := manager.SessionStates()[0]
	restored := NewManager()
	defer restored.Close()
	saved.Key = "native:note-3"
	if _, err := restored.Restore(saved); err != nil {
		t.Fatal(err)
	}
	got := restored.SessionStates()[0]
	if !reflect.DeepEqual(got, saved) || restored.slots[2] == nil || restored.slots[2].focused {
		t.Fatalf("session did not restore exact note state:\n got %#v\nwant %#v", got, saved)
	}
}

func TestNoteSaveRefusesExternalReplacement(t *testing.T) {
	manager, view, path := noteFixture(t)
	manager.Focus(view.id)
	context := manager.TextInput(view.id).ContextID
	manager.Send(view.id, experience.Event{Kind: experience.TextCommit, TextContext: context, Text: "local "})
	if err := os.WriteFile(path, []byte("external"), 0640); err != nil {
		t.Fatal(err)
	}
	manager.Send(view.id, experience.Event{Kind: experience.KeyInput, Keycode: 31, Pressed: true, Modifiers: experience.ModControl})
	waitNote(t, manager, view)
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "external" || !view.state.Dirty || !strings.Contains(view.message, "changed on disk") {
		t.Fatal("conflicting save overwrote external work", string(data), view.message, err)
	}
}

func TestNoteLaunchClipboardLeaseAndCloseConfirmation(t *testing.T) {
	manager := NewManager()
	defer manager.Close()
	launches := manager.ApplicationLaunches()
	if len(launches) != 1 || launches[0].Kind != "note" {
		t.Fatal("note missing from native launcher", launches)
	}
	if key, err := manager.LaunchApplication("note"); err != nil || key != "native:note" {
		t.Fatal("blank note launch failed", key, err)
	}
	if key, err := manager.LaunchApplication("note"); err != nil || key != "native:note-2" {
		t.Fatal("second note did not get an independent slot", key, err)
	}
	first, second := manager.slots[0], manager.slots[1]
	manager.Focus(first.id)
	context := manager.TextInput(first.id).ContextID
	manager.Send(first.id, experience.Event{Kind: experience.TextCommit, TextContext: context, Text: "copy me"})
	manager.Send(first.id, experience.Event{Kind: experience.KeyInput, Keycode: 30, Pressed: true, Modifiers: experience.ModControl})
	manager.Send(first.id, experience.Event{Kind: experience.KeyInput, Keycode: 46, Pressed: true, Modifiers: experience.ModControl})
	if copied, ok := manager.TakeCopy(); !ok || copied != "copy me" {
		t.Fatal("note selection did not reach clipboard", copied, ok)
	}
	manager.Send(first.id, experience.Event{Kind: experience.KeyInput, Keycode: 47, Pressed: true, Modifiers: experience.ModControl})
	if !manager.TakePasteRequest() {
		t.Fatal("paste request was not leased")
	}
	manager.Focus(second.id)
	if err := manager.Paste("stale"); err != nil || second.editor.text != "" || first.editor.text != "copy me" {
		t.Fatal("stale paste crossed note focus", first.editor.text, second.editor.text, err)
	}

	manager.CloseApplication(first.id)
	if manager.slots[0] == nil || !first.closeArmed {
		t.Fatal("first close discarded unsaved work without confirmation")
	}
	manager.CloseApplication(first.id)
	if manager.slots[0] != nil || len(manager.RetiredTextures()) != 1 || manager.slots[1] != second {
		t.Fatal("confirmed close leaked or removed the sibling note")
	}
}
