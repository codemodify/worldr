package workspace

import (
	"math"

	"github.com/codemodify/worldr/internal/experience"
)

// The scene controller continues the launcher rail at the lower-right edge.
// Its small reticle is the desktop's only pointer target for orbiting the
// spatial scene; the adjacent gear and Reset controls own separate strokes.
var (
	orbitPadBounds      = box{1364, 796, 60, 92}
	orbitPadDragBounds  = box{1369, 824, 50, 58}
	orbitSettingsButton = box{1401, 802, 18, 18}
	desktopResetButton  = box{1369, 802, 18, 18}
)

type orbitControlTarget uint8

const (
	orbitControlNone orbitControlTarget = iota
	orbitControlSettings
	orbitControlReset
)

type orbitControlPointer struct {
	active, dragged bool
	target          orbitControlTarget
	x, y            float32
	width, height   int
}

func (w *Workspace) orbitPadVisible() bool { return w.desktop && !w.skinDesktopChromeVisible() }

func (w *Workspace) orbitPadCanOrbit() bool {
	view := w.m.applicationState
	return w.desktop && !view.Reading && !view.Overview && !view.Placing
}

func (w *Workspace) cancelOrbitControl() { w.orbitControl = orbitControlPointer{} }

func orbitControlAt(x, y float32) orbitControlTarget {
	switch {
	case orbitSettingsButton.contains(x, y):
		return orbitControlSettings
	case desktopResetButton.contains(x, y):
		return orbitControlReset
	default:
		return orbitControlNone
	}
}

func (w *Workspace) drawOrbitPad() {
	if !w.orbitPadVisible() {
		return
	}
	b := orbitPadBounds
	w.rect(b.x, b.y, b.w, b.h, bg, .84)

	// Open rails align this compact controller with the launcher without adding
	// a title or field labels.
	const corner = float32(10)
	for _, edge := range [][4]float32{
		{b.x, b.y, b.x + corner, b.y}, {b.x, b.y, b.x, b.y + corner},
		{b.x + b.w - corner, b.y, b.x + b.w, b.y}, {b.x + b.w, b.y, b.x + b.w, b.y + corner},
		{b.x, b.y + b.h - corner, b.x, b.y + b.h}, {b.x, b.y + b.h, b.x + corner, b.y + b.h},
		{b.x + b.w - corner, b.y + b.h, b.x + b.w, b.y + b.h}, {b.x + b.w, b.y + b.h - corner, b.x + b.w, b.y + b.h},
	} {
		w.line(edge[0], edge[1], edge[2], edge[3], 1.25, teal, .72)
	}

	cx := orbitPadDragBounds.x + orbitPadDragBounds.w/2
	cy := orbitPadDragBounds.y + orbitPadDragBounds.h/2
	radius := float32(18)
	strength := float32(1)
	if !w.orbitPadCanOrbit() {
		strength = .32
	}
	w.circle(cx, cy, radius, .9, teal, .52*strength)
	w.circle(cx, cy, radius-5, .5, teal, .2*strength)
	for _, offset := range []float32{-10, 0, 10} {
		chord := float32(math.Sqrt(float64(radius*radius - offset*offset)))
		w.line(cx-chord, cy+offset, cx+chord, cy+offset, .45, teal, .18*strength)
		w.line(cx+offset, cy-chord, cx+offset, cy+chord, .45, teal, .18*strength)
	}
	needleX := cx + float32(math.Sin(float64(w.m.yaw)))*radius*.65
	needleY := cy - float32(math.Sin(float64(w.m.pitch)))*radius*.65
	w.line(cx, cy, needleX, needleY, 1.15, amber, .92*strength)
	w.circle(needleX, needleY, 1.8, 1.8, amber, .95*strength)
	w.circle(cx, cy, 1.4, 1.4, teal, .95*strength)

	gx, gy := orbitSettingsButton.x+orbitSettingsButton.w/2, orbitSettingsButton.y+orbitSettingsButton.h/2
	w.circle(gx, gy, 5.4, 1.1, teal, .82)
	w.circle(gx, gy, 2.0, 1.0, teal, .9)
	for i := 0; i < 8; i++ {
		a := float64(i) * math.Pi / 4
		c, s := float32(math.Cos(a)), float32(math.Sin(a))
		w.line(gx+c*6.2, gy+s*6.2, gx+c*8, gy+s*8, 1.2, teal, .78)
	}

	rx := desktopResetButton.x + desktopResetButton.w/2
	ry := desktopResetButton.y + desktopResetButton.h/2
	w.line(rx-4, ry-4, rx+4, ry+4, 1.4, teal, .95)
	w.line(rx-4, ry+4, rx+4, ry-4, 1.4, teal, .95)
}

func (w *Workspace) handleOrbitPad(event experience.Event) bool {
	if w.orbitControl.active {
		if event.Kind == experience.PointerCancel || event.Kind == experience.KeyboardCancel || w.width != w.orbitControl.width || w.height != w.orbitControl.height {
			w.cancelOrbitControl()
			return true
		}
		x, y := (event.X-w.ox)/w.scale, (event.Y-w.oy)/w.scale
		switch event.Kind {
		case experience.PointerMove, experience.PointerUp:
			if abs(x-w.orbitControl.x)+abs(y-w.orbitControl.y) > 4 {
				w.orbitControl.dragged = true
			}
			if event.Kind == experience.PointerUp && applicationButton(event) == 272 {
				target := w.orbitControl.target
				activate := !w.orbitControl.dragged && orbitControlAt(x, y) == target
				w.cancelOrbitControl()
				if activate {
					switch target {
					case orbitControlSettings:
						w.openSettings()
					case orbitControlReset:
						_ = w.Dispatch(Action{Kind: ResetView})
					}
				}
			}
		}
		return true
	}
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
	if !w.orbitPadVisible() {
		return false
	}
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
		w.resetApplicationReadClick()
		w.clearApplicationFocus()
		if applicationButton(event) != 272 {
			return true
		}
		if target := orbitControlAt(x, y); target != orbitControlNone {
			w.orbitControl = orbitControlPointer{active: true, target: target, x: x, y: y, width: w.width, height: w.height}
			return true
		}
		if orbitPadDragBounds.contains(x, y) && w.orbitPadCanOrbit() {
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
