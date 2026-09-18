package app

import (
	"io"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/input"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/workspace"
)

func TestDirectQueuePreservesEdgesModifiersCoordinatesAndTimes(t *testing.T) {
	var state keyState
	raw := []input.Event{
		{Kind: input.KeyInput, Code: 42, Pressed: true, Time: 1},
		{Kind: input.Move, X: 21, Y: 40, Time: 2},
		{Kind: input.Down, Code: 0x110, X: 21, Y: 40, Time: 3},
		{Kind: input.Up, Code: 0x110, X: 21, Y: 40, Time: 4},
		{Kind: input.KeyInput, Code: 42, Time: 5},
		{Kind: input.Down, Code: 0x111, X: 27, Y: 49, Time: 6},
		{Kind: input.Move, X: 70, Y: 89, Time: 7},
		{Kind: input.Scroll, X: 70, Y: 89, ScrollX: 2.5, ScrollY: -10, Time: 8},
		{Kind: input.Up, Code: 0x111, X: 70, Y: 89, Time: 9},
		{Kind: input.Down, Code: 0x113, X: 72, Y: 92, Time: 10},
		{Kind: input.Up, Code: 0x113, X: 72, Y: 92, Time: 11},
		{Kind: input.KeyInput, Code: 767, Pressed: true, Time: 12},
		{Kind: input.KeyInput, Code: 767, Time: 13},
	}
	got := state.directEvents(nil, raw)
	wantKinds := []experience.EventKind{
		experience.KeyInput, experience.PointerMove, experience.PointerDown, experience.PointerUp,
		experience.KeyInput, experience.PointerDown, experience.PointerMove, experience.PointerScroll,
		experience.PointerUp, experience.PointerDown, experience.PointerUp, experience.KeyInput, experience.KeyInput,
	}
	if len(got) != len(raw) {
		t.Fatalf("queue collapsed edges: %d events, want %d", len(got), len(raw))
	}
	for i, event := range got {
		if event.Kind != wantKinds[i] || event.Time != raw[i].Time || event.Repeat {
			t.Fatalf("event %d lost its edge/time: %+v", i, event)
		}
		if raw[i].Kind != input.KeyInput && (event.X != float32(raw[i].X) || event.Y != float32(raw[i].Y)) {
			t.Fatalf("event %d used poll-end coordinates: %+v", i, event)
		}
		mods, depressed := experience.Modifiers(0), uint32(0)
		if i < 4 {
			mods, depressed = experience.ModShift, 1
		}
		if event.Modifiers != mods || event.Depressed != depressed {
			t.Fatalf("event %d used poll-end modifiers: %+v", i, event)
		}
	}
	if got[2].Button != experience.ButtonPrimary || got[5].Button != experience.ButtonSecondary || got[9].Button != experience.ButtonNone || got[9].ButtonCode != 0x113 || got[10].ButtonCode != 0x113 {
		t.Fatal("direct input lost button identity")
	}
	if got[7].ScrollX != 2.5 || got[7].ScrollY != -10 || got[11].Keycode != 767 || got[11].Key != experience.KeyUnknown || !got[11].Pressed || got[12].Pressed {
		t.Fatal("direct input lost scroll units or high key edges")
	}
	got = state.directEvents(got[:0], []input.Event{{Kind: input.Down, Code: 0x112, Time: 14}})
	if len(got) != 1 || got[0].Button != experience.ButtonMiddle || got[0].Modifiers != 0 || got[0].Time != 14 {
		t.Fatal("reused event storage replayed an old batch")
	}
}

func TestDirectQueueCancelClearsHeldKeysLocksAndPointerCapture(t *testing.T) {
	var state keyState
	state.directEvents(nil, []input.Event{
		{Kind: input.KeyInput, Code: 29, Pressed: true},
		{Kind: input.KeyInput, Code: 58, Pressed: true},
		{Kind: input.KeyInput, Code: 767, Pressed: true},
	})
	got := state.directEvents(nil, []input.Event{
		{Kind: input.Cancel, Time: 90},
		{Kind: input.Move, X: 16, Y: 20, Time: 91},
		{Kind: input.KeyInput, Code: 16, Pressed: true, Time: 92},
	})
	if len(got) != 4 || got[0].Kind != experience.KeyboardCancel || got[1].Kind != experience.PointerCancel || got[0].Time != 90 || got[1].Time != 90 {
		t.Fatalf("input loss did not cancel both owners in place: %+v", got)
	}
	for _, event := range got[2:] {
		if event.Modifiers != 0 || event.Depressed != 0 || event.Locked != 0 {
			t.Fatalf("input loss retained stale modifier state: %+v", event)
		}
	}
	if state.held[767] || isHostShortcut(got[3], experience.KeyQ) {
		t.Fatal("input after cancellation revived a held key or stale quit chord")
	}
}

