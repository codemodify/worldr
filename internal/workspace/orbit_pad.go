package workspace

import (
	"fmt"
	"math"

	"github.com/codemodify/worldr/internal/experience"
)

// orbitPadBounds is deliberately separated from application content and the
// right-side launcher. It is the desktop's only pointer target for orbiting the
// whole spatial scene.
var orbitPadBounds = box{1162, 633, 172, 144}

func (w *Workspace) orbitPadVisible() bool {
	view := w.m.applicationState
	return w.desktop && !view.Reading && !view.Overview && !view.Placing
}

func (w *Workspace) drawOrbitPad() {
	if !w.orbitPadVisible() {
		return
	}
	b := orbitPadBounds
	strength := float32(.68 + .22*w.presentationBlend)
	w.rect(b.x, b.y, b.w, b.h, bg, .78)

	// Open corner rails keep the controller visually light while making its
	// complete hit region clear.
	const corner = float32(18)
	w.line(b.x, b.y, b.x+corner, b.y, 1.5, teal, strength)
	w.line(b.x, b.y, b.x, b.y+corner, 1.5, teal, strength)
	w.line(b.x+b.w-corner, b.y, b.x+b.w, b.y, 1.5, teal, strength)
	w.line(b.x+b.w, b.y, b.x+b.w, b.y+corner, 1.5, teal, strength)
	w.line(b.x, b.y+b.h-corner, b.x, b.y+b.h, 1.5, teal, strength)
	w.line(b.x, b.y+b.h, b.x+corner, b.y+b.h, 1.5, teal, strength)
	w.line(b.x+b.w-corner, b.y+b.h, b.x+b.w, b.y+b.h, 1.5, teal, strength)
	w.line(b.x+b.w, b.y+b.h-corner, b.x+b.w, b.y+b.h, 1.5, teal, strength)
	w.text(b.x+10, b.y+9, 9, "SCENE ROTATION", teal, .95)
	w.text(b.x+110, b.y+9, 8, "DRAG", muted, .9)

	cx, cy, radius := b.x+51, b.y+81, float32(37)
	w.circle(cx, cy, radius, 1, teal, .48)
	w.circle(cx, cy, radius-6, .6, teal, .2)
	for _, offset := range []float32{-24, -12, 0, 12, 24} {
		chord := float32(math.Sqrt(float64(radius*radius - offset*offset)))
		w.line(cx-chord, cy+offset, cx+chord, cy+offset, .55, teal, .2)
		w.line(cx+offset, cy-chord, cx+offset, cy+chord, .55, teal, .2)
	}
	for i := 0; i < 24; i++ {
		a := float64(i) * 2 * math.Pi / 24
		length := float32(3)
		if i%3 == 0 {
			length = 6
		}
		cosine, sine := float32(math.Cos(a)), float32(math.Sin(a))
		w.line(cx+cosine*(radius-1), cy+sine*(radius-1), cx+cosine*(radius+length), cy+sine*(radius+length), .7, teal, .36)
	}

	// The reticle mirrors the current camera angles instead of acting as a
	// decorative animation, so the controller doubles as an orientation readout.
	needleX := cx + float32(math.Sin(float64(w.m.yaw)))*radius*.64
	needleY := cy - float32(math.Sin(float64(w.m.pitch)))*radius*.64
	w.line(cx, cy, needleX, needleY, 1.4, amber, .9)
	w.circle(needleX, needleY, 2.5, 2.5, amber, .95)
	w.circle(cx, cy, 2, 2, teal, .9)

	yaw := float64(w.m.yaw) * 180 / math.Pi
	pitch := float64(w.m.pitch) * 180 / math.Pi
	w.text(b.x+101, b.y+49, 8, "YAW", muted, .9)
	w.text(b.x+101, b.y+64, 11, fmt.Sprintf("%+04.0f°", yaw), ink, 1)
	w.text(b.x+101, b.y+88, 8, "PITCH", muted, .9)
	w.text(b.x+101, b.y+103, 11, fmt.Sprintf("%+04.0f°", pitch), ink, 1)
	w.line(b.x+98, b.y+43, b.x+98, b.y+119, .7, teal, .2)
}

func (w *Workspace) handleOrbitPad(event experience.Event) bool {
	if w.pointer.kind == captureOrbitPad {
		switch event.Kind {
		case experience.PointerMove:
			w.movePointer(event.X, event.Y)
			return true
		case experience.PointerUp:
			if applicationButton(event) != 272 {
				return true
			}
			w.movePointer(event.X, event.Y)
			w.commitPointer()
			return true
		case experience.PointerDown, experience.PointerScroll:
			return true
		case experience.PointerCancel, experience.KeyboardCancel:
			return w.cancelPointer()
		}
	}
	if !w.orbitPadVisible() || w.portals.open || w.portals.pressed != -1 {
		return false
	}
	// Existing client and window gestures keep their grabs even when the pointer
	// crosses the fixed controller.
	if w.pointer.kind != captureNone || len(w.applicationButtons) != 0 || len(w.windowDragButtons) != 0 {
		return false
	}
	x, y := (event.X-w.ox)/w.scale, (event.Y-w.oy)/w.scale
	inside := orbitPadBounds.contains(x, y)
	switch event.Kind {
	case experience.PointerMove:
		if inside {
			w.clearApplicationHover()
		}
		return inside
	case experience.PointerScroll:
		return inside
	case experience.PointerDown:
		if !inside {
			return false
		}
		w.clearApplicationFocus()
		if applicationButton(event) == 272 {
			w.pointer = pointerCapture{
				kind: captureOrbitPad, start: w.Document(),
				pressX: x, pressY: y, lastX: x, lastY: y,
				scale: w.scale, ox: w.ox, oy: w.oy,
			}
		}
		return true
	case experience.PointerUp:
		return inside
	}
	return false
}
