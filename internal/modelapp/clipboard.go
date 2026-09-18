package modelapp

import "fmt"

func (v *viewer) bindClipboard() {
	if !v.closed && v.focused && v.editing {
		v.clipboard.Bind(v.note, fmt.Sprintf("model:%d:%d", v.id, v.textEpoch))
	} else {
		v.clipboard.Bind(nil, "")
	}
}
func (m *Manager) TakeCopy() (string, bool) {
	for _, v := range m.slots {
		if v != nil {
			if text, ok := v.clipboard.TakeCopy(); ok {
				return text, true
			}
		}
	}
	return "", false
}
func (m *Manager) TakePasteRequest() bool {
	if m.pasteTarget != nil {
		return false
	}
	for _, v := range m.slots {
		if v != nil {
			v.bindClipboard()
			if v.clipboard.TakePasteRequest() {
				m.pasteTarget = v
				return true
			}
		}
	}
	return false
}
func (m *Manager) Paste(text string) error {
	v := m.pasteTarget
	m.pasteTarget = nil
	if v == nil {
		return nil
	}
	v.bindClipboard()
	err := v.clipboard.Paste(text)
	v.dirty = true
	return err
}
