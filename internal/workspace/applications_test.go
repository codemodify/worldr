package workspace

import (
	"bytes"
	"math"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

type applicationEvent struct {
	id    uint64
	event experience.Event
}
type applicationResize struct {
	id            uint64
	width, height int
}
type fakeApplications struct {
	surfaces []experience.ApplicationSurface
	focus    []uint64
	events   []applicationEvent
	resizes  []applicationResize
}

func (f *fakeApplications) Surfaces() []experience.ApplicationSurface { return f.surfaces }
func (f *fakeApplications) Focus(id uint64)                           { f.focus = append(f.focus, id) }
func (f *fakeApplications) Send(id uint64, event experience.Event) {
	f.events = append(f.events, applicationEvent{id, event})
}
func (f *fakeApplications) Resize(id uint64, width, height int) {
	f.resizes = append(f.resizes, applicationResize{id, width, height})
}

func applicationStudy(t *testing.T) (*Workspace, *fakeApplications) {
	t.Helper()
	w := study(t)
	texture, err := render.NewTexture(960, 600, make([]byte, 960*600*4))
	if err != nil {
		t.Fatal(err)
	}
	apps := &fakeApplications{surfaces: []experience.ApplicationSurface{{ID: 777, Title: "foot — shell", Texture: texture}}}
	w.SetApplications(apps)
	w.Draw(1440, 900)
	return w, apps
}

func applicationPoint(w *Workspace, x, y float32) (float32, float32) {
	tw, th := w.application.Texture.Size()
	point := w.scene.Node(w.applicationNode).Transform.TransformPoint(scene.Vec3{X: x/float32(tw) - .5, Y: .5 - y/float32(th)})
	sx, sy, _, _ := w.camera.Project(point, w.viewport)
	return sx, sy
}

func visibleApplicationPoint(t *testing.T, w *Workspace) (float32, float32) {
	t.Helper()
	for y := float32(60); y < 600; y += 60 {
		for x := float32(96); x < 960; x += 96 {
			sx, sy := applicationPoint(w, x, y)
			if _, ok := w.applicationHit(sx, sy); ok {
				return sx, sy
			}
		}
	}
	t.Fatal("application has no visible hit")
	return 0, 0
}

func TestApplicationFocusOwnsEveryRawKey(t *testing.T) {
	w, apps := applicationStudy(t)
	x, y := visibleApplicationPoint(t, w)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if !w.OwnsKeyboard() || apps.focus[len(apps.focus)-1] != 777 {
		t.Fatal("visible application click did not grant keyboard focus")
	}
	before := w.Document()
	events := []experience.Event{
		{Kind: experience.KeyInput, Key: experience.KeyE, Keycode: 18, Pressed: true},
		{Kind: experience.KeyInput, Key: experience.KeyB, Keycode: 48, Pressed: true},
		{Kind: experience.KeyInput, Key: experience.KeyS, Keycode: 31, Pressed: true, Modifiers: experience.ModControl},
		{Kind: experience.KeyInput, Key: experience.KeyQ, Keycode: 16, Pressed: true, Modifiers: experience.ModControl},
		{Kind: experience.KeyInput, Keycode: 30, Pressed: true, Repeat: true},
		{Kind: experience.KeyInput, Keycode: 30, Pressed: false},
		{Kind: experience.KeyboardModifiers, Depressed: 4, Latched: 2, Locked: 1, Group: 1},
	}
	for _, event := range events {
		if !w.Handle(event) {
			t.Fatalf("application key event was not consumed: %+v", event)
		}
		got := apps.events[len(apps.events)-1]
		if got.id != 777 || got.event != event {
			t.Fatalf("raw event changed: got %+v want %+v", got, event)
		}
	}
	if w.Document() != before {
		t.Fatal("application typing triggered native workspace shortcuts")
	}
	pointer(w, experience.PointerDown, 10, 100)
	if w.OwnsKeyboard() || apps.focus[len(apps.focus)-1] != 0 {
		t.Fatal("background click did not clear keyboard focus")
	}
	if !key(w, experience.KeyE, 0) || w.Document().View.Exploded == before.View.Exploded {
		t.Fatal("native key ownership did not resume")
	}
}

func TestApplicationCaptureMapsOutsideBoundsAndSupportsAllButtons(t *testing.T) {
	w, apps := applicationStudy(t)
	command(t, w, Action{Kind: ToggleApplicationReading})
	w.Draw(1440, 900)
	x, y := visibleApplicationPoint(t, w)
	for _, button := range []experience.Button{experience.ButtonPrimary, experience.ButtonSecondary, experience.ButtonMiddle} {
		if !w.Handle(experience.Event{Kind: experience.PointerDown, X: x, Y: y, Button: button}) {
			t.Fatal("raw button press not consumed")
		}
	}
	count := len(apps.events)
	w.Handle(experience.Event{Kind: experience.PointerUp, X: x, Y: y, ButtonCode: 275})
	if len(apps.events) != count || len(w.applicationButtons) != 3 {
		t.Fatal("stray release reached the client or ended a held button")
	}
	w.Handle(experience.Event{Kind: experience.PointerUp, X: x, Y: y, Button: experience.ButtonMiddle})
	if len(w.applicationButtons) != 2 || apps.events[len(apps.events)-1].event.ButtonCode != 274 {
		t.Fatal("middle button fallback did not preserve its evdev code")
	}
	sx, sy := applicationPoint(w, -20, 300)
	move := experience.Event{Kind: experience.PointerMove, X: sx, Y: sy, Time: 123}
	if !w.Handle(move) {
		t.Fatal("capture lost outside surface")
	}
	got := apps.events[len(apps.events)-1].event
	if math.Abs(float64(got.X+20)) > .05 || math.Abs(float64(got.Y-300)) > .05 || got.Time != 123 {
		t.Fatalf("capture mapping lost texture coordinates: %+v", got)
	}
	sx, sy = applicationPoint(w, 1200, 700)
	if sx >= w.viewport.X && sx < w.viewport.X+w.viewport.Width && sy >= w.viewport.Y && sy < w.viewport.Y+w.viewport.Height {
		t.Fatal("capture test did not leave the viewport")
	}
	w.Handle(experience.Event{Kind: experience.PointerMove, X: sx, Y: sy})
	got = apps.events[len(apps.events)-1].event
	if math.Abs(float64(got.X-1200)) > .05 || math.Abs(float64(got.Y-700)) > .05 {
		t.Fatalf("viewport exit stopped captured mapping: %+v", got)
	}
	if !w.Handle(experience.Event{Kind: experience.PointerUp, X: sx, Y: sy, ButtonCode: 273}) || len(w.applicationButtons) != 1 {
		t.Fatal("secondary release ended primary capture")
	}
	if !w.Handle(experience.Event{Kind: experience.PointerScroll, X: sx, Y: sy, ScrollX: 3, ScrollY: -8}) {
		t.Fatal("captured scroll was not routed")
	}
	got = apps.events[len(apps.events)-1].event
	if got.ScrollX != 3 || got.ScrollY != -8 {
		t.Fatal("scroll changed in routing")
	}
	if !w.Handle(experience.Event{Kind: experience.PointerUp, X: sx, Y: sy, ButtonCode: 272}) || len(w.applicationButtons) != 0 {
		t.Fatal("final button did not end capture")
	}
	// Host keyboard loss also revokes any in-flight pointer selection.
	w.Handle(experience.Event{Kind: experience.PointerDown, X: x, Y: y, ButtonCode: 272})
	if !w.Handle(experience.Event{Kind: experience.KeyboardCancel}) || w.OwnsKeyboard() || len(w.applicationButtons) != 0 {
		t.Fatal("keyboard loss left focus or capture behind")
	}
	if apps.focus[len(apps.focus)-1] != 0 {
		t.Fatal("keyboard cancellation did not reach adapter")
	}
}

func TestKeyboardLossCancelsNativeGesture(t *testing.T) {
	w := study(t)
	w.Draw(1440, 900)
	before := w.Document()
	pointer(w, experience.PointerDown, 600, 400)
	pointer(w, experience.PointerMove, 670, 450)
	if w.pointer.kind != captureOrbit || w.Document().View.Camera == before.View.Camera {
		t.Fatal("native orbit gesture did not start")
	}
	if !w.Handle(experience.Event{Kind: experience.KeyboardCancel}) || w.pointer.kind != captureNone || w.Document() != before {
		t.Fatal("host keyboard loss did not cancel native gesture")
	}
}

func TestOccludedApplicationCannotAcquireFocus(t *testing.T) {
	w, apps := applicationStudy(t)
	command(t, w, Action{Kind: ToggleApplicationDepth})
	w.Draw(1440, 900)
	for y := float32(30); y < 600; y += 24 {
		for x := float32(24); x < 960; x += 24 {
			sx, sy := applicationPoint(w, x, y)
			hit, ok := w.scene.Pick(w.camera, w.viewport, sx, sy)
			if !ok || hit.Surface {
				continue
			}
			count := len(apps.events)
			pointer(w, experience.PointerDown, sx, sy)
			if w.OwnsKeyboard() || len(apps.events) != count || w.pointer.kind != captureOrbit {
				t.Fatal("hidden application intercepted visible mesh press")
			}
			w.Handle(experience.Event{Kind: experience.PointerCancel})
			return
		}
	}
	t.Fatal("rear application did not overlap any native geometry")
}

func TestApplicationReadReturnResizePersistenceAndClose(t *testing.T) {
	w, apps := applicationStudy(t)
	initialCamera, initialTransform := w.camera, w.scene.Node(w.applicationNode).Transform
	click := func(b box) {
		pointer(w, experience.PointerDown, b.x+12, b.y+12)
		pointer(w, experience.PointerUp, b.x+12, b.y+12)
		w.Draw(1440, 900)
	}
	click(applicationReadButton)
	if !w.m.applicationReading || w.scene.Node(w.applicationNode).Transform != initialTransform || w.camera == initialCamera {
		t.Fatal("read mode failed to frame app without moving it")
	}
	for _, id := range w.nodes {
		if !w.scene.Node(id).Hidden {
			t.Fatal("read mode did not isolate application")
		}
	}
	click(applicationReadButton)
	if w.m.applicationReading || w.camera != initialCamera || w.scene.Node(w.applicationNode).Transform != initialTransform {
		t.Fatal("return did not restore spatial view")
	}
	click(applicationSizeButton)
	if last := apps.resizes[len(apps.resizes)-1]; last.width != 1440 || last.height != 900 || !w.m.applicationWide {
		t.Fatal("wide preset did not configure the app")
	}
	command(t, w, Action{Kind: Undo})
	if last := apps.resizes[len(apps.resizes)-1]; last.width != 960 || last.height != 600 {
		t.Fatal("resize undo did not configure compact size")
	}
	command(t, w, Action{Kind: Redo})
	click(applicationDepthButton)
	click(applicationReadButton)
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("777")) || bytes.Contains(data, []byte("foot")) {
		t.Fatal("live application identity leaked into saved state")
	}
	loaded := study(t)
	if err := loaded.LoadState(data); err != nil || loaded.Document() != w.Document() {
		t.Fatalf("application view did not survive save/load: %v", err)
	}
	x, y := visibleApplicationPoint(t, w)
	pointer(w, experience.PointerDown, x, y)
	apps.surfaces = nil
	w.Update(time.Millisecond)
	w.Draw(1440, 900)
	if w.OwnsKeyboard() || w.applicationNode != 0 || len(w.applicationButtons) != 0 || w.scene.Node(w.panelNode).Hidden {
		t.Fatal("closing application retained its input or hid the native panel")
	}
	for _, id := range w.nodes {
		if w.scene.Node(id).Hidden {
			t.Fatal("closing application left geometry isolated")
		}
	}
}
