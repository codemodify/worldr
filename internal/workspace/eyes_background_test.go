package workspace

import (
	"math"
	"reflect"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

func TestAmbientPointerObservationIsPassiveAndResetsOnCancel(t *testing.T) {
	var pointerState ambientPointerState
	for _, kind := range []experience.EventKind{
		experience.PointerMove, experience.PointerDown, experience.PointerUp, experience.PointerScroll,
	} {
		event := experience.Event{Kind: kind, X: 317.5, Y: 208.25}
		pointerState.Observe(event)
		if !pointerState.valid || pointerState.x != event.X || pointerState.y != event.Y {
			t.Fatalf("pointer event %v was not observed exactly: %+v", kind, pointerState)
		}
	}
	before := pointerState
	pointerState.Observe(experience.Event{Kind: experience.KeyInput, Key: experience.KeyF1, Pressed: true})
	if pointerState != before {
		t.Fatal("non-pointer input changed passive pointer state")
	}
	pointerState.Observe(experience.Event{Kind: experience.PointerCancel})
	if pointerState != (ambientPointerState{}) {
		t.Fatal("pointer cancellation retained a stale gaze target")
	}
	pointerState.Observe(experience.Event{Kind: experience.PointerMove, X: float32(math.NaN()), Y: 10})
	if pointerState.valid {
		t.Fatal("non-finite pointer coordinates became a gaze target")
	}
}

func TestAmbientPointerConvertsFramebufferToDesignCoordinates(t *testing.T) {
	p := ambientPointerState{x: 620, y: 390, valid: true}
	x, y, ok := p.designPoint(20, 30, 2)
	if !ok || x != 300 || y != 180 {
		t.Fatalf("scaled design point = (%v, %v, %t)", x, y, ok)
	}
	for _, scale := range []float32{0, -1, float32(math.NaN()), float32(math.Inf(1))} {
		if _, _, valid := p.designPoint(20, 30, scale); valid {
			t.Fatalf("invalid display scale %v produced a gaze target", scale)
		}
	}
	p.valid = false
	if _, _, valid := p.designPoint(0, 0, 1); valid {
		t.Fatal("cancelled pointer produced design coordinates")
	}
}

func TestEyeGazeTracksAndNeverLeavesSclera(t *testing.T) {
	const cx, cy, radius = float32(20), float32(30), float32(40)
	centerX, centerY := eyeGazePoint(cx, cy, radius, cx, cy)
	if centerX != cx || centerY != cy {
		t.Fatal("a centered target moved the iris")
	}

	tests := []struct {
		name                 string
		x, y                 float32
		wantXSign, wantYSign int
	}{
		{name: "right", x: 10000, y: cy, wantXSign: 1},
		{name: "left", x: -10000, y: cy, wantXSign: -1},
		{name: "down", x: cx, y: 10000, wantYSign: 1},
		{name: "up", x: cx, y: -10000, wantYSign: -1},
		{name: "diagonal", x: 500, y: -700, wantXSign: 1, wantYSign: -1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			x, y := eyeGazePoint(cx, cy, radius, test.x, test.y)
			dx, dy := x-cx, y-cy
			if !finite(float64(x)) || !finite(float64(y)) || float32(math.Hypot(float64(dx), float64(dy))) > radius*.34001 {
				t.Fatalf("gaze escaped its finite clamp: (%v, %v)", x, y)
			}
			if test.wantXSign > 0 && dx <= 0 || test.wantXSign < 0 && dx >= 0 ||
				test.wantYSign > 0 && dy <= 0 || test.wantYSign < 0 && dy >= 0 {
				t.Fatalf("gaze did not follow target: delta=(%v, %v)", dx, dy)
			}
		})
	}
	for _, invalid := range [][3]float32{
		{0, 100, 100},
		{-1, 100, 100},
		{radius, float32(math.NaN()), 100},
		{radius, 100, float32(math.Inf(1))},
	} {
		x, y := eyeGazePoint(cx, cy, invalid[0], invalid[1], invalid[2])
		if x != cx || y != cy {
			t.Fatalf("invalid gaze input moved iris: (%v, %v)", x, y)
		}
	}
}

