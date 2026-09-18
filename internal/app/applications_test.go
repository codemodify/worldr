package app

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"reflect"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/platform/linux/apps"
)

type adapterServer struct {
	applicationServer
	current   []apps.Surface
	focused   uint64
	pointerID uint64
	x, y      float32
	button    uint32
	pressed   bool
	key       uint32
	mods      [4]uint32
}

func (s *adapterServer) Poll() ([]apps.Surface, error) { return s.current, nil }
func (s *adapterServer) Focus(id uint64) error         { s.focused = id; return nil }
func (s *adapterServer) Pointer(id uint64, x, y float32) error {
	s.pointerID, s.x, s.y = id, x, y
	return nil
}
func (s *adapterServer) Button(code uint32, pressed bool, time uint32) error {
	s.button, s.pressed = code, pressed
	return nil
}
func (s *adapterServer) Key(code uint32, pressed bool, time, depressed, latched, locked, group uint32) error {
	s.key, s.pressed, s.mods = code, pressed, [4]uint32{depressed, latched, locked, group}
	return nil
}
func (s *adapterServer) Modifiers(depressed, latched, locked, group uint32) error {
	s.mods = [4]uint32{depressed, latched, locked, group}
	return nil
}

func TestApplicationAdapterRetainsAndRetiresResources(t *testing.T) {
	s := &adapterServer{current: []apps.Surface{{ID: 5, Title: "first", Width: 2, Height: 1, LogicalWidth: 2, LogicalHeight: 1, Revision: 1, Pixels: []byte{1, 2, 3, 255, 4, 5, 6, 255}}}}
	a := &applicationController{server: s, images: make(map[uint64]*applicationImage), output: io.Discard}
	if err := a.poll(); err != nil {
		t.Fatal(err)
	}
	texture := a.Surfaces()[0].Texture
	first, _ := texture.Snapshot(0)
	if err := a.poll(); err != nil {
		t.Fatal(err)
	}
	if texture.Revision() != first.Revision {
		t.Fatal("unchanged client image produced an upload")
	}
	s.current[0].Title = "renamed"
	if err := a.poll(); err != nil {
		t.Fatal(err)
	}
	if a.Surfaces()[0].Title != "renamed" || texture.Revision() != first.Revision {
		t.Fatal("title-only change modified texture")
	}
	s.current[0].Width, s.current[0].Height = 1, 2
	s.current[0].Revision++
	s.current[0].Pixels = []byte{10, 20, 30, 255, 40, 50, 60, 255}
	if err := a.poll(); err != nil {
		t.Fatal(err)
	}
	current, _ := texture.Snapshot(0)
	if a.Surfaces()[0].Texture != texture || current.Width != 1 || current.Height != 2 || current.Revision == first.Revision || !bytes.Equal(current.Pixels, s.current[0].Pixels) {
		t.Fatal("client resize lost texture identity or current pixels")
	}
	s.current = nil
	if err := a.poll(); err != nil {
		t.Fatal(err)
	}
	if len(a.Surfaces()) != 0 || len(a.images) != 0 || !reflect.DeepEqual(a.retired, []uint64{texture.ID()}) {
		t.Fatal("disconnected client retained a live texture")
	}
}

func TestApplicationAdapterMapsInputAndReleasesFocus(t *testing.T) {
	s := &adapterServer{current: []apps.Surface{{ID: 7, Width: 4, Height: 2, LogicalWidth: 2, LogicalHeight: 1, Revision: 1, Pixels: make([]byte, 32)}}}
	a := &applicationController{server: s, images: make(map[uint64]*applicationImage), output: io.Discard}
	if err := a.poll(); err != nil {
		t.Fatal(err)
	}
	a.seat(experience.Event{Kind: experience.KeyboardModifiers, Depressed: 1})
	if s.mods != [4]uint32{1, 0, 0, 0} {
		t.Fatal("unfocused seat modifiers were dropped before application focus")
	}
	a.Focus(7)
	a.Send(7, experience.Event{Kind: experience.PointerDown, X: 3, Y: 1, Button: experience.ButtonSecondary})
	if s.focused != 7 || s.pointerID != 7 || s.x != 1.5 || s.y != .5 || s.button != 0x111 || !s.pressed {
		t.Fatal("pointer was not mapped to the client's logical buffer extent")
	}
	a.Send(7, experience.Event{Kind: experience.KeyInput, Keycode: 20, Pressed: true, Depressed: 4, Locked: 2})
	if s.key != 20 || !s.pressed || s.mods != [4]uint32{4, 0, 2, 0} {
		t.Fatal("raw physical key or XKB state lost")
	}
	a.Send(7, experience.Event{Kind: experience.KeyboardModifiers})
	if s.mods != [4]uint32{} {
		t.Fatal("modifier-only release left a stuck modifier")
	}
	a.Send(7, experience.Event{Kind: experience.PointerCancel})
	a.Send(7, experience.Event{Kind: experience.KeyboardCancel})
	if s.pointerID != 0 || s.focused != 0 {
		t.Fatal("cancel did not release protocol focus")
	}
	a.Focus(999)
	if s.focused != 0 {
		t.Fatal("focused a disconnected client")
	}
}

