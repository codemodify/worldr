//go:build linux

package projectapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

func operationFor(t *testing.T, r projectReader, action fileAction, source, target string) fileOperation {
	t.Helper()
	op := fileOperation{action: action, source: source, target: target}
	if source != "" {
		parent := filepath.Dir(source)
		if parent == "." {
			parent = ""
		}
		listing := readPath(r, parent, true)
		if listing.err != nil {
			t.Fatal(listing.err)
		}
		found := false
		for _, item := range listing.entries {
			if item.name == filepath.Base(source) {
				op.expected = item.stamp
				found = true
				break
			}
		}
		if !found {
			t.Fatal("operation fixture not listed", source)
		}
	}
	return op
}

func perform(t *testing.T, r projectReader, op fileOperation) result {
	t.Helper()
	res := r.read(request{ctx: context.Background(), operation: &op})
	if res.err != nil {
		t.Fatal(res.err)
	}
	return res
}

func TestFileOperationsCreateDuplicateRenameMoveTrashAndUndoWithoutOverwrite(t *testing.T) {
	root := t.TempDir()
	content := []byte("original bytes\x00\xff")
	putFile(t, filepath.Join(root, "original.bin"), content)
	r := fixtureReader(t, root)
	created := perform(t, r, operationFor(t, r, createFolder, "", "folder"))
	if created.undo == nil || !created.undo.created {
		t.Fatal("folder creation cannot be undone")
	}
	copy := perform(t, r, operationFor(t, r, duplicateItem, "original.bin", "copy.bin"))
	if data, err := os.ReadFile(filepath.Join(root, "copy.bin")); err != nil || !bytes.Equal(data, content) {
		t.Fatal("copy lost bytes", err)
	}
	rename := perform(t, r, operationFor(t, r, renameItem, "copy.bin", "renamed.bin"))
	move := perform(t, r, operationFor(t, r, moveItem, "renamed.bin", "folder/moved.bin"))
	trash := perform(t, r, operationFor(t, r, trashItem, "folder/moved.bin", ""))
	if trash.undo == nil || !restorableTrashItem(trash.undo.target) {
		t.Fatal("Trash did not retain a recoverable location")
	}
	if data, err := os.ReadFile(filepath.Join(root, trash.undo.target)); err != nil || !bytes.Equal(data, content) {
		t.Fatal("Trash lost file contents", err)
	}
	for _, undo := range []*fileUndo{trash.undo, move.undo, rename.undo, copy.undo} {
		perform(t, r, fileOperation{action: undoFileAction, undo: undo})
	}
	if _, err := os.Stat(filepath.Join(root, "copy.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("undo did not withdraw duplicate", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "original.bin")); err != nil || !bytes.Equal(data, content) {
		t.Fatal("operations modified the original", err)
	}
	// The moved file changed this folder, so stale creation undo must preserve it.
	res := r.read(request{ctx: context.Background(), operation: &fileOperation{action: undoFileAction, undo: created.undo}})
	if res.err == nil {
		t.Fatal("Undo removed a directory changed since creation")
	}
	for _, name := range []string{"original.bin", projectTrash, "folder"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTrashRestoreSurvivesReaderRestartAndRetainsArbitraryFilenameBytes(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0700); err != nil {
		t.Fatal(err)
	}
	source := "docs/report-\xff.bin"
	putFile(t, filepath.Join(root, source), []byte("recover me"))
	r := fixtureReader(t, root)
	trashed := perform(t, r, operationFor(t, r, trashItem, source, ""))
	r.close()
	reopened := fixtureReader(t, root)
	if data, err := os.ReadFile(filepath.Join(root, filepath.Dir(filepath.Dir(trashed.undo.target)), "restore.json")); err != nil || !json.Valid(data) {
		t.Fatal("Trash metadata is not durable valid JSON", err)
	}
	perform(t, reopened, operationFor(t, reopened, restoreTrashItem, trashed.undo.target, ""))
	if data, err := os.ReadFile(filepath.Join(root, source)); err != nil || string(data) != "recover me" {
		t.Fatal("restart recovery substituted filename bytes or lost content", err)
	}
}

func TestFileOperationsRejectOccupiedDestinationsChangedIdentityAndSymlinkTraversal(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	putFile(t, filepath.Join(root, "source"), []byte("source"))
	putFile(t, filepath.Join(root, "occupied"), []byte("keep me"))
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	r := fixtureReader(t, root)
	for _, op := range []fileOperation{
		operationFor(t, r, renameItem, "source", "occupied"),
		operationFor(t, r, duplicateItem, "source", "occupied"),
		operationFor(t, r, moveItem, "source", "link/outside"),
		operationFor(t, r, createFolder, "", "link/created"),
		operationFor(t, r, createFolder, "", "../escape"),
		operationFor(t, r, renameItem, "link", "renamed-link"),
	} {
		if res := r.read(request{ctx: context.Background(), operation: &op}); res.err == nil {
			t.Fatalf("unsafe operation succeeded: %+v", op)
		}
	}
	stale := operationFor(t, r, renameItem, "source", "renamed")
	if err := os.Rename(filepath.Join(root, "source"), filepath.Join(root, "original-source")); err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(root, "source"), []byte("replacement"))
	if res := r.read(request{ctx: context.Background(), operation: &stale}); res.err == nil {
		t.Fatal("stale selection moved a replacement inode")
	}
	if data, err := os.ReadFile(filepath.Join(root, "occupied")); err != nil || string(data) != "keep me" {
		t.Fatal("occupied destination was overwritten", err)
	}
	if items, err := os.ReadDir(outside); err != nil || len(items) != 0 {
		t.Fatal("operation escaped through a symlink", err)
	}
	items, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if strings.HasPrefix(item.Name(), ".worldr-copy-") || strings.HasPrefix(item.Name(), ".worldr-folder-") {
			t.Fatal("failed operation leaked a temporary item")
		}
	}
}

