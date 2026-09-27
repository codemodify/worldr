package projectapp

import (
	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

var _ experience.ApplicationSkinSetter = (*Provider)(nil)

func (p *Provider) SetSkin(selected skin.Skin) {
	if p.closed || selected.Validate() != nil {
		return
	}
	if p.renderer.ui.SetSkin(selected) != nil || p.renderer.uiSmall.SetSkin(selected) != nil {
		return
	}
	p.dirty = true
}

func (p *Provider) SetControlTheme(theme experience.ControlTheme) {
	selected, err := nativeui.LegacySkin(theme.Family, theme.Shape)
	if err == nil {
		p.SetSkin(selected)
	}
}
