package workspace

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/presentation"
)

func TestPresentationPreferenceRoundTrip(t *testing.T) {
	w := study(t)
	if w.Document().View.Presentation != presentation.Cinematic {
		t.Fatal("new studies must use the selected cinematic default")
	}
	command(t, w, Action{Kind: SetPresentation, Presentation: presentation.Adaptive})
	command(t, w, Action{Kind: SetFocused, Enabled: true})
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	loaded := study(t)
	if err := loaded.LoadState(data); err != nil {
		t.Fatal(err)
	}
	if loaded.Document() != w.Document() || loaded.State().Presentation != presentation.Adaptive {
		t.Fatal("saved presentation preference was not restored", loaded.Document())
	}
	if loaded.CanUndo() || loaded.CanRedo() {
		t.Fatal("loading a preference must not import edit history")
	}
}

func TestOlderDocumentsDefaultToCinematic(t *testing.T) {
	// This is the original version 1 shape, without a presentation field.
	legacy := []byte(`{"version":1,"selection":"shaft","timeline":{"seconds":17.25,"playing":false},"view":{"exploded":true,"focused":true,"camera":{"yaw":0.3,"pitch":0.7}}}`)
	w := study(t)
	command(t, w, Action{Kind: SetPresentation, Presentation: presentation.Adaptive})
	if err := w.LoadState(legacy); err != nil {
		t.Fatal(err)
	}
	d := w.Document()
	if d.View.Presentation != presentation.Cinematic || d.Selection != Shaft || d.Timeline.Seconds != 17.25 || !d.View.Exploded || !d.View.Focused || d.View.Camera != (CameraState{Yaw: 0.3, Pitch: 0.7}) {
		t.Fatal("legacy migration changed study data", d)
	}
}

func TestInvalidPresentationIsTransactional(t *testing.T) {
	w := study(t)
	command(t, w, Action{Kind: TogglePresentation})
	w.Draw(1440, 900)
	pointer(w, experience.PointerDown, 600, 400)
	pointer(w, experience.PointerMove, 670, 420)
	before, gesture := w.Document(), w.pointer
	history, position := append([]edit(nil), w.history...), w.historyPosition
	for _, invalid := range []presentation.Mode{"", "quiet", "Cinematic"} {
		if err := w.Dispatch(Action{Kind: SetPresentation, Presentation: invalid}); err == nil {
			t.Fatalf("accepted invalid mode %q", invalid)
		}
		bad := before
		bad.View.Presentation = invalid
		data, err := json.Marshal(bad)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.LoadState(data); err == nil {
			t.Fatalf("accepted invalid saved mode %q", invalid)
		}
		if w.Document() != before || !reflect.DeepEqual(w.pointer, gesture) || !reflect.DeepEqual(w.history, history) || w.historyPosition != position {
			t.Fatal("invalid preference changed live state, gesture, or history")
		}
	}
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"null", "true", "42", "{}"} {
		bad := bytes.Replace(data, []byte(`"presentation": "adaptive"`), []byte(`"presentation": `+invalid), 1)
		if bytes.Equal(data, bad) {
			t.Fatal("test did not replace the saved presentation value")
		}
		if err := w.LoadState(bad); err == nil {
			t.Fatalf("accepted non-string saved mode %s", invalid)
		}
		if w.Document() != before || !reflect.DeepEqual(w.pointer, gesture) || !reflect.DeepEqual(w.history, history) || w.historyPosition != position {
			t.Fatal("invalid saved preference changed live state, gesture, or history")
		}
	}
}

func TestPresentationUndoPreservesPlaybackAndStudy(t *testing.T) {
	w := study(t)
	before := w.Document()
	command(t, w, Action{Kind: TogglePresentation})
	after := w.Document()
	if after.View.Presentation != presentation.Adaptive {
		t.Fatal("toggle did not select adaptive presentation")
	}
	after.View.Presentation = before.View.Presentation
	if after != before {
		t.Fatal("presentation changed study capabilities or content")
	}
	w.Update(2 * time.Second)
	live := w.Document()
	command(t, w, Action{Kind: Undo})
	live.View.Presentation = presentation.Cinematic
	if w.Document() != live {
		t.Fatal("undoing presentation rewound playback or changed study state")
	}
	command(t, w, Action{Kind: Redo})
	live.View.Presentation = presentation.Adaptive
	if w.Document() != live {
		t.Fatal("redoing presentation changed unrelated state")
	}
	command(t, w, Action{Kind: SetPresentation, Presentation: presentation.Adaptive})
	if len(w.history) != 1 {
		t.Fatal("reselecting the same presentation recorded an empty edit")
	}
}

func TestResetPreservesPresentationPreference(t *testing.T) {
	w := study(t)
	command(t, w, Action{Kind: SetPresentation, Presentation: presentation.Adaptive})
	command(t, w, Action{Kind: SeekTime, Seconds: 19})
	command(t, w, Action{Kind: SetExploded, Enabled: true})
	command(t, w, Action{Kind: SetFocused, Enabled: true})
	command(t, w, Action{Kind: ResetStudy})
	want := initialModel().document()
	want.View.Presentation = presentation.Adaptive
	if w.Document() != want {
		t.Fatal("reset did not restore study while preserving presentation", w.Document())
	}
	command(t, w, Action{Kind: Undo})
	if w.Document().View.Presentation != presentation.Adaptive || w.Document().Timeline.Seconds != 19 {
		t.Fatal("undo reset changed presentation or failed to restore study")
	}
}