func TestUndoAndTrashRestoreRefuseOverwriteOrChangedCreatedContent(t *testing.T) {
	root := t.TempDir()
	putFile(t, filepath.Join(root, "source"), []byte("source"))
	r := fixtureReader(t, root)
	duplicate := perform(t, r, operationFor(t, r, duplicateItem, "source", "copy"))
	putFile(t, filepath.Join(root, "copy"), []byte("edited copy must remain"))
	if res := r.read(request{ctx: context.Background(), operation: &fileOperation{action: undoFileAction, undo: duplicate.undo}}); res.err == nil {
		t.Fatal("Undo removed an edited copy")
	}
	trash := perform(t, r, operationFor(t, r, trashItem, "source", ""))
	putFile(t, filepath.Join(root, "source"), []byte("new occupant"))
	for _, op := range []fileOperation{{action: undoFileAction, undo: trash.undo}, operationFor(t, r, restoreTrashItem, trash.undo.target, "")} {
		if res := r.read(request{ctx: context.Background(), operation: &op}); res.err == nil {
			t.Fatal("recovery overwrote a new occupant")
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, trash.undo.target)); err != nil || string(data) != "source" {
		t.Fatal("failed recovery lost the trashed file", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "copy")); err != nil || string(data) != "edited copy must remain" {
		t.Fatal("changed copy was removed", err)
	}
	metadata := filepath.Join(root, filepath.Dir(filepath.Dir(trash.undo.target)), "restore.json")
	invalid, _ := json.Marshal(trashRecord{Version: 1, Original: []byte("../source")})
	putFile(t, metadata, invalid)
	op := operationFor(t, r, restoreTrashItem, trash.undo.target, "")
	if res := r.read(request{ctx: context.Background(), operation: &op}); res.err == nil {
		t.Fatal("recovery metadata escaped the project")
	}
}

