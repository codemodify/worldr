package app

import (
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

type skinnedHubProvider struct {
	themedHubProvider
	skins []skin.Skin
}

func (p *skinnedHubProvider) SetSkin(selected skin.Skin) { p.skins = append(p.skins, selected) }

func TestHubRetainsSkinForLateProvidersAndIsolatesOwnedData(t *testing.T) {
	selected, err := skin.Builtin("merrick")
	if err != nil {
		t.Fatal(err)
	}
	first, sibling := &skinnedHubProvider{}, &skinnedHubProvider{}
	hub := newApplicationHub(first, &hubProvider{}, sibling)
	legacy := experience.ControlTheme{Family: "glass", Shape: "slab"}
	hub.SetControlTheme(legacy)
	original := selected.Palette["accent"]
	hub.SetSkin(selected)
	selected.Palette["accent"] = "#123456"
	first.skins[0].Palette["accent"] = "#654321"
	late := &skinnedHubProvider{}
	hub.AddProvider(late)
	if len(late.skins) != 1 || late.skins[0].Palette["accent"] != original || sibling.skins[0].Palette["accent"] != original {
		t.Fatal("skin data shared across caller/providers or late provider missed preference")
	}
	if len(late.themes) != 1 || late.themes[0] != legacy {
		t.Fatal("late legacy provider missed theme")
	}
	selected.Version = 999
	hub.SetSkin(selected)
	if len(late.skins) != 1 {
		t.Fatal("invalid skin broadcast")
	}
	if err := hub.Close(); err != nil {
		t.Fatal(err)
	}
	selected.Version = skin.Version
	hub.SetSkin(selected)
	hub.AddProvider(&skinnedHubProvider{})
	if len(late.skins) != 1 || len(hub.providers) != 4 {
		t.Fatal("closed hub accepted skin or provider")
	}
}