func TestApplicationLaunchArgumentsAndEnvironment(t *testing.T) {
	o, err := Parse([]string{"--app", "foot", "--", "--config=/dev/null", "/bin/sh", "-c", "printf '%s' '$literal'"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if o.Application != "foot" || !reflect.DeepEqual(o.ApplicationArgs, []string{"--config=/dev/null", "/bin/sh", "-c", "printf '%s' '$literal'"}) {
		t.Fatal("application argument boundaries changed")
	}
	parent := []string{"WAYLAND_DISPLAY=host", "DISPLAY=:0", "WAYLAND_SOCKET=9", "XDG_SESSION_TYPE=x11", "XDG_RUNTIME_DIR=/run/user/1", "KEEP=yes"}
	before := append([]string(nil), parent...)
	got := applicationEnvironment(parent, "/private/worldr/socket")
	want := []string{"XDG_RUNTIME_DIR=/run/user/1", "KEEP=yes", "WAYLAND_DISPLAY=/private/worldr/socket", "XDG_SESSION_TYPE=wayland"}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(parent, before) {
		t.Fatalf("child display isolation failed: %v", got)
	}
	multiple, err := Parse([]string{"--app=foot", "--app=foot", "--", "--config=/dev/null"}, io.Discard)
	if err != nil || !reflect.DeepEqual(multiple.Applications, []string{"foot", "foot"}) || multiple.Application != "foot" || !reflect.DeepEqual(multiple.ApplicationArgs, []string{"--config=/dev/null"}) {
		t.Fatalf("repeated application arguments lost: %+v %v", multiple, err)
	}
}

func TestApplicationKeysSurviveMapOrderAndReconnect(t *testing.T) {
	first := apps.Surface{ID: 91, PID: 420, AppID: "foot", Width: 1, Height: 1, Revision: 1, Pixels: []byte{0, 0, 0, 255}}
	second := apps.Surface{ID: 72, PID: 430, AppID: "foot", Width: 1, Height: 1, Revision: 1, Pixels: []byte{0, 0, 0, 255}}
	s := &adapterServer{current: []apps.Surface{second, first}}
	a := &applicationController{server: s, images: make(map[uint64]*applicationImage), output: io.Discard, children: []*applicationProcess{
		{cmd: &exec.Cmd{Process: &os.Process{Pid: 420}}, wait: make(chan error), slot: "launch-1"},
		{cmd: &exec.Cmd{Process: &os.Process{Pid: 430}}, wait: make(chan error), slot: "launch-2"},
	}}
	if err := a.poll(); err != nil {
		t.Fatal(err)
	}
	if a.images[91].surface.Key != "launch-1/window-1" || a.images[72].surface.Key != "launch-2/window-1" {
		t.Fatal("connection order replaced launch identity")
	}
	first.ID = 999 // The first client's connection has been replaced.
	s.current = []apps.Surface{first, second}
	if err := a.poll(); err != nil {
		t.Fatal(err)
	}
	if a.images[999].surface.Key != "launch-1/window-1" || a.images[72].surface.Key != "launch-2/window-1" {
		t.Fatal("reconnection lost the application layout association")
	}
	third := first
	third.ID = 1000
	s.current = []apps.Surface{second, first, third}
	if err := a.poll(); err != nil {
		t.Fatal(err)
	}
	if a.images[1000].surface.Key != "launch-1/window-2" || a.images[999].surface.Key != "launch-1/window-1" {
		t.Fatal("a second window changed its live sibling's stable key")
	}
}

type focusedApplicationExperience struct {
	experience.Experience
	events []experience.Event
}

func (s *focusedApplicationExperience) OwnsKeyboard() bool { return true }
func (s *focusedApplicationExperience) Handle(e experience.Event) bool {
	s.events = append(s.events, e)
	return true
}

func TestFocusedApplicationOwnsOrdinaryHostChords(t *testing.T) {
	work := &focusedApplicationExperience{}
	for _, key := range []experience.Key{experience.KeyQ, experience.KeyS, experience.KeyE} {
		e := experience.Event{Kind: experience.KeyInput, Key: key, Modifiers: experience.ModControl, Pressed: true, Keycode: 16}
		quit, err := dispatchEvent(work, e, "", io.Discard)
		if err != nil || quit || work.events[len(work.events)-1] != e {
			t.Fatal("host intercepted focused client key", key)
		}
	}
	before := len(work.events)
	quit, err := dispatchEvent(work, experience.Event{Kind: experience.KeyInput, Key: experience.KeyQ, Modifiers: experience.ModControl | experience.ModAlt, Pressed: true}, "", io.Discard)
	if err != nil || !quit || len(work.events) != before {
		t.Fatal("explicit exit chord failed or leaked into client")
	}
}
