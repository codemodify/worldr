package workspace

import (
	"math"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
)

// ambientPointerState passively remembers the host pointer in framebuffer
// coordinates. It never participates in routing or capture; background
// ornaments can therefore react to the same motion that reaches an application.
type ambientPointerState struct {
	x, y  float32
	valid bool
}

func (p *ambientPointerState) Observe(event experience.Event) {
	switch event.Kind {
	case experience.PointerCancel:
		*p = ambientPointerState{}
	case experience.PointerMove, experience.PointerDown, experience.PointerUp, experience.PointerScroll:
		if !finite(float64(event.X)) || !finite(float64(event.Y)) {
			p.valid = false
			return
		}
		p.x, p.y, p.valid = event.X, event.Y, true
	}
}

func (p ambientPointerState) designPoint(ox, oy, scale float32) (float32, float32, bool) {
	if !p.valid || !finite(float64(scale)) || scale <= 0 {
		return 0, 0, false
	}
	x, y := (p.x-ox)/scale, (p.y-oy)/scale
	if !finite(float64(x)) || !finite(float64(y)) {
		return 0, 0, false
	}
	return x, y, true
}

type backgroundEye struct {
	x, y, radius float32
	veinPhase    float32
}

var backgroundEyes = [...]backgroundEye{
	{x: 165, y: 157, radius: 36, veinPhase: .17},
	{x: 247, y: 124, radius: 29, veinPhase: .73},
	{x: 329, y: 166, radius: 40, veinPhase: 1.31},
}

// eyeGazePoint keeps the iris inside its sclera while retaining fine motion
// when the pointer is near the eye. Far targets asymptotically reach the rim.
func eyeGazePoint(cx, cy, radius, targetX, targetY float32) (float32, float32) {
	if radius <= 0 || !finite(float64(cx)) || !finite(float64(cy)) ||
		!finite(float64(radius)) || !finite(float64(targetX)) || !finite(float64(targetY)) {
		return cx, cy
	}
	dx, dy := targetX-cx, targetY-cy
	distance := float32(math.Hypot(float64(dx), float64(dy)))
	if distance <= 1e-5 {
		return cx, cy
	}
	maximum := radius * .34
	travel := maximum * min(float32(1), distance/(radius*3.2))
	return cx + dx/distance*travel, cy + dy/distance*travel
}

func (w *Workspace) drawBackgroundEyes() {
	if !w.desktop || !w.environment.Eyes || w.canvas == nil || w.scale <= 0 {
		return
	}
	targetX, targetY, tracking := w.ambientPointer.designPoint(w.ox, w.oy, w.scale)
	for index, eye := range backgroundEyes {
		irisX, irisY := eye.x, eye.y
		if tracking {
			irisX, irisY = eyeGazePoint(eye.x, eye.y, eye.radius, targetX, targetY)
		}
		w.drawBloodshotEye(eye, irisX, irisY, index)
	}
}

func (w *Workspace) drawBloodshotEye(eye backgroundEye, irisX, irisY float32, index int) {
	cx, cy, radius := eye.x, eye.y, eye.radius

	// A dark halo seats each eye in the wall instead of making it look like
	// flat chrome. The translucent sclera leaves a trace of the living net.
	w.circle(cx+1.4, cy+2.1, radius+5, 0, 0x02050a, .55)
	w.circle(cx, cy, radius+2.2, 1.4, 0x8d172b, .52)
	w.circle(cx, cy, radius, 0, 0xcbd7da, .72)
	w.circle(cx-radius*.15, cy-radius*.19, radius*.72, 0, 0xf4f7f4, .12)

	// Fixed branching paths make the blood vessels stable across frames. Every
	// point stays between the iris and rim, so no clipping surface is needed.
	for vein := 0; vein < 9; vein++ {
		angle := float64(eye.veinPhase) + 2*math.Pi*float64(vein)/9
		bend := .15 * math.Sin(float64((vein+1)*(index+2)))
		outerX := cx + radius*.9*float32(math.Cos(angle))
		outerY := cy + radius*.9*float32(math.Sin(angle))
		middleAngle := angle + bend
		middleX := cx + radius*.68*float32(math.Cos(middleAngle))
		middleY := cy + radius*.68*float32(math.Sin(middleAngle))
		innerAngle := angle - bend*.55
		innerX := cx + radius*.49*float32(math.Cos(innerAngle))
		innerY := cy + radius*.49*float32(math.Sin(innerAngle))
		w.line(outerX, outerY, middleX, middleY, .72, 0xb71f38, .7)
		w.line(middleX, middleY, innerX, innerY, .58, 0xda3048, .62)
		branchAngle := middleAngle + .45*float64(1-2*(vein&1))
		branchX := middleX + radius*.16*float32(math.Cos(branchAngle))
		branchY := middleY + radius*.16*float32(math.Sin(branchAngle))
		w.line(middleX, middleY, branchX, branchY, .48, 0x9f1931, .55)
	}

	// The iris carries the workspace cyan while a thin red ring ties it into the
	// vascular sclera. All coordinates remain in design space so display scaling
	// cannot alter the gaze direction.
	irisRadius := radius * .31
	w.circle(irisX, irisY, irisRadius+1.5, 0, 0x711426, .78)
	w.circle(irisX, irisY, irisRadius, 0, 0x245a68, .96)
	w.circle(irisX, irisY, irisRadius*.73, 1.2, 0x65d8e8, .82)
	w.circle(irisX, irisY, radius*.135, 0, 0x01050a, .98)
	w.circle(irisX-radius*.075, irisY-radius*.08, radius*.055, 0, 0xeafcff, .9)

	// Small broken eyelid arcs keep the round MATE-like silhouette while making
	// the ornament belong to worldr's segmented visual language.
	scale := w.scale
	sx, sy := w.ox+cx*scale, w.oy+cy*scale
	w.canvas.Arc(sx, sy, (radius+3)*scale, float32(math.Pi*.12), float32(math.Pi*.88), 1.5*scale, scene.ColorHex(0xd65d69, .42))
	w.canvas.Arc(sx, sy, (radius+3)*scale, float32(math.Pi*1.12), float32(math.Pi*1.88), 1.5*scale, scene.ColorHex(0x5caec2, .3))
}
