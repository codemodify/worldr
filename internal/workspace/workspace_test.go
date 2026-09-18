package workspace

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

func study(t *testing.T) *Workspace {
	t.Helper()
	w, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}
func command(t *testing.T, w *Workspace, a Action) {
	t.Helper()
	if err := w.Dispatch(a); err != nil {
		t.Fatal(err)
	}
}
func pointer(w *Workspace, kind experience.EventKind, x, y float32) bool {
	return w.Handle(experience.Event{Kind: kind, X: x, Y: y, Button: experience.ButtonPrimary})
}
func key(w *Workspace, k experience.Key, mods experience.Modifiers) bool {
	return w.Handle(experience.Event{Kind: experience.KeyInput, Key: k, Modifiers: mods, Pressed: true})
}

func TestPlaybackPauseScrubAndLoop(t *testing.T) {
	w := study(t)
	w.Update(1500 * time.Millisecond)
	if w.Document().Timeline.Seconds != 7.5 {
		t.Fatal(w.Document().Timeline)
	}
	command(t, w, Action{Kind: SeekTime, Seconds: 12})
	w.Update(time.Second)
	if d := w.Document(); d.Timeline.Playing || d.Timeline.Seconds != 12 {
		t.Fatal(d.Timeline)
	}
	command(t, w, Action{Kind: SeekTime, Seconds: 100})
	if w.Document().Timeline.Seconds != duration {
		t.Fatal("scrub did not clamp")
	}
	command(t, w, Action{Kind: SetPlayback, Enabled: true})
	w.Update(250 * time.Millisecond)
	if math.Abs(w.Document().Timeline.Seconds-.25) > 1e-8 {
		t.Fatal("playback did not wrap")
	}
}

func TestExplosionProgressesWhileTransportPaused(t *testing.T) {
	w := study(t)
	command(t, w, Action{Kind: SetPlayback})
	command(t, w, Action{Kind: SetExploded, Enabled: true})
	w.Update(100 * time.Millisecond)
	if w.State().Explosion <= 0 || w.State().Explosion >= 1 {
		t.Fatal("explosion must interpolate")
	}
	w.Update(2 * time.Second)
	if w.State().Explosion != 1 || w.Document().Timeline.Seconds != 6 {
		t.Fatal(w.State())
	}
	command(t, w, Action{Kind: SetExploded})
	w.Update(2 * time.Second)
	if w.State().Explosion != 0 {
		t.Fatal("assembly did not close")
	}
}

func TestTimelineAndOrbitInputRemainSeparate(t *testing.T) {
	w := study(t)
	w.Draw(1440, 900)
	pointer(w, experience.PointerDown, 844.5, 824)
	pointer(w, experience.PointerUp, 844.5, 824)
	if math.Abs(w.State().Time-15) > 1e-6 || w.State().Playing {
		t.Fatal(w.State())
	}
	before := w.Document()
	pointer(w, experience.PointerDown, 600, 400)
	pointer(w, experience.PointerMove, 680, 425)
	pointer(w, experience.PointerUp, 680, 425)
	after := w.Document()
	if after.View.Camera == before.View.Camera {
		t.Fatal("drag did not orbit")
	}
	if after.Timeline != before.Timeline || after.Selection != before.Selection {
		t.Fatal("orbit changed timeline or selection")
	}
	w.Draw(720, 450)
	pointer(w, experience.PointerDown, 690, 412)
	pointer(w, experience.PointerUp, 690, 412)
	if w.Document().Timeline.Seconds < 29 {
		t.Fatal("scaled timeline missed")
	}
}

func TestMeshClickSelectsPickedComponent(t *testing.T) {
	w := study(t)
	w.Draw(1440, 900)
	for y := float32(350); y < 580; y += 30 {
		for x := float32(430); x < 960; x += 30 {
			hit, ok := w.scene.Pick(w.camera, w.viewport, x, y)
			if !ok {
				continue
			}
			for i, id := range w.nodes {
				if id == hit.Node {
					command(t, w, Action{Kind: SelectComponent, Component: componentIDs[(i+1)%3]})
					pointer(w, experience.PointerDown, x, y)
					pointer(w, experience.PointerUp, x, y)
					if w.Document().Selection != componentIDs[i] {
						t.Fatal("mesh pick did not select its stable component ID")
					}
					return
				}
			}
		}
	}
	t.Fatal("no assembly geometry was pickable in the visible viewport")
}

