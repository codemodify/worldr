package workspace

import (
	"bytes"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/presentation"
)

func TestFullMotionInterpolatesWithoutChangingPlayback(t *testing.T) {
	w := study(t)
	w.Draw(1440, 900)
	before := w.Document()
	command(t, w, Action{Kind: SetExploded, Enabled: true})
	w.Update(30 * time.Millisecond)
	if w.State().Explosion <= 0 || w.State().Explosion >= 1 {
		t.Fatal("permanent full-motion explosion did not interpolate")
	}
	if w.State().Time <= before.Timeline.Seconds || !w.State().Playing {
		t.Fatal("visual interpolation interrupted explicit study playback")
	}
	if w.State().Presentation != presentation.Cinematic || w.State().ReducedMotion || w.Document().View.ReducedMotion {
		t.Fatal("runtime exposed a removed presentation or motion preference")
	}
	w.Update(5 * time.Second)
	if w.State().Explosion != 1 {
		t.Fatal("full-motion transition did not settle at its target")
	}
}

func TestLegacyReducedMotionStateLoadsAsFullMotion(t *testing.T) {
	w := study(t)
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	legacy := bytes.Replace(data, []byte(`"presentation": "cinematic",`), []byte("\"presentation\": \"cinematic\",\n    \"reduced_motion\": true,"), 1)
	if bytes.Equal(data, legacy) {
		t.Fatal("legacy fixture did not add the removed motion preference")
	}
	loaded := study(t)
	if err := loaded.LoadState(legacy); err != nil {
		t.Fatal(err)
	}
	if loaded.State().ReducedMotion || loaded.Document().View.ReducedMotion || loaded.State().Presentation != presentation.Cinematic {
		t.Fatal("legacy reduced-motion preference was not normalized")
	}
	command(t, loaded, Action{Kind: SetExploded, Enabled: true})
	loaded.Update(30 * time.Millisecond)
	if loaded.State().Explosion <= 0 || loaded.State().Explosion >= 1 {
		t.Fatal("legacy state disabled permanent full-motion interpolation")
	}
	canonical, err := loaded.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(canonical, []byte(`"reduced_motion"`)) {
		t.Fatalf("canonical state retained the legacy motion preference: %s", canonical)
	}
}
