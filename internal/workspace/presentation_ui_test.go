package workspace

import (
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/presentation"
)

func TestPresentationShortcutIsRemoved(t *testing.T) {
	w := study(t)
	before := w.Document()
	for _, modifiers := range []experience.Modifiers{0, experience.ModControl, experience.ModShift} {
		if key(w, experience.KeyP, modifiers) {
			t.Fatalf("P with modifiers %v retained a presentation control", modifiers)
		}
	}
	if w.Document() != before || w.State().Presentation != presentation.Cinematic {
		t.Fatal("removed presentation shortcuts changed the permanent cinematic state")
	}
}

func TestRemovedPresentationShortcutsReachFocusedApplication(t *testing.T) {
	w, apps := applicationStudy(t)
	x, y := visibleApplicationPoint(t, w)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if !w.OwnsKeyboard() {
		t.Fatal("fixture did not focus the application")
	}
	before := w.Document()
	for _, modifiers := range []experience.Modifiers{0, experience.ModShift} {
		for _, pressed := range []bool{true, false} {
			event := experience.Event{Kind: experience.KeyInput, Key: experience.KeyP, Keycode: 25, Pressed: pressed, Modifiers: modifiers}
			count := len(apps.events)
			if !w.Handle(event) || len(apps.events) != count+1 || apps.events[count].event != event {
				t.Fatalf("focused application did not receive removed presentation key: %+v", event)
			}
		}
	}
	if w.Document() != before {
		t.Fatal("application P input changed workspace presentation state")
	}
}

func TestFocusedStudyKeepsCinematicFraming(t *testing.T) {
	w := study(t)
	command(t, w, Action{Kind: SetPlayback, Enabled: false})
	command(t, w, Action{Kind: SetFocused, Enabled: true})
	frame := w.Draw(1440, 900)
	selected := w.scene.Node(w.nodes[w.m.selected])
	if selected.WireColor.A <= 0 || selected.WireWidth <= 0 || len(frame.Vertices) == 0 {
		t.Fatal("focused work lost the permanent cinematic guides")
	}
	if w.State().Presentation != presentation.Cinematic || w.State().ReducedMotion {
		t.Fatal("focus changed the permanent presentation or motion policy")
	}
}
