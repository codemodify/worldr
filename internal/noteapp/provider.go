package noteapp

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/resourcepath"
	"github.com/codemodify/worldr/internal/textinput"
)

type viewer struct {
	id                                uint64
	state                             SessionState
	editor                            *editor
	renderer                          *renderer
	input                             *textinput.Translator
	controls                          nativeui.Controller
	saveResult                        chan saveResult
	message                           string
	textEpoch                         uint64
	focused, selecting, dirty, saving bool
	closeArmed, closed                bool
	copyText                          string
	copyReady                         bool
}

type Manager struct {
	slots                        [MaxNotes]*viewer
	retiring                     []*viewer
	pasteTarget                  *viewer
	pasteContext                 uint64
	next                         uint64
	surfaces                     []experience.ApplicationSurface
	retired                      []uint64
	keymap, modifiers            experience.Event
	pasteRequested, pastePending bool
	pasteValid                   bool
	closed                       bool
}

var _ experience.Applications = (*Manager)(nil)
var _ experience.ApplicationPoller = (*Manager)(nil)
var _ experience.ApplicationProviderCloser = (*Manager)(nil)
var _ experience.ApplicationCloser = (*Manager)(nil)
var _ experience.ApplicationTextureRetirer = (*Manager)(nil)
var _ experience.ApplicationLauncher = (*Manager)(nil)
var _ experience.ApplicationLaunchCatalog = (*Manager)(nil)
var _ experience.ApplicationTextInput = (*Manager)(nil)

func NewManager() *Manager { return &Manager{} }

func (m *Manager) count() int {
	count := 0
	for _, view := range m.slots {
		if view != nil {
			count++
		}
	}
	return count
}

func (m *Manager) NextKey() (string, error) {
	m.collectRetiring(false)
	if m.closed {
		return "", fmt.Errorf("native notes are closed")
	}
	if m.count()+len(m.retiring) >= MaxNotes {
		return "", fmt.Errorf("native note limit reached (%d)", MaxNotes)
	}
	for slot, view := range m.slots {
		if view == nil {
			return slotKey(slot), nil
		}
	}
	return "", fmt.Errorf("native note slots are exhausted")
}

func (m *Manager) ApplicationLaunches() []experience.ApplicationLaunch {
	return []experience.ApplicationLaunch{{Kind: "note", Title: "New native note"}}
}

func (m *Manager) LaunchApplication(kind string) (string, error) {
	if kind != "note" {
		return "", fmt.Errorf("unsupported native note launch %q", kind)
	}
	key, err := m.NextKey()
	if err != nil {
		return "", err
	}
	state := SessionState{Key: key, Text: "", Caret: 0, Anchor: 0}
	return m.openState(state, "")
}

// OpenFile takes ownership of file only after it has accepted and installed
// the document. Callers retain ownership on an error.
func (m *Manager) OpenFile(file *os.File, name string) (string, error) {
	key, err := m.NextKey()
	if err != nil {
		return "", err
	}
	path, text, hash, err := readDocument(file)
	if err != nil {
		return "", err
	}
	state := SessionState{Key: key, Source: path, Text: text, DiskHash: hash, Caret: 0, Anchor: 0}
	key, err = m.openState(state, "")
	if err != nil {
		return "", err
	}
	_ = file.Close()
	return key, nil
}

func (m *Manager) Restore(state SessionState) (string, error) {
	if err := state.Validate(); err != nil {
		return "", err
	}
	message := ""
	if state.Source != "" {
		file, err := resourcepath.OpenFile(state.Source)
		if err != nil {
			return "", err
		}
		_, _, currentHash, err := readDocument(file)
		_ = file.Close()
		if err != nil {
			return "", err
		}
		if !strings.EqualFold(currentHash, state.DiskHash) {
			message = "Disk copy changed since this workspace checkpoint · saved edits preserved"
		}
	}
	return m.openState(state, message)
}