func TestKeyboardBindingsRespectModifiersAndRepeat(t *testing.T) {
	w := study(t)
	key(w, experience.Key3, 0)
	key(w, experience.KeyE, 0)
	key(w, experience.KeyF, 0)
	key(w, experience.KeySpace, 0)
	if s := w.State(); s.Selected != 2 || !s.Exploded || !s.Focused || s.Playing {
		t.Fatal(s)
	}
	if !key(w, experience.KeyEscape, 0) || w.Document().View.Focused {
		t.Fatal("escape did not reset view")
	}
	before := w.Document()
	if key(w, experience.KeyR, experience.ModControl) || w.Document() != before {
		t.Fatal("Ctrl+R must not invoke the unmodified reset binding")
	}
	if w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyE, Pressed: true, Repeat: true}) {
		t.Fatal("held toggle key must not repeat")
	}
	key(w, experience.KeyR, 0)
	if w.Document() != initialModel().document() {
		t.Fatal("reset did not restore document defaults")
	}
}

func TestTimeControlsDriveGeometryAndResponse(t *testing.T) {
	w := study(t)
	key(w, experience.KeySpace, 0)
	w.Draw(1440, 900)
	rotation := w.scene.Node(w.nodes[1]).Transform
	response := signal(w.m.selected, w.m.clock)
	w.Update(time.Second)
	w.Draw(1440, 900)
	if w.scene.Node(w.nodes[1]).Transform != rotation || signal(w.m.selected, w.m.clock) != response {
		t.Fatal("paused geometry and inspector must retain the same time")
	}
	key(w, experience.KeyRight, 0)
	w.Draw(1440, 900)
	if w.scene.Node(w.nodes[1]).Transform == rotation || signal(w.m.selected, w.m.clock) == response {
		t.Fatal("time step must update both geometry and response")
	}
	response = signal(w.m.selected, w.m.clock)
	key(w, experience.Key1, 0)
	if signal(w.m.selected, w.m.clock) == response {
		t.Fatal("selection did not change response channel")
	}
}

func TestUndoDoesNotRewindUnrelatedPlayback(t *testing.T) {
	w := study(t)
	command(t, w, Action{Kind: SelectComponent, Component: Housing})
	w.Update(2 * time.Second)
	clock := w.Document().Timeline
	key(w, experience.KeyZ, experience.ModControl)
	if d := w.Document(); d.Selection != Rotor || d.Timeline != clock {
		t.Fatal("selection undo rewound playback", d)
	}
	key(w, experience.KeyZ, experience.ModControl|experience.ModShift)
	if w.Document().Selection != Housing || w.Document().Timeline != clock {
		t.Fatal("redo did not preserve unrelated state")
	}
	command(t, w, Action{Kind: Undo})
	command(t, w, Action{Kind: ToggleExplode})
	if w.CanRedo() {
		t.Fatal("new edit did not discard redo branch")
	}
}

func TestOrbitGestureIsOneEditAndCancellationRollsBack(t *testing.T) {
	w := study(t)
	before := w.Document()
	pointer(w, experience.PointerDown, 600, 400)
	for i := 1; i <= 12; i++ {
		pointer(w, experience.PointerMove, float32(600+5*i), float32(400+i))
		w.Update(10 * time.Millisecond)
	}
	pointer(w, experience.PointerUp, 660, 412)
	if len(w.history) != 1 {
		t.Fatalf("one gesture produced %d edits", len(w.history))
	}
	clock := w.Document().Timeline
	command(t, w, Action{Kind: Undo})
	if w.Document().View.Camera != before.View.Camera || w.Document().Timeline != clock {
		t.Fatal("orbit undo did not isolate the camera edit")
	}
	command(t, w, Action{Kind: Redo})
	before = w.Document()
	pointer(w, experience.PointerDown, 600, 400)
	pointer(w, experience.PointerMove, 900, 500)
	w.Handle(experience.Event{Kind: experience.PointerCancel})
	if w.Document() != before || len(w.history) != 1 {
		t.Fatal("cancelled orbit changed document or history")
	}
}

