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

func TestNativeHologramPhaseIsTransientAndReducedMotionFreezesIt(t *testing.T) {
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
	command(t, w, Action{Kind: SetReducedMotion, Enabled: true})
	phase := w.hologramPhase
	w.Update(7 * time.Second)
	frozenView, frozen, ok := hologramDraw(w.Draw(1440, 900), geometry)
	if !ok || w.hologramPhase != phase || frozenView.EffectPhase != movingView.EffectPhase || frozen.Model != moving.Model {
		t.Fatal("Reduced Motion failed to freeze the current hologram scan pose")
	}
	command(t, w, Action{Kind: SetReducedMotion, Enabled: false})
	w.Update(time.Second)
	resumedView, _, _ := hologramDraw(w.Draw(1440, 900), geometry)
	if resumedView.EffectPhase != .5 {
		t.Fatal("hologram phase restarted or replayed paused time")
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
