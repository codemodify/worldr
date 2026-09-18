package projectapp

import (
	"fmt"
	"image"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
)

type fileAction uint8

const (
	createFolder fileAction = iota + 1
	renameItem
	moveItem
	duplicateItem
	trashItem
	restoreTrashItem
	undoFileAction
)

const projectTrash = ".worldr-trash"
const maxFileUndo = 16
const maxDuplicateBytes = 128 << 20

type fileOperation struct {
	action         fileAction
	source, target string
	expected       fileStamp
	undo           *fileUndo
}

type fileUndo struct {
	source, target string
	expected       fileStamp
	created        bool
	directory      bool
}

type fileDialog struct {
	operation fileOperation
	title     string
	field     *nativeui.Field
}

var operationButtons = [...]struct {
	label  string
	rect   image.Rectangle
	action fileAction
}{
	{"New folder", image.Rect(14, 94, 128, 121), createFolder},
	{"Rename", image.Rect(136, 94, 224, 121), renameItem},
	{"Move", image.Rect(232, 94, 308, 121), moveItem},
	{"Duplicate", image.Rect(316, 94, 418, 121), duplicateItem},
	{"Trash", image.Rect(426, 94, 504, 121), trashItem},
	{"Undo file", image.Rect(512, 94, 626, 121), undoFileAction},
}

func operationDialogRect(width, height int) image.Rectangle {
	return image.Rect(32, max(24, (height-190)/2), width-32, max(24, (height-190)/2)+190)
}

func (p *Provider) beginOperation(action fileAction) {
	if p.operationPending || p.loadingDirectory {
		return
	}
	p.searchActive = false
	p.dirty = true
	p.notice = ""
	if action == undoFileAction {
		if len(p.fileHistory) == 0 {
			p.notice = "No file operation to undo."
			return
		}
		undo := p.fileHistory[len(p.fileHistory)-1]
		p.startOperation(fileOperation{action: action, undo: &undo})
		return
	}
	limit := 255
	if action == moveItem {
		limit = 4096
	}
	dialog := &fileDialog{operation: fileOperation{action: action}, field: nativeui.NewField(limit)}
	value := ""
	if action == createFolder {
		dialog.title = "New folder name"
	} else {
		if p.selected < 0 || p.selected >= len(p.entries) {
			p.notice = "Select a file or folder first."
			return
		}
		item := p.entries[p.selected]
		if item.kind != fileEntry && item.kind != directoryEntry {
			p.notice = "File operations require a regular file or folder; symbolic links are not followed."
			return
		}
		dialog.operation.source, dialog.operation.expected = p.selectedPath(), item.stamp
		switch action {
		case renameItem:
			dialog.title, value = "Rename to", safeLabel(item.name)
		case moveItem:
			dialog.title, value = "Move to folder relative to project root (empty = root)", p.directory
		case duplicateItem:
			if item.kind != fileEntry {
				p.notice = "Duplicate supports regular files up to 128 MiB. Folder copying is not available."
				return
			}
			dialog.title = "Duplicate file as"
			extension := filepath.Ext(item.name)
			value = safeLabel(strings.TrimSuffix(item.name, extension) + " copy" + extension)
		case trashItem:
			dialog.title, value = "Move to project Trash?", safeLabel(item.name)
		case restoreTrashItem:
			if !restorableTrashItem(dialog.operation.source) {
				p.notice = "Select an item directly inside a Trash entry's items folder."
				return
			}
			dialog.title, value = "Restore to its recorded project folder?", safeLabel(item.name)
		}
	}
	dialog.field.Set(value)
	dialog.field.SelectAll()
	p.dialog = dialog
	p.text.Handle(experience.Event{Kind: experience.KeyboardCancel})
}

func validNewName(name string) bool {
	return name != "" && len(name) <= 255 && utf8.ValidString(name) && !strings.ContainsRune(name, 0) && filepath.Base(name) == name && name != "." && name != ".." && !filepath.IsAbs(name)
}

func cleanRelative(path string) bool {
	return !filepath.IsAbs(path) && !strings.ContainsRune(path, 0) && (path == "" || filepath.Clean(path) == path && path != "." && path != ".." && !strings.HasPrefix(path, ".."+string(filepath.Separator)))
}

func (p *Provider) confirmOperation() {
	if p.dialog == nil {
		return
	}
	op := p.dialog.operation
	value := p.dialog.field.Text()
	if op.action == moveItem {
		if !cleanRelative(value) || len(value) > 4096 || !utf8.ValidString(value) {
			p.notice, p.dirty = "Use a clean folder path inside the project, without .. or an absolute path.", true
			return
		}
		op.target = filepath.Join(value, filepath.Base(op.source))
	} else if op.action != trashItem && op.action != restoreTrashItem {
		if !validNewName(value) {
			p.notice, p.dirty = "Use a single name of 1–255 UTF-8 bytes; /, . and .. are not allowed.", true
			return
		}
		op.target = filepath.Join(p.directory, value)
	}
	p.dialog = nil
	p.startOperation(op)
}

