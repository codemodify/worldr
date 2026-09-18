package nativeapps

import (
	"fmt"
	"github.com/codemodify/worldr/internal/experience"
)

func (p *Provider) TextInput(id uint64) experience.TextInputState {
	if p.closed || !p.focused || id != 1 || p.tools.mode != "find" || p.snapshot.AlternateScreen {
		return experience.TextInputState{}
	}
	return p.tools.field.TextInput(fmt.Sprintf("terminal-find/%d", p.tools.epoch), findFieldRect(p.renderer.image.Rect.Dx()))
}
func (m *Manager) TextInput(id uint64) experience.TextInputState {
	if m.closed || id != m.focused {
		return experience.TextInputState{}
	}
	entry := m.find(id)
	if entry == nil {
		return experience.TextInputState{}
	}
	return entry.provider.TextInput(1)
}