func TestPointerCancelDoesNotClickAndRestoresTimeline(t *testing.T) {
	w := study(t)
	before := w.Document()
	pointer(w, experience.PointerDown, 80, 620)
	w.Handle(experience.Event{Kind: experience.PointerCancel})
	pointer(w, experience.PointerUp, 80, 620)
	if w.Document() != before || w.CanUndo() {
		t.Fatal("focus loss synthesized a click")
	}
	pointer(w, experience.PointerDown, 800, 824)
	pointer(w, experience.PointerMove, 1100, 824)
	if w.Document().Timeline.Playing {
		t.Fatal("scrubbing did not pause")
	}
	w.Handle(experience.Event{Kind: experience.PointerCancel})
	if w.Document() != before || w.CanUndo() {
		t.Fatal("cancelled scrub did not restore transport")
	}
}

func TestDocumentRoundTripAndTransactionalValidation(t *testing.T) {
	w := study(t)
	command(t, w, Action{Kind: SelectComponent, Component: Shaft})
	command(t, w, Action{Kind: SetExploded, Enabled: true})
	command(t, w, Action{Kind: SeekTime, Seconds: 17.25})
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	loaded := study(t)
	if err := loaded.LoadState(data); err != nil {
		t.Fatal(err)
	}
	if loaded.Document() != w.Document() || loaded.State().Explosion != 1 || loaded.CanUndo() {
		t.Fatal("document round trip leaked transient presentation/history state")
	}
	before := w.Document()
	history := w.historyPosition
	badDocs := []Document{before, before, before, before}
	badDocs[0].Version = 99
	badDocs[1].Selection = "missing"
	badDocs[2].Timeline.Seconds = 100
	badDocs[3].View.Camera.Pitch = 2
	for _, bad := range badDocs {
		encoded, _ := json.Marshal(bad)
		if err := w.LoadState(encoded); err == nil {
			t.Fatalf("accepted invalid document: %+v", bad)
		}
		if w.Document() != before || w.historyPosition != history {
			t.Fatal("invalid load changed live state")
		}
	}
	for _, bad := range [][]byte{append(append([]byte{}, data...), []byte(" {}")...), []byte(`{"version":1,"unknown":true}`)} {
		if err := w.LoadState(bad); err == nil {
			t.Fatal("accepted trailing/unknown JSON")
		}
	}
}

func TestInvalidActionsAreTransactionalAndHistoryIsBounded(t *testing.T) {
	w := study(t)
	before := w.Document()
	for _, action := range []Action{{Kind: SelectComponent, Component: "missing"}, {Kind: SeekTime, Seconds: math.NaN()}, {Kind: OrbitCamera, DeltaX: float32(math.Inf(1))}, {Kind: "unknown"}} {
		if err := w.Dispatch(action); err == nil {
			t.Fatal("accepted invalid command", action)
		}
		if w.Document() != before || w.CanUndo() {
			t.Fatal("invalid command changed document/history")
		}
	}
	for i := 0; i < historyLimit+10; i++ {
		command(t, w, Action{Kind: ToggleExplode})
	}
	if len(w.history) != historyLimit {
		t.Fatal("history is not bounded")
	}
	for i := 0; i < historyLimit; i++ {
		command(t, w, Action{Kind: Undo})
	}
	if w.CanUndo() {
		t.Fatal("history cursor exceeded retained edits")
	}
}

func TestDemonstrationUsesSemanticActions(t *testing.T) {
	w := study(t)
	w.Demo(4 * time.Second)
	if !w.Document().View.Exploded || w.Document().Selection != Housing || len(w.history) != 2 {
		t.Fatal("demo did not execute the same undoable semantic actions")
	}
}