type directQueueApplications struct {
	hubProvider
	events []experience.Event
	seat   []experience.Event
}

func (p *directQueueApplications) Send(_ uint64, event experience.Event) {
	p.events = append(p.events, event)
}

func (p *directQueueApplications) Seat(event experience.Event) { p.seat = append(p.seat, event) }

func TestDirectQueueRoutesClickThenKeysAndCancelsInputLoss(t *testing.T) {
	texture, err := render.NewTexture(960, 600, make([]byte, 960*600*4))
	if err != nil {
		t.Fatal(err)
	}
	provider := &directQueueApplications{hubProvider: hubProvider{surfaces: []experience.ApplicationSurface{{ID: 31, Key: "direct-test", Title: "Synthetic input target", Texture: texture}}}}
	hub := newApplicationHub(provider)
	work, err := workspace.New()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Close()
	work.SetApplications(hub)
	defer work.SetApplications(nil)
	if err := work.Dispatch(workspace.Action{Kind: workspace.ToggleApplicationReading}); err != nil {
		t.Fatal(err)
	}
	frame := work.Draw(1440, 900)
	// Read mode centers the selected application in the scene viewport.
	x, y := 0, 0
	for _, command := range frame.Commands {
		if command.Kind == render.SceneCommand {
			v := command.View.Viewport
			x, y = int(v[0]+v[2]/2), int(v[1]+v[3]/2)
			break
		}
	}
	if x == 0 || y == 0 || work.OwnsKeyboard() {
		t.Fatal("fixture did not prepare an unfocused application in Read mode")
	}
	var state keyState
	events := state.directEvents(nil, []input.Event{
		{Kind: input.KeyInput, Code: 30, Pressed: true, Time: 1},
		{Kind: input.KeyInput, Code: 30, Time: 2},
		{Kind: input.Move, X: x, Y: y, Time: 3},
		{Kind: input.Down, Code: 0x110, X: x, Y: y, Time: 4},
		{Kind: input.Up, Code: 0x110, X: x, Y: y, Time: 5},
		{Kind: input.KeyInput, Code: 29, Pressed: true, Time: 6},
		{Kind: input.KeyInput, Code: 31, Pressed: true, Time: 7},
		{Kind: input.KeyInput, Code: 31, Time: 8},
		{Kind: input.Scroll, X: x, Y: y, ScrollY: -10, Time: 9},
		{Kind: input.Down, Code: 0x111, X: x, Y: y, Time: 10},
	})
	dispatch := func(events []experience.Event) {
		t.Helper()
		for _, event := range events {
			hub.seat(event)
			if quit, err := dispatchEvent(work, event, "", io.Discard); err != nil || quit {
				t.Fatalf("queued event caused unexpected host exit: %+v quit=%v err=%v", event, quit, err)
			}
		}
	}
	dispatch(events)
	if !work.OwnsKeyboard() || provider.focused != 31 {
		t.Fatal("queued click did not grant focus before subsequent keys")
	}
	var delivered []experience.Event
	for _, event := range provider.events {
		if event.Kind == experience.KeyInput || event.Kind == experience.PointerDown || event.Kind == experience.PointerUp || event.Kind == experience.PointerScroll {
			delivered = append(delivered, event)
		}
	}
	if len(delivered) != 7 {
		t.Fatalf("queue lost transitions or delivered a key before the click: %+v", delivered)
	}
	for i, want := range []uint32{4, 5, 6, 7, 8, 9, 10} {
		if delivered[i].Time != want {
			t.Fatalf("application input was reordered at %d: %+v", i, delivered)
		}
	}
	if delivered[3].Key != experience.KeyS || delivered[3].Modifiers != experience.ModControl || delivered[5].ScrollY != -10 || delivered[6].ButtonCode != 0x111 {
		t.Fatal("focus routing lost a client shortcut, scroll, or secondary button")
	}
	provider.events = nil
	dispatch(state.directEvents(events[:0], []input.Event{
		{Kind: input.Cancel, Time: 11},
		{Kind: input.Up, Code: 0x111, X: x, Y: y, Time: 12},
		{Kind: input.KeyInput, Code: 16, Pressed: true, Time: 13},
	}))
	if work.OwnsKeyboard() || provider.focused != 0 {
		t.Fatal("input loss retained keyboard ownership")
	}
	for _, event := range provider.events {
		if event.Kind == experience.KeyInput || event.Kind == experience.PointerUp {
			t.Fatalf("input loss left a live capture or key route: %+v", event)
		}
	}
	if len(provider.seat) != 14 || provider.seat[10].Kind != experience.KeyboardCancel || provider.seat[11].Kind != experience.PointerCancel {
		t.Fatal("ordered direct events did not reach application seat metadata")
	}
}
