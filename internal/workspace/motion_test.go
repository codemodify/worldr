package workspace

import (
	"bytes"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/presentation"
)

func TestReducedMotionCompletesTransitionsWithoutChangingPlayback(t *testing.T) {
	w := study(t)
	w.Draw(1440, 900)
	command(t, w, Action{Kind: SetExploded, Enabled: true})
	w.Update(30 * time.Millisecond)
	if w.State().Explosion <= 0 || w.State().Explosion >= 1 {
		t.Fatal("full-motion explosion did not interpolate")
	}
	command(t, w, Action{Kind: SetPresentation, Presentation: presentation.Adaptive})
	command(t, w, Action{Kind: SetFocused, Enabled: true})
	w.Update(30 * time.Millisecond)
	before := w.Document()
	command(t, w, Action{Kind: SetReducedMotion, Enabled: true})
	w.Draw(1440, 900) // Immediate even before the next positive-duration tick.
	if w.State().Explosion != 1 || w.presentationBlend != 0 {
		t.Fatal("enabling reduced motion left an unfinished transition")
	}
	want := before
	want.View.ReducedMotion = true
	if w.Document() != want {
		t.Fatal("motion preference changed content, playback or placement")
	}
	w.Update(time.Second)
	if w.State().Time != before.Timeline.Seconds+1 || !w.State().Playing {
		t.Fatal("reduced motion interrupted explicit study playback")
	}
	command(t, w, Action{Kind: SetExploded, Enabled: false})
	command(t, w, Action{Kind: SetPresentation, Presentation: presentation.Cinematic})
	w.Draw(1440, 900)
	if w.State().Explosion != 0 || w.presentationBlend != 1 {
		t.Fatal("reduced motion did not immediately resolve later transitions")
	}
	command(t, w, Action{Kind: SetReducedMotion, Enabled: false})
	command(t, w, Action{Kind: SetExploded, Enabled: true})
	w.Update(30 * time.Millisecond)
	if w.State().Explosion <= 0 || w.State().Explosion >= 1 {
		t.Fatal("disabling reduced motion did not restore interpolation")
	}
}

func TestReducedMotionPersistenceUndoAndReset(t *testing.T) {
	w := study(t)
	command(t, w, Action{Kind: ToggleReducedMotion})
	w.Update(time.Second)
	live := w.Document()
	command(t, w, Action{Kind: Undo})
	want := live
	want.View.ReducedMotion = false
	if w.Document() != want {
		t.Fatal("undo rewound playback or failed to restore motion preference")
	}
	command(t, w, Action{Kind: Redo})
	if w.Document() != live {
		t.Fatal("redo changed unrelated state")
	}
	command(t, w, Action{Kind: SetExploded, Enabled: true})
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	loaded := study(t)
	if err := loaded.LoadState(data); err != nil {
		t.Fatal(err)
	}
	loaded.Draw(1440, 900)
	if loaded.Document() != w.Document() || loaded.State().Explosion != 1 {
		t.Fatal("loaded motion preference or final geometry was lost")
	}
	command(t, loaded, Action{Kind: ResetStudy})
	if !loaded.State().ReducedMotion || loaded.State().Explosion != 0 {
		t.Fatal("study reset discarded the preference or animated its reset")
	}
	legacy := bytes.Replace(data, []byte(`"reduced_motion": true,`), nil, 1)
	if bytes.Equal(data, legacy) {
		t.Fatal("missing legacy fixture replacement")
	}
	if err := loaded.LoadState(legacy); err != nil || loaded.State().ReducedMotion {
		t.Fatal("older documents did not retain full motion by default", err)
	}
}

func TestReducedMotionHeaderAndKeyboard(t *testing.T) {
	w := study(t)
	w.Draw(2880, 1800)
	x, y := (motionButton.x+20)*2, (motionButton.y+10)*2
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if !w.State().ReducedMotion {
		t.Fatal("scaled motion control missed")
	}
	if !key(w, experience.KeyP, experience.ModShift) || w.State().ReducedMotion {
		t.Fatal("Shift+P did not restore full motion")
	}
	if w.State().Presentation != presentation.Cinematic {
		t.Fatal("motion shortcut also changed cinematic mode")
	}
	if w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyP, Modifiers: experience.ModShift, Pressed: true, Repeat: true}) {
		t.Fatal("held shortcut repeatedly changed motion preference")
	}
	pointer(w, experience.PointerDown, x, y)
	w.Handle(experience.Event{Kind: experience.PointerCancel})
	pointer(w, experience.PointerUp, x, y)
	if w.State().ReducedMotion {
		t.Fatal("cancelled control click changed motion preference")
	}
}
