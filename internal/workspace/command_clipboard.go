package workspace

import "fmt"

func (w *Workspace) bindCommandClipboard() {
	if w.commands == nil {
		return
	}
	p := w.commands
	if p.open {
		p.clipboard.Bind(p.field, fmt.Sprintf("commands:%d", p.epoch))
	} else {
		p.clipboard.Bind(nil, "")
	}
}
func (w *Workspace) TakeCopy() (string, bool) {
	if w.commands == nil {
		return "", false
	}
	return w.commands.clipboard.TakeCopy()
}
func (w *Workspace) TakePasteRequest() bool {
	w.bindCommandClipboard()
	return w.commands != nil && w.commands.clipboard.TakePasteRequest()
}
func (w *Workspace) Paste(text string) error {
	w.bindCommandClipboard()
	if w.commands == nil {
		return nil
	}
	err := w.commands.clipboard.Paste(text)
	w.commands.dirty = true
	w.refreshCommands()
	return err
}