func TestBackgroundEyesRenderThreeDeterministicToggleableOrnaments(t *testing.T) {
	if len(backgroundEyes) != 3 {
		t.Fatalf("background contains %d eyes, want 3", len(backgroundEyes))
	}
	canvas, err := scene.NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Close()
	w := &Workspace{
		desktop: true, canvas: canvas, scale: 1,
		environment:    environmentSettings{Eyes: true},
		ambientPointer: ambientPointerState{x: 1260, y: 780, valid: true},
	}

	renderEyes := func() []render.Vertex {
		canvas.Reset(1440, 900)
		w.drawBackgroundEyes()
		return append([]render.Vertex(nil), canvas.Frame().Vertices...)
	}
	beforePointer := w.ambientPointer
	first, second := renderEyes(), renderEyes()
	if len(first) == 0 || !reflect.DeepEqual(first, second) {
		t.Fatal("eye drawing was empty or changed without input")
	}
	if w.ambientPointer != beforePointer {
		t.Fatal("drawing mutated passive pointer state")
	}
	for _, vertex := range first {
		for _, value := range [...]float32{vertex.X, vertex.Y, vertex.Z, vertex.R, vertex.G, vertex.B, vertex.A} {
			if !finite(float64(value)) {
				t.Fatal("eye drawing emitted non-finite geometry")
			}
		}
	}
	w.environment.Eyes = false
	if disabled := renderEyes(); len(disabled) != 0 {
		t.Fatalf("disabled eyes submitted %d vertices", len(disabled))
	}
}

func TestBackgroundEyesObserveApplicationMotionWithoutTakingCapture(t *testing.T) {
	w := desktop(t)
	apps := desktopApplications(t, w)
	x, y := visibleApplication(t, w, apps.surfaces[0])
	apps.events = nil
	beforeCapture, beforeDocument, beforeHistory := w.pointer, w.Document(), w.historyPosition
	event := experience.Event{Kind: experience.PointerMove, X: x, Y: y, Time: 91}
	if !w.Handle(event) {
		t.Fatal("application motion was not routed")
	}
	if !w.ambientPointer.valid || w.ambientPointer.x != x || w.ambientPointer.y != y {
		t.Fatal("ambient eyes missed application-owned pointer motion")
	}
	if len(apps.events) != 1 || apps.events[0].id != apps.surfaces[0].ID || apps.events[0].event.Kind != experience.PointerMove {
		t.Fatal("passive eye observation intercepted application motion")
	}
	if w.pointer != beforeCapture || w.Document() != beforeDocument || w.historyPosition != beforeHistory {
		t.Fatal("passive eye observation changed capture or workspace state")
	}
	w.Handle(experience.Event{Kind: experience.PointerCancel})
	if w.ambientPointer.valid {
		t.Fatal("pointer cancellation did not center the background eyes")
	}
}

func TestBackgroundEyesDrawAfterBackdropAndBeforeApplications(t *testing.T) {
	w := desktop(t)
	apps := desktopApplications(t, w)
	frame := w.Draw(1440, 900)
	backdropIndex, _, _ := dnaBackgroundCommand(t, w, frame)
	applicationIndex := -1
	for index, command := range frame.Commands {
		for _, draw := range command.Draws {
			if draw.Texture == apps.surfaces[0].Texture {
				applicationIndex = index
			}
		}
	}
	if applicationIndex <= backdropIndex {
		t.Fatal("fixture did not draw an application after the backdrop")
	}
	eyesBetween := false
	for index := backdropIndex + 1; index < applicationIndex; index++ {
		eyesBetween = eyesBetween || frame.Commands[index].Kind == render.OverlayCommand && frame.Commands[index].Count > 0
	}
	if !eyesBetween {
		t.Fatal("eyes were not submitted between the retained backdrop and applications")
	}

	w.environment.Eyes = false
	frame = w.Draw(1440, 900)
	backdropIndex, _, _ = dnaBackgroundCommand(t, w, frame)
	applicationIndex = -1
	for index, command := range frame.Commands {
		for _, draw := range command.Draws {
			if draw.Texture == apps.surfaces[0].Texture {
				applicationIndex = index
			}
		}
	}
	for index := backdropIndex + 1; index < applicationIndex; index++ {
		if frame.Commands[index].Kind == render.OverlayCommand && frame.Commands[index].Count > 0 {
			t.Fatal("disabled eyes left background overlay geometry before applications")
		}
	}
}