type cancelDuringCopy struct {
	context.Context
	calls int
}

func (c *cancelDuringCopy) Err() error {
	c.calls++
	if c.calls >= 7 {
		return context.Canceled
	}
	return nil
}

func TestDuplicateCancellationAndSizeBoundsLeaveNoPartialDestination(t *testing.T) {
	root := t.TempDir()
	putFile(t, filepath.Join(root, "source"), bytes.Repeat([]byte("x"), 256<<10))
	r := fixtureReader(t, root)
	op := operationFor(t, r, duplicateItem, "source", "copy")
	ctx := &cancelDuringCopy{Context: context.Background()}
	if res := r.read(request{ctx: ctx, operation: &op}); !errors.Is(res.err, context.Canceled) {
		t.Fatal("copy did not observe cancellation", res.err)
	}
	file, err := os.OpenFile(filepath.Join(root, "large"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(maxDuplicateBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	op = operationFor(t, r, duplicateItem, "large", "oversize-copy")
	if res := r.read(request{ctx: context.Background(), operation: &op}); res.err == nil {
		t.Fatal("unbounded copy accepted")
	}
	items, err := os.ReadDir(root)
	if err != nil || len(items) != 2 {
		t.Fatal("canceled/oversized copy left partial files", items, err)
	}
}

func TestFileOperationsRemainAnchoredAfterRootRename(t *testing.T) {
	base := t.TempDir()
	root, moved := filepath.Join(base, "project"), filepath.Join(base, "moved")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(root, "source"), []byte("original"))
	r := fixtureReader(t, root)
	op := operationFor(t, r, renameItem, "source", "renamed")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(root, "source"), []byte("replacement"))
	perform(t, r, op)
	perform(t, r, operationFor(t, r, createFolder, "", "created"))
	if data, err := os.ReadFile(filepath.Join(moved, "renamed")); err != nil || string(data) != "original" {
		t.Fatal("renamed root lost operation anchor", err)
	}
	if _, err := os.Stat(filepath.Join(root, "created")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("operation entered replacement root")
	}
}

func TestProviderFileDialogKeyboardButtonUndoAndNoRepeat(t *testing.T) {
	root := t.TempDir()
	p, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	pollUntil(t, p, func() bool { return !p.loadingDirectory })
	p.Focus(1)
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 49, Pressed: true, Modifiers: experience.ModControl | experience.ModShift})
	for _, code := range []uint32{49, 24, 20, 18, 31} {
		p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: code, Pressed: true})
	}
	if p.dialog == nil || p.dialog.field.Text() != "notes" {
		t.Fatal("new-folder text dialog did not receive native input")
	}
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: true})
	generation := p.generation
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: true, Repeat: true})
	if p.generation != generation {
		t.Fatal("held Enter repeated a mutation")
	}
	pollUntil(t, p, func() bool { return !p.operationPending && !p.loadingDirectory })
	if p.selectionName() != "notes" || len(p.fileHistory) != 1 {
		t.Fatal("created folder was not selected/undoable", p.notice)
	}
	p.Send(1, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 160, Y: 105})
	if p.dialog == nil || p.dialog.operation.action != renameItem {
		t.Fatal("Rename button did not route to dialog")
	}
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 1, Pressed: true})
	if p.dialog != nil {
		t.Fatal("Escape did not cancel filename editing")
	}
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 44, Pressed: true, Modifiers: experience.ModControl})
	pollUntil(t, p, func() bool { return !p.operationPending && !p.loadingDirectory })
	if len(p.fileHistory) != 0 {
		t.Fatal("Undo did not consume the file operation")
	}
	if _, err := os.Stat(filepath.Join(root, "notes")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Undo left the created folder", err)
	}
	if _, err := os.Stat(filepath.Join(root, projectTrash)); err != nil {
		t.Fatal("Undo did not retain recovery data", err)
	}
}
