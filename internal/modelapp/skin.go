package modelapp

import (
	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

var _ experience.ApplicationSkinSetter = (*Manager)(nil)

// SetSkin updates current controls and the preference inherited by new windows.
func (m *Manager) SetSkin(selected skin.Skin) {
	if m.closed || selected.Validate() != nil {
		return
	}
	owned := selected.Clone()
	m.skin = &owned
	for _, view := range m.slots {
		if view != nil && view.renderer.painter.SetSkin(owned) == nil {
			view.dirty = true
		}
	}
}

func (p *Manager) SetControlTheme(theme experience.ControlTheme) {
	selected, err := nativeui.LegacySkin(theme.Family, theme.Shape)
	if err == nil {
		p.SetSkin(selected)
	}
}
