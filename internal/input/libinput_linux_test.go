//go:build linux

package input

import (
	"errors"
	"github.com/codemodify/worldr/internal/platform/linux/seat"
	"testing"
)

type managedFixture struct {
	events []seat.InputEvent
	err    error
	closed int
}

func (f *managedFixture) Poll() ([]seat.InputEvent, error) {
	events := f.events
	f.events = nil
	return events, f.err
}
func (f *managedFixture) Close() error { f.closed++; return nil }
func TestManagedInputKeepsFractionalMotionAbsoluteCoordinatesAndRemovalCancel(t *testing.T) {
	f := &managedFixture{events: []seat.InputEvent{{Kind: seat.Motion, X: .25, Y: .25, Time: 1}, {Kind: seat.Motion, X: .25, Y: .25, Time: 2}, {Kind: seat.Button, Code: 0x110, Pressed: true, Time: 3}, {Kind: seat.Absolute, X: 1, Y: 0, Time: 4}, {Kind: seat.Scroll, X: 5, Y: -2.5, Time: 5}, {Kind: seat.Key, Code: 30, Pressed: true, Time: 6}, {Kind: seat.Cancel, Time: 7}}}
	p := &Pointer{W: 100, H: 80, X: 10, Y: 20, managedX: 10, managedY: 20, managed: f}
	p.Poll()
	if len(p.Events) != 7 || p.Events[0].X != 10 || p.Events[1].X != 11 || p.Events[2].X != 11 || p.Events[2].Kind != Down {
		t.Fatalf("fractional motion/button coordinates: %+v", p.Events)
	}
	if p.Events[3].X != 99 || p.Events[3].Y != 0 || p.Events[4].ScrollX != 5 || p.Events[4].ScrollY != -2.5 || p.Events[5].Code != 30 || p.Events[6].Kind != Cancel {
		t.Fatalf("absolute/scroll/key/device removal: %+v", p.Events)
	}
	p.Resize(50, 50)
	if p.X != 49 || p.W != 50 {
		t.Fatal("input bounds did not follow display reconfiguration")
	}
	p.Poll()
	if len(p.Events) != 0 {
		t.Fatal("managed input replayed a previous batch")
	}
	p.Close()
	p.Close()
	if f.closed != 1 {
		t.Fatal("input context closed more than once")
	}
}
func TestManagedInputFailureCancelsOnceAndReportsError(t *testing.T) {
	want := errors.New("revoked input fixture")
	f := &managedFixture{err: want}
	p := &Pointer{W: 40, H: 30, managed: f}
	p.Poll()
	if !errors.Is(p.Err(), want) || len(p.Events) != 1 || p.Events[0].Kind != Cancel {
		t.Fatal("input failure retained held state")
	}
	p.Poll()
	if len(p.Events) != 0 {
		t.Fatal("input failure repeated cancellation forever")
	}
}