func (p *Provider) startOperation(op fileOperation) {
	if p.loadingFile {
		p.previewPath = ""
	}
	p.loadingFile = false
	p.notice = "Working on files…"
	p.submit(request{path: p.directory, operation: &op})
}

func (p *Provider) operationResult(res *result) {
	p.operationPending = false
	if res.err != nil {
		p.notice = "File operation failed: " + safeLabel(res.err.Error())
	} else {
		if res.operation.action == undoFileAction {
			if len(p.fileHistory) > 0 {
				p.fileHistory = p.fileHistory[:len(p.fileHistory)-1]
			}
		} else if res.undo != nil {
			if len(p.fileHistory) == maxFileUndo {
				copy(p.fileHistory, p.fileHistory[1:])
				p.fileHistory = p.fileHistory[:maxFileUndo-1]
			}
			p.fileHistory = append(p.fileHistory, *res.undo)
		}
		p.notice = res.notice
	}
	selected := p.selectionName()
	if res.err == nil && res.selected != "" {
		selected = res.selected
	}
	p.refreshSelection(selected)
}

func (p *Provider) operationKey(event experience.Event) bool {
	if event.Repeat {
		return false
	}
	action := fileAction(0)
	switch {
	case event.Keycode == 49 && event.Modifiers == experience.ModControl|experience.ModShift:
		action = createFolder
	case event.Keycode == 60 && event.Modifiers == 0:
		action = renameItem
	case event.Keycode == 50 && event.Modifiers == experience.ModControl:
		action = moveItem
	case event.Keycode == 32 && event.Modifiers == experience.ModControl:
		action = duplicateItem
	case event.Keycode == 111 && event.Modifiers == 0:
		action = trashItem
	case event.Keycode == 44 && event.Modifiers == experience.ModControl:
		action = undoFileAction
	case event.Keycode == 19 && event.Modifiers == experience.ModControl|experience.ModShift:
		action = restoreTrashItem
	}
	if action != 0 {
		p.beginOperation(action)
		return true
	}
	return false
}

func (p *Provider) dialogKey(event experience.Event) {
	d := p.dialog
	text := p.text.Handle(event)
	p.dirty = true
	if event.Keycode == 1 {
		p.dialog, p.notice = nil, ""
		return
	}
	if event.Keycode == 28 || event.Keycode == 96 {
		if !event.Repeat {
			p.confirmOperation()
		}
		return
	}
	if d.operation.action == trashItem || d.operation.action == restoreTrashItem {
		return
	}
	d.field.Handle(event, text)
}

func (p *Provider) dialogClick(x, y int) {
	rect := operationDialogRect(p.renderer.image.Rect.Dx(), p.renderer.image.Rect.Dy())
	if image.Pt(x, y).In(image.Rect(rect.Min.X+18, rect.Max.Y-46, rect.Min.X+130, rect.Max.Y-16)) {
		p.confirmOperation()
	} else if image.Pt(x, y).In(image.Rect(rect.Min.X+140, rect.Max.Y-46, rect.Min.X+250, rect.Max.Y-16)) {
		p.dialog, p.notice, p.dirty = nil, "", true
	} else if image.Pt(x, y).In(image.Rect(rect.Min.X+18, rect.Min.Y+54, rect.Max.X-18, rect.Min.Y+88)) {
		p.dirty = true
		if p.dialog.operation.action != trashItem && p.dialog.operation.action != restoreTrashItem {
			field := image.Rect(rect.Min.X+18, rect.Min.Y+54, rect.Max.X-18, rect.Min.Y+88)
			if err := p.renderer.ui.PlaceCaret(p.dialog.field, field, image.Pt(x, y), false); err != nil {
				p.err = err
			}
		}
	}
}

func operationName(action fileAction) string {
	switch action {
	case createFolder:
		return "Created folder"
	case renameItem:
		return "Renamed"
	case moveItem:
		return "Moved"
	case duplicateItem:
		return "Duplicated"
	case trashItem:
		return "Moved to project Trash"
	case restoreTrashItem:
		return "Restored from project Trash"
	default:
		return "Undid file operation"
	}
}

func restorableTrashItem(path string) bool {
	parts := strings.Split(path, string(filepath.Separator))
	return len(parts) == 4 && parts[0] == projectTrash && parts[2] == "items"
}

func (p *Provider) operationButton(action fileAction) (fileAction, string) {
	if action == trashItem && restorableTrashItem(p.selectedPath()) {
		return restoreTrashItem, "Restore"
	}
	return action, ""
}

func operationNotice(action fileAction, target string) string {
	return fmt.Sprintf("%s: %s. Ctrl+Z undoes file operations in Files.", operationName(action), safeLabel(target))
}
