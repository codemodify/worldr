package workspace

import (
	"bytes"
	"math"
	"os"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/presentation"
	"github.com/codemodify/worldr/internal/render"
)

func dnaBackgroundCommand(t *testing.T, w *Workspace, frame render.Frame) (int, render.Command, render.Draw) {
	t.Helper()
	if w.backgroundScene == nil || w.backgroundScene.Node(w.dnaNode) == nil {
		t.Fatal("desktop has no retained DNA background")
	}
	geometry := w.backgroundScene.Node(w.dnaNode).Mesh.Geometry()
	for i, command := range frame.Commands {
		for _, draw := range command.Draws {
			if draw.Geometry == geometry {
				return i, command, draw
			}
		}
	}
	t.Fatal("visible background did not submit its retained geometry")
	return 0, render.Command{}, render.Draw{}
}

func TestDNABackgroundUsesSeparateEarlierSceneAndFixedCamera(t *testing.T) {
	w := desktop(t)
	apps := desktopApplications(t, w)
	frame := w.Draw(1440, 900)
	index, backdrop, draw := dnaBackgroundCommand(t, w, frame)
	if backdrop.Kind != render.SceneCommand || draw.Texture != nil || !w.backgroundScene.Node(w.dnaNode).Unpickable {
		t.Fatal("DNA did not remain unpickable retained background geometry")
	}
	if draw.Model[12] != 0 || draw.Model[13] != 0 || draw.Model[14] != 0 {
		t.Fatalf("DNA is not centered: translation=(%v, %v, %v)", draw.Model[12], draw.Model[13], draw.Model[14])
	}
	if draw.Model[4] != 0 || draw.Model[5] != 1 || draw.Model[6] != 0 {
		t.Fatalf("DNA long axis is not vertical: y-axis=(%v, %v, %v)", draw.Model[4], draw.Model[5], draw.Model[6])
	}
	appIndex := -1
	for i, command := range frame.Commands {
		for _, instance := range command.Draws {
			if instance.Texture == apps.surfaces[0].Texture {
				appIndex = i
			}
			if instance.Geometry == draw.Geometry && i != index {
				t.Fatal("DNA geometry leaked into the application's scene pass")
			}
		}
	}
	if appIndex <= index || frame.Commands[appIndex].Kind != render.SceneCommand || frame.Commands[appIndex].View.Viewport != backdrop.View.Viewport {
		t.Fatal("application pass did not follow the background with its own matching depth-clear viewport")
	}
	command(t, w, Action{Kind: OrbitCamera, DeltaX: 35, DeltaY: -15})
	command(t, w, Action{Kind: ZoomCamera, DeltaZoom: .2})
	_, movedCamera, movedDraw := dnaBackgroundCommand(t, w, w.Draw(1440, 900))
	if movedCamera.View != backdrop.View || movedDraw.Model != draw.Model || movedDraw.Geometry != draw.Geometry {
		t.Fatal("workspace camera motion changed backdrop framing, pose or geometry")
	}
	_, resized, resizedDraw := dnaBackgroundCommand(t, w, w.Draw(1000, 700))
	if resized.View.Viewport == backdrop.View.Viewport || resizedDraw.Model != draw.Model || resizedDraw.Geometry != draw.Geometry {
		t.Fatal("resize failed to update the backdrop viewport or rebuilt its model")
	}
	// The background remains purely decorative, and empty desktop space is no
	// longer an implicit camera control. Scene rotation belongs to the explicit
	// orbit pad so an ordinary workspace click cannot move every window.
	w.SetApplications(nil)
	w.Draw(1440, 900)
	x, y := w.viewport.X+w.viewport.Width*.65, w.viewport.Y+w.viewport.Height*.45
	before, history := w.Document(), w.historyPosition
	if pointer(w, experience.PointerDown, x, y) ||
		pointer(w, experience.PointerMove, x+80, y+25) ||
		pointer(w, experience.PointerUp, x+80, y+25) {
		t.Fatal("empty desktop space retained implicit orbit input")
	}
	if w.pointer.kind != captureNone || w.Document() != before || w.historyPosition != history {
		t.Fatal("dragging decorative background changed the camera or history")
	}
	study := study(t)
	if study.backgroundScene != nil || study.dnaNode != 0 {
		t.Fatal("AXIAL allocated the desktop-only background")
	}
}

