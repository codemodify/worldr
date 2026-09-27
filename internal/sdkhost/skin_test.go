package sdkhost

import (
	"github.com/codemodify/worldr/internal/experience"
	"testing"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func TestSkinRequestRequiresItsOwnCapabilityAndOwnsQueuedData(t *testing.T) {
	selected, err := skin.Builtin("merrick")
	if err != nil {
		t.Fatal(err)
	}
	for _, legacy := range []bool{false, true} {
		p := bareProvider()
		p.requests = make(chan queuedRequest, 2)
		p.manifest.ControlThemes = legacy
		p.SetSkin(selected)
		if len(p.requests) != 0 {
			t.Fatal("skin sent without the separate capability")
		}
	}
	p := bareProvider()
	p.requests = make(chan queuedRequest, 2)
	p.manifest.Skins = true
	original := selected.Palette["accent"]
	p.SetSkin(selected)
	selected.Palette["accent"] = "#123456"
	request := (<-p.requests).request
	if request.Kind != nativeapp.RequestSkin || request.Skin == nil || request.Skin.Palette["accent"] != original {
		t.Fatal("queued skin borrowed caller data")
	}
	selected.Version = 999
	p.SetSkin(selected)
	if len(p.requests) != 0 {
		t.Fatal("invalid skin queued")
	}
}

func TestSkinOnlyClientReceivesLegacyPreferenceAsFallbackSkin(t *testing.T) {
	p := bareProvider()
	p.manifest.Skins = true
	p.requests = make(chan queuedRequest, 2)
	p.SetControlTheme(experience.ControlTheme{Family: "glass", Shape: "slab"})
	if len(p.requests) != 1 {
		t.Fatal("skin-only client did not receive exactly one supported fallback request")
	}
	request := (<-p.requests).request
	if request.Kind != nativeapp.RequestSkin || request.Skin == nil || request.Skin.ID != "glass" {
		t.Fatal("wrong legacy fallback", request)
	}
	for _, layer := range request.Skin.Controls["button"].Layers {
		if layer.Geometry.Kind != "rect" {
			t.Fatal("legacy independent silhouette lost")
		}
	}
}
