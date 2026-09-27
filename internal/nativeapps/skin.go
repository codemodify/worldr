package nativeapps

import (
	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

var _ experience.ApplicationSkinSetter = (*Manager)(nil)
var _ experience.ApplicationSkinSetter = (*Provider)(nil)

func (m *Manager) SetSkin(selected skin.Skin) {
	if m.closed || selected.Validate() != nil {
		return
	}
	owned := selected.Clone()
	m.skin = &owned
	for _, entry := range m.slots {
		if entry != nil {
			entry.provider.SetSkin(owned)
		}
	}
}

func (p *Provider) SetSkin(selected skin.Skin) {
	if p.closed || p.dismissed || selected.Validate() != nil {
		return
	}
	if p.renderer.ui.SetSkin(selected) == nil {
		// The floating panel scene opts into alpha both here and on its nodes.
		p.renderer.panelField = selected.Desktop.Backdrop == "panel-field"
		p.renderer.initialized = false
	}
}

func (p *Manager) SetControlTheme(theme experience.ControlTheme) {
	selected, err := nativeui.LegacySkin(theme.Family, theme.Shape)
	if err == nil {
		p.SetSkin(selected)
	}
}

func (p *Provider) SetControlTheme(theme experience.ControlTheme) {
	selected, err := nativeui.LegacySkin(theme.Family, theme.Shape)
	if err == nil {
		p.SetSkin(selected)
	}
}