func TestDNABackgroundTimingIsFrameIndependentAndBounded(t *testing.T) {
	fast, slow := desktop(t), desktop(t)
	_, _, initial := dnaBackgroundCommand(t, fast, fast.Draw(1440, 900))
	for i := 0; i < 40; i++ {
		fast.Update(25 * time.Millisecond)
		fast.Draw(1440, 900)
	}
	slow.Update(time.Second)
	_, _, fastDraw := dnaBackgroundCommand(t, fast, fast.Draw(1440, 900))
	_, _, slowDraw := dnaBackgroundCommand(t, slow, slow.Draw(1440, 900))
	if fast.backgroundPhase != slow.backgroundPhase || fastDraw.Model != slowDraw.Model || fastDraw.Model == initial.Model {
		t.Fatal("background pose depends on frame rate or failed to rotate")
	}
	phase := fast.backgroundPhase
	for i := 0; i < 5; i++ {
		fast.Draw(1440, 900)
	}
	fast.Update(0)
	fast.Update(-time.Second)
	if fast.backgroundPhase != phase {
		t.Fatal("draw-only, zero or negative updates advanced background time")
	}
	fast.Update(dnaRotationPeriod - phase)
	_, _, loop := dnaBackgroundCommand(t, fast, fast.Draw(1440, 900))
	if fast.backgroundPhase != 0 || loop.Model != initial.Model {
		t.Fatal("a full revolution failed to restore the exact initial rotation")
	}
	fast.Update(17 * time.Second)
	beforeInterruption := fast.backgroundPhase
	fast.Update(time.Duration(math.MaxInt64))
	want := (beforeInterruption + time.Duration(math.MaxInt64)%dnaRotationPeriod) % dnaRotationPeriod
	if fast.backgroundPhase != want || fast.backgroundPhase < 0 || fast.backgroundPhase >= dnaRotationPeriod {
		t.Fatal("a long interruption overflowed or lost the bounded rotation phase")
	}

}

func TestDNABackgroundMotionIsTransientAndReducedMotionFreezesPose(t *testing.T) {
	w := desktop(t)
	before := w.Document()
	state, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	w.Update(13 * time.Second)
	_, _, moving := dnaBackgroundCommand(t, w, w.Draw(1440, 900))
	updatedState, err := w.SaveState()
	if err != nil || !bytes.Equal(updatedState, state) || w.Document() != before || w.CanUndo() {
		t.Fatal("ambient rotation entered document state, persistence or undo history")
	}
	command(t, w, Action{Kind: OrbitCamera, DeltaX: 10})
	phase := w.backgroundPhase
	command(t, w, Action{Kind: Undo})
	if w.backgroundPhase != phase || w.Document() != before {
		t.Fatal("undo rewound ambient time or failed to restore the camera")
	}
	command(t, w, Action{Kind: SetReducedMotion, Enabled: true})
	w.Update(20 * time.Second)
	_, _, still := dnaBackgroundCommand(t, w, w.Draw(1440, 900))
	if w.backgroundPhase != phase || still.Model != moving.Model {
		t.Fatal("Reduced Motion failed to freeze the current background pose")
	}
	command(t, w, Action{Kind: SetReducedMotion, Enabled: false})
	w.Update(time.Second)
	if w.backgroundPhase != phase+time.Second {
		t.Fatal("resuming motion replayed paused time or restarted the rotation")
	}
	loaded := desktop(t)
	if err := loaded.LoadState(updatedState); err != nil || loaded.backgroundPhase != 0 || loaded.CanUndo() {
		t.Fatal("loading workspace state replayed the previous ambient clock/history")
	}
}

