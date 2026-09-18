package projectapp

import (
	"fmt"
	"image"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
)

func (p *Provider) activeField() (*nativeui.Field, image.Rectangle) {
	if p.closed || !p.focused || p.operationPending {
		return nil, image.Rectangle{}
	}
	if p.dialog != nil {
		if p.dialog.operation.action == trashItem || p.dialog.operation.action == restoreTrashItem {
			return nil, image.Rectangle{}
		}
		rect := operationDialogRect(p.renderer.image.Rect.Dx(), p.renderer.image.Rect.Dy())
		return p.dialog.field, image.Rect(rect.Min.X+18, rect.Min.Y+54, rect.Max.X-18, rect.Min.Y+88)
	}
	if p.searchActive {
		return p.searchField, searchFieldRect(p.renderer.image.Rect.Dx())
	}
	return nil, image.Rectangle{}
}

func (p *Provider) syncFieldContext() {
	field, _ := p.activeField()
	if field == p.contextField {
		return
	}
	if p.contextField != nil {
		p.contextField.CancelComposition()
	}
	p.contextField = field
	p.fieldContext++
	p.cancelFieldPaste()
	p.text.Handle(experience.Event{Kind: experience.KeyboardCancel})
}

// TextInput describes only an explicitly active search or filename field.
// Navigation and read-only file previews never become text-input destinations.
func (p *Provider) TextInput(id uint64) experience.TextInputState {
	if id != 1 || p.closed {
		return experience.TextInputState{}
	}
	p.syncFieldContext()
	field, rect := p.activeField()
	if field == nil {
		return experience.TextInputState{}
	}
	return field.TextInput(fmt.Sprintf("files:%d", p.fieldContext), rect)
}

func (p *Provider) fieldTextEvent(event experience.Event) {
	p.syncFieldContext()
	if event.TextContext != "" && event.TextContext != fmt.Sprintf("files:%d", p.fieldContext) {
		return
	}
	field, _ := p.activeField()
	if field == nil {
		return
	}
	p.text.Handle(event) // Discard any physical-key compose sequence superseded by IME.
	p.cancelFieldPaste()
	if field.Handle(event, "") && field == p.searchField {
		p.setQuery(field.Text())
	}
	p.dirty = true
}

func (p *Provider) cancelFieldPaste() {
	p.pasteRequested, p.pasteValid = false, false
	// Keep an in-flight transfer leased until its completion. A second request
	// cannot make a delayed first transfer target a newly opened field.
	if !p.pastePending {
		p.pasteField = nil
	}
}

func (p *Provider) fieldClipboardKey(event experience.Event) bool {
	field, _ := p.activeField()
	if field == nil || event.Modifiers != experience.ModControl {
		return false
	}
	switch event.Keycode {
	case 46, 45: // Copy / Cut selection, leaving Ctrl+Shift+C as Copy path.
		p.cancelFieldPaste()
		if !event.Repeat {
			text := field.Selection()
			if text != "" {
				p.copyText, p.copyReady = text, true
			}
			if event.Keycode == 45 && text != "" {
				if field.Commit("") && field == p.searchField {
					p.setQuery(field.Text())
				}
				p.dirty = true
			}
		}
		return true
	case 47:
		if !event.Repeat && !p.pastePending {
			p.syncFieldContext()
			p.pasteRequested, p.pasteValid = true, true
			p.pasteField, p.pasteContext = field, p.fieldContext
		}
		return true
	}
	return false
}

// TakePasteRequest leases the original field until Paste completes. New edits,
// field changes, closing, and losing keyboard focus invalidate that lease.
func (p *Provider) TakePasteRequest() bool {
	p.syncFieldContext()
	if !p.pasteRequested || p.pastePending || !p.pasteValid {
		return false
	}
	p.pasteRequested, p.pastePending = false, true
	return true
}

func (p *Provider) Paste(text string) error {
	if !p.pastePending {
		return nil
	}
	p.syncFieldContext()
	valid := p.pasteValid
	field, context := p.pasteField, p.pasteContext
	p.pastePending, p.pasteValid, p.pasteRequested = false, false, false
	p.pasteField = nil
	active, _ := p.activeField()
	if !valid || active == nil || field != active || context != p.fieldContext {
		return nil
	}
	if len(text) > 1<<20 || !utf8.ValidString(text) {
		p.notice, p.dirty = "Clipboard text must be valid UTF-8 and no larger than 1 MiB.", true
		return fmt.Errorf("invalid filename clipboard text")
	}
	if field.Commit(text) && field == p.searchField {
		p.setQuery(field.Text())
	}
	p.dirty = true
	return nil
}
