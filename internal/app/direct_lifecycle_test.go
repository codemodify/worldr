package app

import (
	"errors"
	"reflect"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/platform/linux/seat"
)

func TestSeatDisableClosesResourcesBeforeACKAndResume(t *testing.T) {
	var calls []string
	action := func(name string) func() error { return func() error { calls = append(calls, name); return nil } }
	changed, err := applySeatTransitions([]seat.Event{seat.Disabled, seat.Enabled}, action("release graphics and input"), action("ack disable"), action("reopen"))
	if err != nil || !changed || !reflect.DeepEqual(calls, []string{"release graphics and input", "ack disable", "reopen"}) {
		t.Fatal("seat ownership order", calls, err)
	}
	blocked := errors.New("device lease remains")
	calls = nil
	_, err = applySeatTransitions([]seat.Event{seat.Disabled, seat.Enabled}, func() error { return blocked }, action("ack"), action("reopen"))
	if !errors.Is(err, blocked) || len(calls) != 0 {
		t.Fatal("disable acknowledged while ownership release failed", calls, err)
	}
	_, err = applySeatTransitions([]seat.Event{seat.Disabled, seat.Enabled}, action("release"), func() error { return blocked }, action("reopen"))
	if !errors.Is(err, blocked) || !reflect.DeepEqual(calls, []string{"release"}) {
		t.Fatal("resumed without ACK", calls, err)
	}
}
func TestOutputLayoutPreservesSelectionAcrossHotplugAndWraps(t *testing.T) {
	available := []native.DRMOutput{{ID: 9, Connected: true, Width: 3840, Height: 2160}, {ID: 3, Connected: true, Width: 3840, Height: 2160}, {ID: 20, Connected: true, Width: 3840, Height: 2160}, {ID: 8, Connected: false}}
	outputs, err := layoutDirectOutputs(available, nil, false)
	if err != nil || len(outputs) != 3 {
		t.Fatal(outputs, err)
	}
	if outputs[0].connector.ID != 3 || outputs[1].bounds.Min.X != 3840 || outputs[2].bounds.Min.Y != 2160 {
		t.Fatal("unstable or overflowing layout", outputs)
	}
	selected, err := layoutDirectOutputs(available, []uint32{20, 8, 3}, false)
	if err != nil || len(selected) != 2 || selected[0].connector.ID != 20 || selected[1].connector.ID != 3 {
		t.Fatal("disconnected selected output disrupted live monitors", selected, err)
	}
	available[3].Connected = true
	available[3].Width, available[3].Height = 1920, 1080
	replugged, err := layoutDirectOutputs(available, []uint32{20, 8, 3}, false)
	if err != nil || len(replugged) != 3 || replugged[1].connector.ID != 8 {
		t.Fatal("replug lost explicit order", replugged, err)
	}
	if _, err := layoutDirectOutputs(available, []uint32{20, 3}, true); err == nil {
		t.Fatal("diagnostic framebuffer accepted multiple outputs")
	}
}
func TestDirectVTSwitchNeedsExactUserChord(t *testing.T) {
	event := experience.Event{Kind: experience.KeyInput, Keycode: 59, Pressed: true, Modifiers: experience.ModControl | experience.ModAlt}
	if directVT(event) != 1 {
		t.Fatal("CtrlAltF1 not recognized")
	}
	event.Keycode = 88
	if directVT(event) != 12 {
		t.Fatal("CtrlAltF12 not recognized")
	}
	for _, mutate := range []func(*experience.Event){func(e *experience.Event) { e.Pressed = false }, func(e *experience.Event) { e.Repeat = true }, func(e *experience.Event) { e.Modifiers |= experience.ModShift }, func(e *experience.Event) { e.Modifiers = experience.ModAlt }} {
		e := event
		mutate(&e)
		if directVT(e) != 0 {
			t.Fatal("non-chord switched VT", e)
		}
	}
}