func TestDNABackgroundPresentationAndApplicationLifecycleRetainGeometry(t *testing.T) {
	w := desktop(t)
	apps := desktopApplications(t, w)
	_, _, original := dnaBackgroundCommand(t, w, w.Draw(1440, 900))
	command(t, w, Action{Kind: ToggleApplicationReading})
	_, _, cinematic := dnaBackgroundCommand(t, w, w.Draw(1440, 900))
	if cinematic.Geometry != original.Geometry || cinematic.Color[3] <= 0 {
		t.Fatal("Cinematic Read hid or rebuilt the backdrop")
	}
	command(t, w, Action{Kind: SetPresentation, Presentation: presentation.Adaptive})
	w.Update(time.Second)
	frame := w.Draw(1440, 900)
	for _, command := range frame.Commands {
		for _, draw := range command.Draws {
			if draw.Geometry == original.Geometry {
				t.Fatal("Adaptive Read retained the ambient DNA scene after its fade")
			}
		}
	}
	command(t, w, Action{Kind: ToggleApplicationReading})
	w.Update(time.Second)
	_, _, returned := dnaBackgroundCommand(t, w, w.Draw(1440, 900))
	if returned.Geometry != original.Geometry || returned.Color[3] <= 0 {
		t.Fatal("returning to space failed to restore retained background geometry")
	}
	apps.surfaces = nil
	w.Update(0)
	_, _, empty := dnaBackgroundCommand(t, w, w.Draw(1440, 900))
	if empty.Geometry != original.Geometry {
		t.Fatal("closing the last application recreated the desktop backdrop")
	}
	w.SetApplications(nil)
	_, _, cleared := dnaBackgroundCommand(t, w, w.Draw(1440, 900))
	if cleared.Geometry != original.Geometry {
		t.Fatal("replacing application providers invalidated background resources")
	}
}

func TestDNABackgroundLeavesOpaqueApplicationPixelsExactGPU(t *testing.T) {
	if os.Getenv("WORLDR_TEST_GPU") != "1" {
		t.Skip("set WORLDR_TEST_GPU=1 to render background/content isolation")
	}
	w := desktop(t)
	apps := desktopApplications(t, w)
	command(t, w, Action{Kind: MoveApplications, DeltaX: 3})
	texture := apps.surfaces[0].Texture
	tw, th := texture.Size()
	content := make([]byte, tw*th*4)
	for i := 0; i < len(content); i += 4 {
		content[i], content[i+1], content[i+2], content[i+3] = byte(i/4%127+30), 69, 131, 255
	}
	if err := texture.Replace(tw, th, content); err != nil {
		t.Fatal(err)
	}
	const width, height = 1440, 900
	vk, err := native.OpenVK(false, width, height)
	if err != nil {
		t.Fatal(err)
	}
	defer vk.Close()
	if err := vk.SetSceneAtlas(w.Atlas()); err != nil {
		t.Fatal(err)
	}
	renderFrame := func(background, app bool) []byte {
		t.Helper()
		frame := w.Draw(width, height)
		index, _, _ := dnaBackgroundCommand(t, w, frame)
		frame.Commands = append([]render.Command(nil), frame.Commands...)
		if !background {
			frame.Commands[index].Draws = nil
		}
		if !app {
			for i := range frame.Commands {
				var draws []render.Draw
				for _, draw := range frame.Commands[i].Draws {
					if draw.Texture != texture {
						draws = append(draws, draw)
					}
				}
				frame.Commands[i].Draws = draws
			}
		}
		pixels := make([]byte, width*height*4)
		if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
			t.Fatal(err)
		}
		return pixels
	}
	without, with := renderFrame(false, true), renderFrame(true, true)
	if bytes.Equal(without, with) {
		t.Fatal("fixture did not render a visible background outside the app")
	}
	backgroundOnly, blank := renderFrame(true, false), renderFrame(false, false)
	checked, overlap := 0, 0
	for row := 1; row < 20; row++ {
		for col := 1; col < 32; col++ {
			sx, sy := projectedApplication(w, apps.surfaces[0], float32(col)*float32(tw)/32, float32(row)*float32(th)/20)
			x, y := int(sx), int(sy)
			if x < 1 || x >= width-1 || y < 1 || y >= height-1 {
				continue
			}
			if hit, ok := w.applicationHit(sx, sy); !ok || hit.Node != w.applicationNodes[apps.surfaces[0].ID] {
				continue
			}
			at := (y*width + x) * 4
			if !bytes.Equal(with[at:at+4], without[at:at+4]) {
				t.Fatalf("background altered opaque application pixels at %d,%d", x, y)
			}
			checked++
			if !bytes.Equal(backgroundOnly[at:at+4], blank[at:at+4]) {
				overlap++
			}
		}
	}
	if checked < 100 || overlap == 0 {
		t.Fatalf("fixture did not exercise enough app pixels over DNA: checked=%d overlapping=%d", checked, overlap)
	}
	if repeated := renderFrame(true, true); !bytes.Equal(with, repeated) {
		t.Fatal("static background rendering was not deterministic")
	}
}
