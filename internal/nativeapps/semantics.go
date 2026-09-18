package nativeapps

import "github.com/codemodify/worldr/internal/nativeui"

// Semantics routes a terminal's native controls using its published runtime ID.
// The host can adapt this snapshot to an accessibility service without exposing
// terminal backing cells or inventing one semantic node per pixel glyph.
func (m *Manager) Semantics(id uint64) nativeui.SemanticTree {
	if m.closed {
		return nativeui.SemanticTree{}
	}
	entry := m.find(id)
	if entry == nil {
		return nativeui.SemanticTree{}
	}
	return entry.provider.Semantics()
}
