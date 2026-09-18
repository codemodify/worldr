package workspace

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/presentation"
)

func TestPresentationSelectorAndKeyboardAtScaledSize(t *testing.T) {
	w := study(t)
	w.Draw(2880, 1800)
	x, y := (adaptiveButton.x+30)*2, (adaptiveButton.y+15)*2
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if w.State().Presentation != presentation.Adaptive {
		t.Fatal("scaled presentation selector missed")
	}
	if !key(w, experience.KeyP, 0) || w.State().Presentation != presentation.Cinematic {
		t.Fatal("P did not switch presentation")
	}
	if key(w, experience.KeyP, experience.ModControl) || w.State().Presentation != presentation.Cinematic {
		t.Fatal("modified P changed presentation")
	}
	if w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyP, Pressed: true, Repeat: true}) {
		t.Fatal("held P repeatedly toggled presentation")
	}
}

func TestAdaptiveFocusChangesFramingWithoutChangingStudy(t *testing.T) {
	w := study(t)
	command(t, w, Action{Kind: SetPlayback, Enabled: false})
	command(t, w, Action{Kind: SetFocused, Enabled: true})
	w.Draw(1440, 900)
	before := w.Document()
	camera := w.camera
	transform := w.scene.Node(w.nodes[1]).Transform
	strongWire := w.scene.Node(w.nodes[1]).WireColor.A

	command(t, w, Action{Kind: SetPresentation, Presentation: presentation.Adaptive})
	w.Update(30 * time.Millisecond)
	if w.presentationBlend <= 0 || w.presentationBlend >= 1 {
		t.Fatal("adaptive focus should fade the frame, not abruptly switch it")
	}
	w.Update(time.Second)
	quietVertices := len(w.Draw(1440, 900).Vertices)
	if w.presentationBlend != 0 || w.scene.Node(w.nodes[1]).WireColor.A >= strongWire {
		t.Fatal("focused adaptive mode did not quiet guides and accents")
	}
	if w.camera != camera || w.scene.Node(w.nodes[1]).Transform != transform || w.Document().Timeline != before.Timeline || w.Document().Selection != before.Selection {
		t.Fatal("presentation changed camera, geometry, time or selection")
	}
	command(t, w, Action{Kind: SetPresentation, Presentation: presentation.Cinematic})
	w.Update(time.Second)
	if w.presentationBlend != 1 || len(w.Draw(1440, 900).Vertices) <= quietVertices {
		t.Fatal("cinematic focus did not restore spatial framing")
	}
	command(t, w, Action{Kind: SetPresentation, Presentation: presentation.Adaptive})
	command(t, w, Action{Kind: SetFocused, Enabled: false})
	w.Update(time.Second)
	if w.presentationBlend != 1 {
		t.Fatal("adaptive exploration should retain cinematic framing")
	}
}

func TestLoadedAdaptiveFocusHasNoCinematicFlash(t *testing.T) {
	w := study(t)
	w.Draw(1440, 900)
	command(t, w, Action{Kind: SetPresentation, Presentation: presentation.Adaptive})
	command(t, w, Action{Kind: SetFocused, Enabled: true})
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	loaded := study(t)
	loaded.Draw(1440, 900)
	if err := loaded.LoadState(data); err != nil {
		t.Fatal(err)
	}
	loaded.Draw(1440, 900)
	if loaded.presentationBlend != 0 {
		t.Fatal("loading focused adaptive document flashed cinematic decorations")
	}
}
