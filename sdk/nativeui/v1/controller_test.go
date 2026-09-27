package nativeui

import (
	"image"
	"testing"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
)

func TestControllerActivationFocusRangeCaptureAndSemantics(t *testing.T) {
	controls := []Control{
		{ID: "run", Kind: KindButton, Bounds: image.Rect(0, 0, 50, 24), Label: "Run"},
		{ID: "query", Kind: KindField, Bounds: image.Rect(60, 0, 150, 24), Label: "Query", Text: "orbit"},
		{ID: "live", Kind: KindSwitch, Bounds: image.Rect(0, 30, 100, 54), Label: "Live", State: State{Selected: true}},
		{ID: "gain", Kind: KindSlider, Bounds: image.Rect(0, 60, 200, 84), Label: "Gain", Min: 0, Max: 100, Value: 20, Step: 5, ValueText: "20 percent"},
		{ID: "row", Kind: KindMenuItem, Bounds: image.Rect(0, 90, 200, 114), Label: "Observation"},
	}
	var controller Controller
	if err := controller.SetControls(controls); err != nil {
		t.Fatal(err)
	}
	move := nativeapp.Event{Kind: nativeapp.PointerMove, X: 10, Y: 10}
	controller.Handle(move)
	if !controller.Decorate(controls[0]).State.Hovered {
		t.Fatal("pointer move did not decorate hover state")
	}
	down := move
	down.Kind = nativeapp.PointerDown
	down.Button = nativeapp.ButtonPrimary
	if action := controller.Handle(down); !action.Consumed || action.Activated || !action.ChangedFocus || controller.FocusedID() != "run" {
		t.Fatalf("button press = %+v", action)
	}
	up := down
	up.Kind = nativeapp.PointerUp
	if action := controller.Handle(up); !action.Activated || action.ID != "run" {
		t.Fatalf("button release = %+v", action)
	}

	painter, err := NewPainter(DefaultTheme())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = painter.Close(); _ = controller.Close() })
	track := painter.Layout(controls[3]).Track
	sliderDown := nativeapp.Event{Kind: nativeapp.PointerDown, Button: nativeapp.ButtonPrimary, X: float32(track.Min.X + track.Dx()/2), Y: 70}
	if action := controller.Handle(sliderDown); !action.Changed || action.Value < 45 || action.Value > 55 || action.ID != "gain" {
		t.Fatalf("slider press = %+v", action)
	}
	if action := controller.Handle(nativeapp.Event{Kind: nativeapp.PointerMove, X: 500, Y: 70}); !action.Changed || action.Value != 100 {
		t.Fatalf("captured slider move = %+v", action)
	}
	if action := controller.Handle(nativeapp.Event{Kind: nativeapp.PointerUp, Button: nativeapp.ButtonPrimary, X: -50, Y: 70}); action.Value != 0 || !action.Consumed {
		t.Fatalf("slider release = %+v", action)
	}

	if !controller.Focus("query") {
		t.Fatal("could not focus field")
	}
	if action := controller.Handle(nativeapp.Event{Kind: nativeapp.KeyInput, Key: "Enter", Pressed: true}); action.Activated || action.Consumed {
		t.Fatalf("Enter activated field: %+v", action)
	}
	if action := controller.Handle(nativeapp.Event{Kind: nativeapp.KeyInput, Key: "Tab", Keycode: 15, Pressed: true}); !action.ChangedFocus || action.ID != "live" {
		t.Fatalf("Tab = %+v", action)
	}
	controller.Focus("gain")
	if action := controller.Handle(nativeapp.Event{Kind: nativeapp.KeyInput, Key: "ArrowRight", Keycode: 106, Pressed: true}); !action.Changed || action.Value != 25 {
		t.Fatalf("slider key = %+v", action)
	}

	tree := controller.Semantics()
	if tree.FocusedID != "gain" || len(tree.Nodes) != len(controls) || tree.Nodes[1].Role != nativeapp.RoleTextField || tree.Nodes[2].Role != nativeapp.RoleButton || !tree.Nodes[2].Selected || tree.Nodes[3].Role != nativeapp.RoleSlider || tree.Nodes[3].Value != "20 percent" {
		t.Fatalf("semantics = %+v", tree)
	}
	var validator nativeapp.Validator
	if err := validator.Validate(nativeapp.Snapshot{
		Textures: []nativeapp.TextureUpdate{{ID: 1, Revision: 1, Width: 200, Height: 120, Rect: nativeapp.Rect{Width: 200, Height: 120}, Pixels: make([]byte, 200*120*4)}},
		Surfaces: []nativeapp.Surface{{ID: 1, Key: "gallery", Title: "Gallery", Texture: 1, Semantics: tree}},
	}); err != nil {
		t.Fatalf("controller emitted an invalid native-app v1 semantic tree: %v", err)
	}
}

func TestControllerSetControlsIsTransactionalAndCancelClearsCapture(t *testing.T) {
	var controller Controller
	valid := []Control{{ID: "a", Kind: KindButton, Bounds: image.Rect(0, 0, 20, 20), Label: "A"}}
	if err := controller.SetControls(valid); err != nil {
		t.Fatal(err)
	}
	invalid := []Control{{ID: "duplicate", Kind: KindButton, Bounds: image.Rect(0, 0, 20, 20)}, {ID: "duplicate", Kind: KindButton, Bounds: image.Rect(20, 0, 40, 20)}}
	if err := controller.SetControls(invalid); err == nil {
		t.Fatal("accepted duplicate IDs")
	}
	if got := controller.Controls(); len(got) != 1 || got[0].ID != "a" {
		t.Fatal("invalid update changed controls")
	}
	controller.Handle(nativeapp.Event{Kind: nativeapp.PointerDown, Button: nativeapp.ButtonPrimary, X: 4, Y: 4})
	if action := controller.Handle(nativeapp.Event{Kind: nativeapp.PointerCancel}); !action.Consumed {
		t.Fatal("cancel did not release capture")
	}
	if action := controller.Handle(nativeapp.Event{Kind: nativeapp.PointerUp, Button: nativeapp.ButtonPrimary, X: 4, Y: 4}); action.Activated {
		t.Fatal("release after cancel activated control")
	}
	if controller.Focus("missing") {
		t.Fatal("focused an unknown control")
	}
	if err := controller.SetControlsWithin(valid, image.Rect(0, 0, 10, 10)); err == nil {
		t.Fatal("accepted semantic control outside the live framebuffer")
	}
	if got := controller.Controls(); len(got) != 1 || got[0].ID != "a" {
		t.Fatal("out-of-surface update changed controls")
	}
	if err := controller.SetControlsWithin(valid, image.Rect(0, 0, 40, 40)); err != nil {
		t.Fatalf("rejected in-surface controls: %v", err)
	}
}