func (m *Manager) openState(state SessionState, message string) (string, error) {
	if m.closed {
		return "", fmt.Errorf("native notes are closed")
	}
	if err := state.Validate(); err != nil {
		return "", err
	}
	slot, err := keySlot(state.Key)
	if err != nil {
		return "", err
	}
	if m.slots[slot] != nil {
		return "", fmt.Errorf("native note slot is already open")
	}
	r, err := newRenderer(noteBaseWidth, noteBaseHeight)
	if err != nil {
		return "", err
	}
	input, err := textinput.New()
	if err != nil {
		r.close()
		return "", err
	}
	if m.keymap.Kind == experience.KeymapChanged {
		input.Handle(m.keymap)
	}
	if m.modifiers.Kind == experience.KeyboardModifiers {
		input.Handle(m.modifiers)
	}
	m.next++
	view := &viewer{id: m.next, state: state, editor: newEditor(state.Text, state.Caret, state.Anchor), renderer: r, input: input, dirty: true, message: message}
	view.updateControls()
	m.slots[slot] = view
	if err := view.refresh(); err != nil {
		m.slots[slot] = nil
		input.Close()
		r.close()
		return "", err
	}
	return state.Key, nil
}

func (v *viewer) syncState() SessionState {
	v.state.Text = v.editor.text
	v.state.Caret, v.state.Anchor = v.editor.caret, v.editor.anchor
	return v.state
}

func (v *viewer) updateControls() {
	buttons := []struct{ id, label string }{{"save", "SAVE"}, {"undo", "UNDO"}, {"redo", "REDO"}}
	nodes := make([]nativeui.Node, 0, len(buttons)+1)
	for index, button := range buttons {
		nodes = append(nodes, nativeui.Node{ID: button.id, Role: nativeui.RoleButton, Label: button.label, Bounds: v.renderer.bounds(642+index*104, 14, 94, 32), Disabled: button.id == "save" && (!v.state.Dirty || v.state.Source == "" || v.saving)})
	}
	value := v.editor.text
	if len(value) > 512 {
		end := 512
		for end > 0 && !utf8.RuneStart(value[end]) {
			end--
		}
		value = value[:end]
	}
	nodes = append(nodes, nativeui.Node{ID: "document", Role: nativeui.RoleTextField, Label: "Note document", Value: value, Description: fmt.Sprintf("%d bytes, %d lines", len(v.editor.text), v.editor.LineCount()), Bounds: v.renderer.editorRect()})
	v.controls.SetNodes(nodes)
}

func (v *viewer) refresh() error {
	v.syncState()
	v.updateControls()
	v.dirty = false
	return v.renderer.draw(v)
}

