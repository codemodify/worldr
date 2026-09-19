package workspace

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/render"
)

func hologramDraw(frame render.Frame, geometry *render.Geometry) (render.View, render.Draw, bool) {
	for _, command := range frame.Commands {
		for _, draw := range command.Draws {
			if draw.Geometry == geometry && draw.Material.Hologram > 0 {
				return command.View, draw, true
			}
		}
	}
	return render.View{}, render.Draw{}, false
}

func TestNativeHologramPhaseIsTransientAndAlwaysAdvances(t *testing.T) {
	w := study(t)
	command(t, w, Action{Kind: SetPlayback, Enabled: false})
	geometry := w.scene.Node(w.hologramNode).Mesh.Geometry()
	initialView, initial, ok := hologramDraw(w.Draw(1440, 900), geometry)
	if !ok || !initial.Translucent || initial.DepthReadOnly || initial.Material.Hologram <= 0 || initialView.EffectPhase != 0 {
		t.Fatal("AXIAL accent did not opt into the depth-composited hologram material")
	}
	before := w.Document()
	history := w.historyPosition
	w.Update(time.Second)
	movingView, moving, ok := hologramDraw(w.Draw(1440, 900), geometry)
	if !ok || movingView.EffectPhase != .25 || moving.Geometry != initial.Geometry || w.Document() != before || w.historyPosition != history {
		t.Fatal("hologram motion rebuilt resources or entered document/history state")
	}
	w.Update(2 * time.Second)
	advancedView, advanced, ok := hologramDraw(w.Draw(1440, 900), geometry)
	if !ok || w.hologramPhase != 3*time.Second || advancedView.EffectPhase != .75 || advanced.Model != moving.Model {
		t.Fatal("permanent full motion failed to advance the hologram scan pose")
	}
	w.Update(time.Second)
	loopedView, _, _ := hologramDraw(w.Draw(1440, 900), geometry)
	if w.hologramPhase != 0 || loopedView.EffectPhase != 0 {
		t.Fatal("hologram phase failed to wrap at its retained cycle")
	}
}

func TestHostedAxialPublishesNativeHologramWithoutChangingItsControlPlane(t *testing.T) {
	m, err := NewAxialApplicationManager()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err := m.LaunchApplication("axial"); err != nil {
		t.Fatal(err)
	}
	surfaces := m.Surfaces()
	if len(surfaces) != 1 || surfaces[0].Texture == nil || surfaces[0].Spatial == nil || len(surfaces[0].Spatial.Objects) != 6 {
		t.Fatal("hosted AXIAL surface contract changed")
	}
	accent := surfaces[0].Spatial.Objects[5].Node
	if accent.Mesh == nil || !accent.Translucent || !accent.Unpickable || accent.Material.Hologram <= 0 || accent.DepthReadOnly {
		t.Fatal("hosted AXIAL did not publish its holographic projection accent")
	}
}
