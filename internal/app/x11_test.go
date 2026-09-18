package app

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/platform/linux/apps"
	"github.com/codemodify/worldr/internal/platform/linux/xwayland"
)

func TestX11OptionsPreserveMixedLaunchOrderAndArguments(t *testing.T) {
	o, err := Parse([]string{"--app=foot", "--x11-app=xmessage", "--app=konsole", "--", "literal $argument", "with spaces"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	launches, err := applicationLaunches(o)
	if err != nil {
		t.Fatal(err)
	}
	want := []ApplicationLaunch{{ID: "launch-1", Command: "foot", Args: []string{"literal $argument", "with spaces"}}, {ID: "launch-2", Command: "xmessage", Args: []string{"literal $argument", "with spaces"}, X11: true}, {ID: "launch-3", Command: "konsole", Args: []string{"literal $argument", "with spaces"}}}
	if !o.Xwayland || !reflect.DeepEqual(launches, want) {
		t.Fatalf("mixed launch plan: %+v enabled=%v", launches, o.Xwayland)
	}
	o, err = Parse([]string{"--xwayland", "--terminal"}, io.Discard)
	if err != nil || !o.Xwayland || len(o.Launches) != 0 {
		t.Fatalf("bridge-only opt-in: %+v %v", o, err)
	}
	var flags []string
	for i := 0; i < 33; i++ {
		if i%2 == 0 {
			flags = append(flags, "--app=foot")
		} else {
			flags = append(flags, "--x11-app=xmessage")
		}
	}
	if _, err := Parse(flags, io.Discard); err == nil {
		t.Fatal("mixed launches exceeded shared capacity")
	}
	path := filepath.Join(t.TempDir(), "apps.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"applications":[{"id":"legacy","command":"xmessage","x11":true,"args":["hello world"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	o, err = Parse([]string{"--apps", path}, io.Discard)
	if err != nil || !o.Xwayland || len(o.Launches) != 1 || !o.Launches[0].X11 {
		t.Fatalf("X11 profile: %+v %v", o, err)
	}
	if _, err := Parse([]string{"--apps", path, "--x11-app=xmessage"}, io.Discard); err == nil {
		t.Fatal("ambiguous profile/CLI launch accepted")
	}
}

type fakeX11Bridge struct {
	windows                  map[uint64]xwayland.Window
	focused, resized, closed uint64
	size                     [2]int
	polls                    int
	dragSource               uint64
	dragMotion               [2]uint64
	dragPosition             [2]float32
	dropped                  [2]uint64
	dragCancelled            bool
}

func (b *fakeX11Bridge) Poll() error { b.polls++; return nil }
func (*fakeX11Bridge) Close() error  { return nil }
func (*fakeX11Bridge) Environment([]string) []string {
	return []string{"DISPLAY=:71", "XAUTHORITY=/private/authority", "WAYLAND_DISPLAY=/private/wayland", "XDG_SESSION_TYPE=x11"}
}
func (b *fakeX11Bridge) Window(id uint64) (xwayland.Window, bool) {
	w, ok := b.windows[id]
	return w, ok
}
func (b *fakeX11Bridge) Focus(id uint64) error { b.focused = id; return nil }
func (b *fakeX11Bridge) Resize(id uint64, w, h int) error {
	b.resized = id
	b.size = [2]int{w, h}
	return nil
}
func (b *fakeX11Bridge) CloseSurface(id uint64) error { b.closed = id; return nil }
func (b *fakeX11Bridge) DragActive(id uint64) bool    { return b.dragSource == id }
func (b *fakeX11Bridge) DragMotion(source, target uint64, x, y float32, _ uint32) error {
	b.dragMotion, b.dragPosition = [2]uint64{source, target}, [2]float32{x, y}
	return nil
}
func (b *fakeX11Bridge) DropXDND(source, target uint64, _ uint32) error {
	b.dropped = [2]uint64{source, target}
	return nil
}
func (b *fakeX11Bridge) CancelXDND() error { b.dragCancelled = true; return nil }

type mixedProtocolServer struct {
	adapterServer
	resized, closed uint64
}

func (s *mixedProtocolServer) Resize(id uint64, w, h int) error { s.resized = id; return nil }
func (s *mixedProtocolServer) CloseSurface(id uint64) error     { s.closed = id; return nil }

func TestX11ControllerRoutesPolicyAndUsesClientPID(t *testing.T) {
	s := &mixedProtocolServer{adapterServer: adapterServer{current: []apps.Surface{{ID: 7, PID: 900, AppID: "xmessage", Width: 1, Height: 1, Revision: 1, Pixels: []byte{1, 2, 3, 255}}, {ID: 8, PID: 430, AppID: "foot", Width: 1, Height: 1, Revision: 1, Pixels: []byte{4, 5, 6, 255}}}}}
	b := &fakeX11Bridge{windows: map[uint64]xwayland.Window{7: {ID: 0x100, SurfaceID: 7, PID: 420}}}
	a := &applicationController{server: s, x11: b, images: make(map[uint64]*applicationImage), output: io.Discard, children: []*applicationProcess{{cmd: &exec.Cmd{Process: &os.Process{Pid: 420}}, wait: make(chan error), slot: "x11"}, {cmd: &exec.Cmd{Process: &os.Process{Pid: 430}}, wait: make(chan error), slot: "wayland"}}}
	if err := a.Poll(); err != nil {
		t.Fatal(err)
	}
	if b.polls != 1 || a.images[7].surface.Key != "x11/window-1" || a.images[8].surface.Key != "wayland/window-1" {
		t.Fatal("Xwayland PID replaced per-client launch identity")
	}
	a.Focus(7)
	if b.focused != 7 || s.focused != 7 {
		t.Fatal("X11 focus failed to update both XWM and seat")
	}
	a.Focus(8)
	if b.focused != 0 || s.focused != 8 {
		t.Fatal("Wayland focus left X11 keyboard focus active")
	}
	a.Resize(7, 400, 300)
	a.CloseApplication(7)
	if b.resized != 7 || b.size != [2]int{400, 300} || b.closed != 7 || s.resized != 0 || s.closed != 0 {
		t.Fatal("X11 policy was sent to xdg-shell")
	}
	a.Resize(8, 500, 300)
	a.CloseApplication(8)
	if s.resized != 8 || s.closed != 8 {
		t.Fatal("Wayland policy was sent to XWM")
	}
	env := strings.Join(a.terminalEnvironment([]string{"DISPLAY=:host", "XAUTHORITY=/host"}), "\n")
	for _, want := range []string{"DISPLAY=:71", "XAUTHORITY=/private/authority", "WAYLAND_DISPLAY=/private/wayland", "XDG_SESSION_TYPE=wayland"} {
		if !strings.Contains(env, want) {
			t.Fatalf("native shell lost %q: %s", want, env)
		}
	}
	if strings.Contains(env, "/host") || strings.Contains(env, ":host") {
		t.Fatal("native shell inherited host X11 credentials")
	}
	a.closed = true
	a.Focus(7)
	a.Resize(7, 2, 2)
	a.CloseApplication(7)
	a.Send(7, experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: true})
	if b.focused != 0 || s.focused != 8 || b.size != [2]int{400, 300} || s.key != 0 {
		t.Fatal("closed provider dispatched input")
	}
}

func TestX11ControllerRoutesSpatialXDND(t *testing.T) {
	s := &mixedProtocolServer{adapterServer: adapterServer{current: []apps.Surface{
		{ID: 7, Width: 4, Height: 2, LogicalWidth: 2, LogicalHeight: 1, Revision: 1, Pixels: make([]byte, 32)},
		{ID: 8, Width: 8, Height: 4, LogicalWidth: 4, LogicalHeight: 2, Revision: 1, Pixels: make([]byte, 128)},
	}}}
	b := &fakeX11Bridge{windows: map[uint64]xwayland.Window{7: {ID: 0x100, SurfaceID: 7}, 8: {ID: 0x200, SurfaceID: 8}}, dragSource: 7}
	a := &applicationController{server: s, x11: b, images: make(map[uint64]*applicationImage), output: io.Discard}
	if err := a.Poll(); err != nil {
		t.Fatal(err)
	}
	if !a.ApplicationDragActive(7) || !a.ApplicationDrag(7, 8, experience.Event{Kind: experience.PointerMove, X: 4, Y: 2, Time: 31}) {
		t.Fatal("X11 selection did not enter application drag routing")
	}
	if b.dragMotion != [2]uint64{7, 8} || b.dragPosition != [2]float32{2, 1} || s.pointerID != 8 || s.x != 2 || s.y != 1 {
		t.Fatalf("XDND motion bridge=%v pos=%v seat=%d %.1f %.1f", b.dragMotion, b.dragPosition, s.pointerID, s.x, s.y)
	}
	if !a.ApplicationDrag(7, 8, experience.Event{Kind: experience.PointerUp, X: 4, Y: 2, Button: experience.ButtonPrimary, Time: 32}) {
		t.Fatal("accepted X11 target was not retained for drop")
	}
	if b.dropped != [2]uint64{7, 8} || s.button != 272 || s.pressed || b.dragCancelled {
		t.Fatalf("XDND drop=%v button=%d/%v cancelled=%v", b.dropped, s.button, s.pressed, b.dragCancelled)
	}
}