func (m *Manager) Poll() error {
	m.collectRetiring(false)
	for _, view := range m.slots {
		if view == nil {
			continue
		}
		if view.saving {
			select {
			case result := <-view.saveResult:
				view.saving = false
				if result.err != nil {
					view.message = "Save failed: " + result.err.Error()
				} else {
					view.state.DiskHash = result.hash
					view.state.Dirty = view.editor.revision != result.revision
					if view.state.Dirty {
						view.message = "Saved snapshot · newer edits remain modified"
					} else {
						view.message = "Saved " + filepath.Base(view.state.Source)
					}
				}
				view.dirty = true
			default:
			}
		}
		if view.dirty {
			if err := view.refresh(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) Surfaces() []experience.ApplicationSurface {
	m.surfaces = m.surfaces[:0]
	for _, view := range m.slots {
		if view == nil {
			continue
		}
		title := "Untitled note"
		if view.state.Source != "" {
			title = filepath.Base(view.state.Source)
		}
		if view.state.Dirty {
			title = "● " + title
		}
		m.surfaces = append(m.surfaces, experience.ApplicationSurface{ID: view.id, Key: view.state.Key, AppID: "worldr.native-note", Title: title, Texture: view.renderer.texture, FrameStyle: experience.FrameCinematic})
	}
	return m.surfaces
}

func (m *Manager) Focus(id uint64) {
	for _, view := range m.slots {
		if view == nil {
			continue
		}
		focused := view.id == id
		if view.focused != focused {
			view.focused, view.dirty = focused, true
			view.textEpoch++
		}
		if focused {
			view.controls.Focus("document")
		} else {
			view.selecting = false
			view.controls.Blur()
			view.editor.CancelComposition()
			view.input.Handle(experience.Event{Kind: experience.KeyboardCancel})
			if m.pasteTarget == view {
				m.pasteValid = false
			}
		}
	}
}

func (m *Manager) Seat(event experience.Event) {
	switch event.Kind {
	case experience.KeymapChanged:
		m.keymap = event
	case experience.KeyboardModifiers:
		m.modifiers = event
	case experience.KeyboardCancel:
		m.modifiers = experience.Event{}
	default:
		return
	}
	for _, view := range m.slots {
		if view != nil {
			view.input.Handle(event)
		}
	}
}

func (m *Manager) Send(id uint64, event experience.Event) {
	for _, view := range m.slots {
		if view == nil || view.id != id {
			continue
		}
		if event.Kind == experience.TextCommit || event.Kind == experience.TextPreedit {
			state := m.TextInput(id)
			if !state.Enabled || state.ContextID != event.TextContext {
				return
			}
		}
		view.handle(m, event)
		return
	}
}

func (m *Manager) Resize(id uint64, width, height int) {
	for _, view := range m.slots {
		if view == nil || view.id != id {
			continue
		}
		if err := view.renderer.resize(width, height); err != nil {
			view.message = err.Error()
		} else {
			view.ensureCaretVisible()
		}
		view.dirty = true
		if err := view.refresh(); err != nil {
			view.message = err.Error()
		}
		return
	}
}

func (v *viewer) ensureCaretVisible() {
	line, column := v.editor.lineColumn(v.editor.caret)
	visibleLines, visibleColumns := v.renderer.visibleLines(), v.renderer.visibleColumns()
	if line < v.state.TopLine {
		v.state.TopLine = line
	} else if line >= v.state.TopLine+visibleLines {
		v.state.TopLine = line - visibleLines + 1
	}
	if column < v.state.Left {
		v.state.Left = column
	} else if column >= v.state.Left+visibleColumns {
		v.state.Left = column - visibleColumns + 1
	}
	v.state.TopLine = max(0, min(max(0, v.editor.LineCount()-visibleLines), v.state.TopLine))
	v.state.Left = max(0, v.state.Left)
}

func (v *viewer) changed(truncated bool) {
	v.state.Dirty = true
	v.closeArmed = false
	v.message = ""
	if truncated {
		v.message = fmt.Sprintf("Document reached the %d KiB limit", MaxDocumentBytes>>10)
	}
	v.ensureCaretVisible()
	v.dirty = true
}

func (v *viewer) save() {
	if v.saving {
		v.message, v.dirty = "A save is already in progress", true
		return
	}
	if v.state.Source == "" {
		v.message, v.dirty = "Untitled notes live in the workspace session · open a .worldr-note.md file to save on disk", true
		return
	}
	if !v.state.Dirty {
		v.message, v.dirty = "Document is already saved", true
		return
	}
	snapshot := saveSnapshot{path: v.state.Source, text: v.editor.text, expected: v.state.DiskHash, revision: v.editor.revision}
	v.saving = true
	v.saveResult = make(chan saveResult, 1)
	v.message, v.dirty = "Saving atomic document…", true
	go func() {
		hash, err := writeDocument(snapshot)
		v.saveResult <- saveResult{revision: snapshot.revision, hash: hash, err: err}
	}()
}

func (v *viewer) clipboardKey(manager *Manager, event experience.Event) bool {
	if event.Kind != experience.KeyInput || !event.Pressed || event.Modifiers != experience.ModControl {
		return false
	}
	switch event.Keycode {
	case 46, 45: // C / X
		manager.invalidatePaste()
		if !event.Repeat {
			selection := v.editor.Selection()
			if selection != "" {
				v.copyText, v.copyReady = selection, true
				if event.Keycode == 45 {
					changed, _ := v.editor.Insert("")
					if changed {
						v.changed(false)
					}
				}
			}
		}
		return true
	case 47: // V
		if !event.Repeat && !manager.pastePending {
			manager.pasteRequested, manager.pasteValid = true, true
			manager.pasteTarget, manager.pasteContext = v, v.textEpoch
		}
		return true
	}
	return false
}

func (v *viewer) handle(manager *Manager, event experience.Event) {
	committed := v.input.Handle(event)
	if v.clipboardKey(manager, event) {
		v.dirty = true
		return
	}
	if event.Kind == experience.KeyboardCancel || event.Kind == experience.PointerCancel {
		v.selecting = false
		v.editor.CancelComposition()
		manager.invalidatePaste()
		v.dirty = true
		return
	}
	if event.Kind == experience.KeyInput && event.Pressed && v.focused && event.Modifiers == experience.ModControl {
		switch event.Keycode {
		case 31: // S
			v.save()
			return
		case 44: // Z
			manager.invalidatePaste()
			if v.editor.Undo() {
				v.changed(false)
			}
			return
		case 21: // Y
			manager.invalidatePaste()
			if v.editor.Redo() {
				v.changed(false)
			}
			return
		}
	}
	if event.Kind == experience.KeyInput && event.Pressed && v.focused && (event.Modifiers == 0 || event.Modifiers == experience.ModShift) && (event.Keycode == 104 || event.Keycode == 109) {
		direction := v.renderer.visibleLines() - 1
		if event.Keycode == 104 {
			direction = -direction
		}
		v.editor.moveVertical(direction, event.Modifiers.Has(experience.ModShift))
		manager.invalidatePaste()
		v.ensureCaretVisible()
		v.dirty = true
		return
	}
	if event.Kind == experience.PointerDown && event.Button == experience.ButtonPrimary && image.Pt(int(event.X), int(event.Y)).In(v.renderer.editorRect()) {
		line, column := v.renderer.pointPosition(v, event.X, event.Y)
		v.editor.SetCaretAt(line, column, event.Modifiers.Has(experience.ModShift))
		v.selecting = true
		v.controls.Focus("document")
		manager.invalidatePaste()
		v.ensureCaretVisible()
		v.dirty = true
		return
	}
	if event.Kind == experience.PointerMove && v.selecting {
		line, column := v.renderer.pointPosition(v, event.X, event.Y)
		v.editor.SetCaretAt(line, column, true)
		manager.invalidatePaste()
		v.ensureCaretVisible()
		v.dirty = true
		return
	}
	if event.Kind == experience.PointerUp && v.selecting {
		v.selecting = false
		v.dirty = true
		return
	}
	if event.Kind == experience.PointerScroll && image.Pt(int(event.X), int(event.Y)).In(v.renderer.editorRect()) {
		delta := 3
		if event.ScrollY < 0 {
			delta = -3
		}
		v.state.TopLine = max(0, min(max(0, v.editor.LineCount()-v.renderer.visibleLines()), v.state.TopLine+delta))
		v.dirty = true
		return
	}
	if event.SpatialObject == 0 {
		action := v.controls.Handle(event)
		if action.Consumed {
			if action.Activated {
				switch action.ID {
				case "save":
					v.save()
				case "undo":
					if v.editor.Undo() {
						v.changed(false)
					}
				case "redo":
					if v.editor.Redo() {
						v.changed(false)
					}
				}
			}
			v.dirty = true
			return
		}
	}
	if !v.focused {
		return
	}
	beforeCaret, beforeAnchor := v.editor.caret, v.editor.anchor
	changed, truncated := v.editor.Handle(event, committed)
	if changed {
		manager.invalidatePaste()
		v.changed(truncated)
	} else if event.Kind == experience.TextPreedit || beforeCaret != v.editor.caret || beforeAnchor != v.editor.anchor {
		manager.invalidatePaste()
		v.ensureCaretVisible()
		v.dirty = true
	}
}

func (m *Manager) Semantics(id uint64) nativeui.SemanticTree {
	for _, view := range m.slots {
		if view != nil && view.id == id {
			view.updateControls()
			return view.controls.Semantics()
		}
	}
	return nativeui.SemanticTree{}
}

func (m *Manager) TextInput(id uint64) experience.TextInputState {
	for _, view := range m.slots {
		if view != nil && view.id == id && view.focused {
			rect := view.renderer.caretRect(view)
			if rect.Empty() {
				rect = image.Rect(view.renderer.textRect().Min.X, view.renderer.textRect().Min.Y, view.renderer.textRect().Min.X+1, view.renderer.textRect().Min.Y+view.renderer.lineHeight())
			}
			return view.editor.TextInput(fmt.Sprintf("note:%d:%d", view.id, view.textEpoch), rect)
		}
	}
	return experience.TextInputState{}
}

func (m *Manager) TakeCopy() (string, bool) {
	for _, view := range m.slots {
		if view != nil && view.copyReady {
			text := view.copyText
			view.copyText, view.copyReady = "", false
			return text, true
		}
	}
	return "", false
}

func (m *Manager) invalidatePaste() {
	m.pasteRequested, m.pasteValid = false, false
	if !m.pastePending {
		m.pasteTarget = nil
	}
}

func (m *Manager) TakePasteRequest() bool {
	if m.pastePending || !m.pasteRequested || !m.pasteValid || m.pasteTarget == nil {
		return false
	}
	m.pasteRequested, m.pastePending = false, true
	return true
}

func (m *Manager) Paste(text string) error {
	if !m.pastePending {
		return nil
	}
	target, context := m.pasteTarget, m.pasteContext
	valid := m.pasteValid && target != nil && !target.closed && target.focused && target.textEpoch == context
	m.pastePending, m.pasteRequested, m.pasteValid = false, false, false
	m.pasteTarget = nil
	if !valid || text == "" {
		return nil
	}
	if err := validateClipboardText(text); err != nil {
		target.message, target.dirty = err.Error(), true
		return err
	}
	changed, truncated := target.editor.Insert(text)
	if changed {
		target.changed(truncated)
	}
	return nil
}

func (m *Manager) closeViewer(slot int) {
	view := m.slots[slot]
	if view == nil {
		return
	}
	if m.pasteTarget == view {
		m.invalidatePaste()
	}
	if view.saving {
		m.retiring = append(m.retiring, view)
	}
	view.closed = true
	view.input.Close()
	m.retired = append(m.retired, view.renderer.texture.ID())
	view.renderer.close()
	m.slots[slot] = nil
}

func (m *Manager) CloseApplication(id uint64) {
	for slot, view := range m.slots {
		if view == nil || view.id != id {
			continue
		}
		if view.saving {
			view.message = "Save in progress · close when the atomic write finishes"
			view.dirty = true
			return
		}
		if view.state.Dirty && !view.closeArmed {
			view.closeArmed = true
			view.message = "Unsaved edits · save now or close again to discard"
			view.dirty = true
			return
		}
		m.closeViewer(slot)
		return
	}
}

func (m *Manager) collectRetiring(wait bool) {
	remaining := m.retiring[:0]
	for _, view := range m.retiring {
		if view.saving {
			if wait {
				<-view.saveResult
				view.saving = false
			} else {
				select {
				case <-view.saveResult:
					view.saving = false
				default:
				}
			}
		}
		if view.saving {
			remaining = append(remaining, view)
		}
	}
	clear(m.retiring[len(remaining):])
	m.retiring = remaining
}

func (m *Manager) Close() error {
	if m.closed {
		return nil
	}
	m.closed = true
	for slot := range m.slots {
		m.closeViewer(slot)
	}
	m.collectRetiring(true)
	return nil
}

func (m *Manager) RetiredTextures() []uint64 {
	ids := m.retired
	m.retired = nil
	return ids
}

func (m *Manager) SessionStates() []SessionState {
	states := make([]SessionState, 0, m.count())
	for _, view := range m.slots {
		if view == nil {
			continue
		}
		state := view.syncState()
		if state.Validate() == nil {
			states = append(states, state)
		}
	}
	return states
}
