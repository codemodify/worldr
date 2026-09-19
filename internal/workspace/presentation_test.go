package workspace

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/presentation"
)

func TestPresentationIsPermanentlyCinematicAndRoundTrips(t *testing.T) {
	w := study(t)
	command(t, w, Action{Kind: SetFocused, Enabled: true})
	if w.Document().View.Presentation != presentation.Cinematic || w.State().Presentation != presentation.Cinematic {
		t.Fatal("new studies must use the permanent cinematic presentation")
	}
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"presentation": "cinematic"`)) || bytes.Contains(data, []byte(`"reduced_motion"`)) {
		t.Fatalf("saved study did not use the canonical cinematic/full-motion state: %s", data)
	}
	loaded := study(t)
	if err := loaded.LoadState(data); err != nil {
		t.Fatal(err)
	}
	if loaded.Document() != w.Document() || loaded.State().Presentation != presentation.Cinematic || loaded.State().ReducedMotion {
		t.Fatal("saved cinematic study was not restored canonically", loaded.Document())
	}
	if loaded.CanUndo() || loaded.CanRedo() {
		t.Fatal("loading presentation state imported edit history")
	}
}

func TestLegacyPresentationDocumentsNormalizeToCinematic(t *testing.T) {
	fixtures := []struct {
		name string
		data []byte
	}{
		{
			name: "field absent",
			data: []byte(`{"version":1,"selection":"shaft","timeline":{"seconds":17.25,"playing":false},"view":{"exploded":true,"focused":true,"camera":{"yaw":0.3,"pitch":0.7}}}`),
		},
		{
			name: "adaptive and reduced motion",
			data: []byte(`{"version":1,"selection":"shaft","timeline":{"seconds":17.25,"playing":false},"view":{"exploded":true,"focused":true,"camera":{"yaw":0.3,"pitch":0.7},"presentation":"adaptive","reduced_motion":true}}`),
		},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			w := study(t)
			if err := w.LoadState(fixture.data); err != nil {
				t.Fatal(err)
			}
			d := w.Document()
			if d.View.Presentation != presentation.Cinematic || d.View.ReducedMotion || d.Selection != Shaft || d.Timeline.Seconds != 17.25 || !d.View.Exploded || !d.View.Focused || d.View.Camera != (CameraState{Yaw: 0.3, Pitch: 0.7}) {
				t.Fatal("legacy migration changed study data or retained a removed preference", d)
			}
			canonical, err := w.SaveState()
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(canonical, []byte(`"adaptive"`)) || bytes.Contains(canonical, []byte(`"reduced_motion"`)) {
				t.Fatalf("legacy preferences survived canonical save: %s", canonical)
			}
		})
	}
}

func TestInvalidPresentationIsTransactional(t *testing.T) {
	w := study(t)
	w.Draw(1440, 900)
	pointer(w, experience.PointerDown, 600, 400)
	pointer(w, experience.PointerMove, 670, 420)
	before, gesture := w.Document(), w.pointer
	history, position := append([]edit(nil), w.history...), w.historyPosition

	for _, invalid := range []presentation.Mode{"", "quiet", "Cinematic"} {
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
		bad := bytes.Replace(data, []byte(`"presentation": "cinematic"`), []byte(`"presentation": `+invalid), 1)
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
