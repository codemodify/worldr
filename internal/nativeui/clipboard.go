package nativeui

import (
	"fmt"
	"github.com/codemodify/worldr/internal/experience"
	"unicode/utf8"
)

// FieldClipboard leases an editable field across asynchronous clipboard reads.
// Bind uses the field's focus-lifetime context. Any intervening edit invalidates
// delivery without releasing the old lease to a different field.
type FieldClipboard struct {
	field                                *Field
	context                              string
	copyText                             string
	copyReady, requested, pending, valid bool
	target                               *Field
	targetContext                        string
}

func (c *FieldClipboard) Bind(field *Field, context string) {
	if c.field != field || c.context != context {
		c.Invalidate()
	}
	c.field, c.context = field, context
}
func (c *FieldClipboard) Invalidate() { c.requested, c.valid = false, false }

// Handle consumes clipboard shortcuts and observes events which can change
// the target selection. Call it before dispatching those events to the field.
func (c *FieldClipboard) Handle(e experience.Event) bool {
	if c.field != nil && e.Kind == experience.KeyInput && e.Pressed && e.Modifiers == experience.ModControl {
		switch e.Keycode {
		case 46, 45:
			c.Invalidate()
			if !e.Repeat {
				text := c.field.Selection()
				if text != "" {
					c.copyText, c.copyReady = text, true
					if e.Keycode == 45 {
						c.field.Commit("")
					}
				}
			}
			return true
		case 47:
			if !e.Repeat && !c.pending {
				c.requested, c.valid = true, true
				c.target, c.targetContext = c.field, c.context
			}
			return true
		}
	}
	if e.Kind == experience.KeyInput && e.Pressed || e.Kind == experience.PointerDown || e.Kind == experience.TextCommit || e.Kind == experience.TextPreedit || e.Kind == experience.KeyboardCancel || e.Kind == experience.PointerCancel {
		c.Invalidate()
	}
	return false
}
func (c *FieldClipboard) TakeCopy() (string, bool) {
	text, ready := c.copyText, c.copyReady
	c.copyText, c.copyReady = "", false
	return text, ready
}
func (c *FieldClipboard) TakePasteRequest() bool {
	if c.pending || !c.requested || !c.valid || c.field == nil {
		return false
	}
	c.requested, c.pending = false, true
	return true
}
func (c *FieldClipboard) Paste(text string) error {
	if !c.pending {
		return nil
	}
	valid := c.valid && c.field != nil && c.field == c.target && c.context == c.targetContext
	c.pending, c.requested, c.valid = false, false, false
	c.target, c.targetContext = nil, ""
	if !valid || text == "" {
		return nil
	}
	if len(text) > 1<<20 || !utf8.ValidString(text) {
		return fmt.Errorf("clipboard text must be UTF-8 and at most 1 MiB")
	}
	c.field.Commit(text)
	return nil
}
