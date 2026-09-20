package workspace

import (
	"math"
	"sort"
	"time"

	"github.com/codemodify/worldr/internal/scene"
)

const (
	energyNetColumns     = 25
	energyNetRows        = 15
	energyNetAspect      = float32(1.64)
	energyNetStep        = time.Second / 120
	energyNetSettleTicks = 8 * 120

	// Application Depth points toward the initial camera. The woven wall is a
	// rear barrier, and the small clearance keeps the back of a window in front
	// of its visible strands.
	energyWallDepth       = float32(-3.6)
	energyWindowClearance = float32(.18)
	energyWallRestitution = float32(.46)
)

type energyNetPoint struct {
	rest, position, velocity scene.Vec3
	pinned                   bool
}

// energyNet is a small deterministic mass-spring wall. Its state is ambient:
// it is never serialized or added to undo history. Fixed integration steps make
// ordinary frame-rate changes produce the same motion while a long suspension
// safely settles the wall instead of feeding an unbounded catch-up loop.
type energyNet struct {
	points      [energyNetColumns * energyNetRows]energyNetPoint
	forces      [energyNetColumns * energyNetRows]scene.Vec3
	accumulator time.Duration
	phase       time.Duration
	activeTicks int
	impactCount uint64
}

type energyWallImpact struct {
	at             time.Duration
	u, v, strength float32
}

func newEnergyNet() *energyNet {
	n := &energyNet{}
	for row := 0; row < energyNetRows; row++ {
		v := float32(row) / float32(energyNetRows-1)
		for column := 0; column < energyNetColumns; column++ {
			u := float32(column) / float32(energyNetColumns-1)
			// A shallow perspective sweep gives the regular weave the soft,
			// draped silhouette of the reference while retaining a real lattice.
			x := energyNetAspect*u + .22*(v-.5) + .026*float32(math.Sin(float64(math.Pi*u)))*float32(math.Sin(float64(2*math.Pi*v)))
			y := v + .062*(u-.5)*(u-.5) - .0155 + .012*float32(math.Sin(float64(2*math.Pi*u)))
			point := &n.points[n.index(column, row)]
			point.rest = scene.Vec3{X: x, Y: y}
			point.position = point.rest
			point.pinned = column == 0 || row == 0 || column == energyNetColumns-1 || row == energyNetRows-1
		}
	}
	return n
}

func (n *energyNet) index(column, row int) int { return row*energyNetColumns + column }

func (n *energyNet) settleMotion() {
	for i := range n.points {
		n.points[i].position = n.points[i].rest
		n.points[i].velocity = scene.Vec3{}
	}
	n.activeTicks = 0
}

func (n *energyNet) update(dt time.Duration) {
	if n == nil || dt <= 0 {
		return
	}
	const cycle = 12 * time.Second
	n.phase = (n.phase + dt%cycle) % cycle
	// Split without adding the full duration to the accumulator: even a maximum
	// time.Duration update stays bounded and cannot overflow transient state.
	steps := dt / energyNetStep
	remainder := dt % energyNetStep
	n.accumulator += remainder
	if n.accumulator >= energyNetStep {
		steps++
		n.accumulator -= energyNetStep
	}
	if n.activeTicks == 0 {
		return
	}
	if steps >= time.Duration(n.activeTicks) {
		// The damped response has a finite real-time lifetime. Resetting at that
		// exact tick makes a long suspension and many small frames agree while
		// bounding catch-up work.
		n.settleMotion()
		return
	}
	for ; steps > 0; steps-- {
		n.integrate(float32(energyNetStep.Seconds()))
		n.activeTicks--
	}
}

type energyNetSpring struct {
	dx, dy    int
	stiffness float32
}

var energyNetSprings = [...]energyNetSpring{
	{1, 0, 72}, {0, 1, 72}, // invisible structural bracing
	{1, 1, 34}, {-1, 1, 34}, // the two visible diagonal fiber families
	{2, 0, 13}, {0, 2, 13}, // bending resistance prevents sharp folds
}

func (n *energyNet) integrate(dt float32) {
	for i := range n.forces {
		n.forces[i] = scene.Vec3{}
	}
	for row := 0; row < energyNetRows; row++ {
		for column := 0; column < energyNetColumns; column++ {
			a := n.index(column, row)
			for _, spring := range energyNetSprings {
				otherColumn, otherRow := column+spring.dx, row+spring.dy
				if otherColumn < 0 || otherColumn >= energyNetColumns || otherRow < 0 || otherRow >= energyNetRows {
					continue
				}
				b := n.index(otherColumn, otherRow)
				restDelta := n.points[b].rest.Sub(n.points[a].rest)
				delta := n.points[b].position.Sub(n.points[a].position)
				length := delta.Length()
				if length < 1e-6 {
					continue
				}
				direction := delta.Mul(1 / length)
				relativeSpeed := n.points[b].velocity.Sub(n.points[a].velocity).Dot(direction)
				force := direction.Mul(spring.stiffness*(length-restDelta.Length()) + 1.15*relativeSpeed)
				n.forces[a] = n.forces[a].Add(force)
				n.forces[b] = n.forces[b].Sub(force)
			}
		}
	}
	damping := float32(math.Exp(-2.35 * float64(dt)))
	for row := 0; row < energyNetRows; row++ {
		for column := 0; column < energyNetColumns; column++ {
			i := n.index(column, row)
			point := &n.points[i]
			if point.pinned {
				point.position, point.velocity = point.rest, scene.Vec3{}
				continue
			}
			point.velocity = point.velocity.Add(n.forces[i].Mul(dt)).Mul(damping)
			point.position = point.position.Add(point.velocity.Mul(dt))
			// Numerical guardrails are well outside authored motion, but keep a bad
			// device stall or extreme impact from destabilizing the whole desktop.
			delta := point.position.Sub(point.rest)
			if length := delta.Length(); length > .42 {
				point.position = point.rest.Add(delta.Mul(.42 / length))
				point.velocity = point.velocity.Mul(.35)
			}
		}
	}
}

func (n *energyNet) impact(u, v, strength float32) {
	if n == nil || !finite(float64(u)) || !finite(float64(v)) || !finite(float64(strength)) || strength <= 0 {
		return
	}
	u, v = max(float32(.03), min(float32(.97), u)), max(float32(.03), min(float32(.97), v))
	n.impactCount++
	n.activeTicks = energyNetSettleTicks
	for row := 1; row < energyNetRows-1; row++ {
		for column := 1; column < energyNetColumns-1; column++ {
			i := n.index(column, row)
			point := &n.points[i]
			pu, pv := point.rest.X/energyNetAspect, point.rest.Y
			dx, dy := pu-u, pv-v
			distance2 := dx*dx + dy*dy
			weight := float32(math.Exp(float64(-distance2 / .018)))
			if weight < .002 {
				continue
			}
			magnitude := min(float32(1.8), strength) * weight
			point.velocity.Z -= 1.25 * magnitude
			if distance2 > 1e-6 {
				inverse := 1 / float32(math.Sqrt(float64(distance2)))
				point.velocity.X += dx * inverse * .075 * magnitude
				point.velocity.Y += dy * inverse * .075 * magnitude
			}
		}
	}
}

func energyPulse(position, phase float64) float32 {
	// Three packets with different spacing keep electricity moving without
	// making every strand pulse in unison.
	result := 0.0
	for _, offset := range [...]float64{0, .37, .71} {
		d := math.Mod(position-phase-offset+4, 1)
		if d > .5 {
			d = 1 - d
		}
		result += math.Exp(-d * d / .0016)
	}
	return float32(min(1.0, result))
}

func (n *energyNet) screenPoint(index int, viewport scene.Viewport) (float32, float32) {
	point := n.points[index].position
	u, v := point.X/energyNetAspect, point.Y
	// Normal displacement is projected around a stable center. Each node's local
	// Z still forms a visible dent, while a later impact elsewhere cannot teleport
	// deformation left by an earlier collision.
	phase := float64(n.phase) / float64(12*time.Second)
	envelope := float32(math.Sin(float64(math.Pi*u)) * math.Sin(float64(math.Pi*v)))
	breath := .012 * envelope * float32(math.Sin(2*math.Pi*(2*phase+float64(u)*.7+float64(v)*.4)))
	scale := 1 + (point.Z+breath)*.22
	u = .5 + (u-.5)*scale
	v = .5 + (v-.5)*scale
	// The photographed weave is a loose sheet rather than a framed Cartesian
	// plane. Let cell size grow slightly toward the lower foreground, add a tiny
	// deterministic fiber irregularity, and extend the pinned edge beyond the
	// viewport so no rectangular perimeter is visible.
	row, column := index/energyNetColumns, index%energyNetColumns
	boundedU := min(float32(1), max(float32(0), u))
	boundedV := min(float32(1), max(float32(0), v))
	envelope *= float32(math.Sin(float64(math.Pi * boundedU)))
	// A broad convex drape and stronger foreground expansion break the uniform
	// drafting-plane spacing. The upper sheet recedes while the lower cells open
	// toward the viewer, like the close macro perspective of the reference.
	u += .034 * envelope * float32(math.Sin(float64(2*math.Pi*boundedV+.35)))
	v += .044*envelope + .01*float32(math.Sin(float64(2*math.Pi*boundedU+.6)))*float32(math.Sin(float64(math.Pi*boundedV)))
	u = .5 + (u-.5)*(.8+.32*boundedV)
	v = v * (.78 + .22*boundedV)
	u += envelope * (.0024*float32(math.Sin(float64(column)*.53+float64(row)*.31+.4)) +
		.0011*float32(math.Sin(float64(column)*1.37-float64(row)*.47)))
	v += envelope * (.0021*float32(math.Sin(float64(column)*.29-float64(row)*.43+1.2)) +
		.0009*float32(math.Sin(float64(column)*1.11+float64(row)*.61)))
	u = .5 + (u-.5)*1.13
	v = .5 + (v-.5)*1.18
	// Oblique camera framing turns the simulated rectangular brace into the
	// skewed lozenge weave seen in the macro reference. Vertical compression and
	// rotation happen in pixels so the material keeps the same inclination on
	// different display aspects.
	px, py := (u-.5)*viewport.Width, (v-.5)*viewport.Height*.82
	const cosine, sine = float32(.9781476), float32(.2079117) // twelve degrees
	return viewport.X + viewport.Width*.5 + cosine*px + sine*py,
		viewport.Y + viewport.Height*.5 - sine*px + cosine*py
}

type energyNetCanvasPoint struct{ x, y float32 }

type energyNetProjection struct {
	points [energyNetColumns * energyNetRows]energyNetCanvasPoint
}

func (n *energyNet) projection(viewport scene.Viewport) energyNetProjection {
	var projected energyNetProjection
	for index := range projected.points {
		x, y := n.screenPoint(index, viewport)
		projected.points[index] = energyNetCanvasPoint{x, y}
	}
	return projected
}

func energyNetBezier(a, control, b energyNetCanvasPoint, t float32) energyNetCanvasPoint {
	one := 1 - t
	return energyNetCanvasPoint{
		x: one*one*a.x + 2*one*t*control.x + t*t*b.x,
		y: one*one*a.y + 2*one*t*control.y + t*t*b.y,
	}
}

func energyNetVariation(a, b int) float32 {
	return .5 + .5*float32(math.Sin(float64(a*73+b*151)+.83))
}

func (w *Workspace) drawEnergyNetFiber(a, control, b energyNetCanvasPoint, path0, path1, phase float64, light, variation float32, over bool) {
	margin := 9 * w.scale
	canvasWidth, canvasHeight := w.canvas.Size()
	if max(a.x, max(control.x, b.x)) < -margin ||
		min(a.x, min(control.x, b.x)) > float32(canvasWidth)+margin ||
		max(a.y, max(control.y, b.y)) < -margin ||
		min(a.y, min(control.y, b.y)) > float32(canvasHeight)+margin {
		return
	}
	// Near fibers gain weight as the sheet rolls toward the viewer. One shaded
	// stroke carries the teal body and pale specular rim together, which looks
	// rounder than two uniformly colored lines laid on top of each other.
	width := (3.75 + 1.35*(1-light) + .72*variation) * w.scale
	if over {
		// Erase only a short span of the lower fiber. Keeping the shadow local to
		// the crossing preserves the translucent polymer body elsewhere.
		underStart, underEnd := energyNetBezier(a, control, b, .42), energyNetBezier(a, control, b, .58)
		w.canvas.Line(underStart.x, underStart.y, underEnd.x, underEnd.y, width+2.5*w.scale, scene.ColorHex(0x01070b, .61))
	}
	const pieces = 3
	for piece := 0; piece < pieces; piece++ {
		t0, t1 := float32(piece)/pieces, float32(piece+1)/pieces
		start, end := energyNetBezier(a, control, b, t0), energyNetBezier(a, control, b, t1)
		dx, dy := end.x-start.x, end.y-start.y
		length := float32(math.Hypot(float64(dx), float64(dy)))
		if length <= 1e-5 {
			continue
		}
		nx, ny := -dy/length, dx/length
		pulse := energyPulse(path0+(path1-path0)*float64(t0+t1)/2, phase)
		dark := scene.ColorHex(0x105360, .22+.2*light+.08*pulse)
		lit := scene.ColorHex(0x8ed8d9, .14+.5*light+.22*pulse)
		// Electricity changes the material's own specular color instead of drawing
		// ruler-like bars over it.
		electric := scene.ColorHex(0xe5ffff, lit.A)
		blend := .68 * pulse
		lit.R += (electric.R - lit.R) * blend
		lit.G += (electric.G - lit.G) * blend
		lit.B += (electric.B - lit.B) * blend
		if nx+ny < 0 { // the +normal side points toward the upper-left key light
			w.canvas.ShadedLine(start.x, start.y, end.x, end.y, width, dark, lit)
		} else {
			w.canvas.ShadedLine(start.x, start.y, end.x, end.y, width, lit, dark)
		}
		// An occasional hairline makes a strand read as bundled polymer rather than
		// a perfectly extruded vector stroke.
		if variation > .45 && piece == int(variation*17)%pieces {
			hairOffset := (width/w.scale*.34 + .32*variation) * w.scale
			w.canvas.Line(start.x+nx*hairOffset, start.y+ny*hairOffset, end.x+nx*hairOffset, end.y+ny*hairOffset, .5*w.scale, scene.ColorHex(0xa1e6e3, .18+.19*light))
		}
		if variation < .2 && piece == (int(variation*29)+1)%pieces {
			hairOffset := (width/w.scale*.3 + .22) * w.scale
			w.canvas.Line(start.x-nx*hairOffset, start.y-ny*hairOffset, end.x-nx*hairOffset, end.y-ny*hairOffset, .42*w.scale, scene.ColorHex(0x073943, .22))
		}
	}
}

func (p *energyNetProjection) gridPoint(column, row int) energyNetCanvasPoint {
	if column < 0 {
		p0, p1 := p.gridPoint(0, row), p.gridPoint(1, row)
		amount := float32(column)
		return energyNetCanvasPoint{p0.x + (p1.x-p0.x)*amount, p0.y + (p1.y-p0.y)*amount}
	}
	if column >= energyNetColumns {
		p0 := p.gridPoint(energyNetColumns-1, row)
		p1 := p.gridPoint(energyNetColumns-2, row)
		amount := float32(column - (energyNetColumns - 1))
		return energyNetCanvasPoint{p0.x + (p0.x-p1.x)*amount, p0.y + (p0.y-p1.y)*amount}
	}
	if row >= 0 && row < energyNetRows {
		return p.points[row*energyNetColumns+column]
	}
	// Continue beyond the pinned simulation boundary. These extrapolated fibers
	// never participate in physics; they only keep the photographed sheet flowing
	// beyond the crop after the oblique camera transform.
	if row < 0 {
		p0, p1 := p.points[column], p.points[energyNetColumns+column]
		amount := float32(row)
		return energyNetCanvasPoint{p0.x + (p1.x-p0.x)*amount, p0.y + (p1.y-p0.y)*amount}
	}
	p0 := p.points[(energyNetRows-1)*energyNetColumns+column]
	p1 := p.points[(energyNetRows-2)*energyNetColumns+column]
	amount := float32(row - (energyNetRows - 1))
	return energyNetCanvasPoint{p0.x + (p0.x-p1.x)*amount, p0.y + (p0.y-p1.y)*amount}
}

func energyNetFiberLight(column, row float32) float32 {
	u := column / float32(energyNetColumns-1)
	v := row / float32(energyNetRows-1)
	vertical := 1 - .66*min(float32(1), max(float32(0), v))
	center := 1 - .34*min(float32(1), float32(math.Abs(float64(u-.48)))*1.7)
	return min(float32(1), max(float32(.12), .1+vertical*center*.9))
}

func (w *Workspace) drawEnergyNet() {
	n := w.energyNet
	if n == nil || w.viewport.Width <= 0 || w.viewport.Height <= 0 {
		return
	}
	phase := float64(n.phase) / float64(12*time.Second)
	// A restrained cyan pool behind the fibers supplies the photographic depth
	// cue visible through the apertures without turning the wall into neon UI.
	radius := max(w.viewport.Width, w.viewport.Height) * .72
	w.canvas.RadialGradient(w.viewport.X+w.viewport.Width*.46, w.viewport.Y+w.viewport.Height*.2, radius,
		scene.ColorHex(0x195a68, .055), scene.ColorHex(0x07121a, 0))
	projection := n.projection(w.viewport)
	const overscanRows, overscanColumns = 3, 3
	for row := -overscanRows; row < energyNetRows-1+overscanRows; row++ {
		for column := -overscanColumns; column < energyNetColumns-1+overscanColumns; column++ {
			tl, tr := projection.gridPoint(column, row), projection.gridPoint(column+1, row)
			bl, br := projection.gridPoint(column, row+1), projection.gridPoint(column+1, row+1)
			seed := (row+overscanRows)*(energyNetColumns+2*overscanColumns) + column + overscanColumns
			jitter := energyNetVariation(seed, seed+energyNetColumns+1)
			waveX := .64*float32(math.Sin(float64(column)*.23+float64(row)*.31+.4)) +
				.36*float32(math.Sin(float64(column)*.67-float64(row)*.19+1.1))
			waveY := .61*float32(math.Sin(float64(column)*.17-float64(row)*.29+1.2)) +
				.39*float32(math.Sin(float64(column)*.61+float64(row)*.23+.2))
			cross := energyNetCanvasPoint{
				x: (tl.x+tr.x+bl.x+br.x)/4 + (waveX*5.2+(jitter-.5)*2.6)*w.scale,
				y: (tl.y+tr.y+bl.y+br.y)/4 + (waveY*4.1+(energyNetVariation(seed+1, seed+energyNetColumns)-.5)*2.1)*w.scale,
			}
			descStart, descEnd := tl, br
			ascStart, ascEnd := tr, bl
			descControl := energyNetCanvasPoint{2*cross.x - (descStart.x+descEnd.x)/2, 2*cross.y - (descStart.y+descEnd.y)/2}
			ascControl := energyNetCanvasPoint{2*cross.x - (ascStart.x+ascEnd.x)/2, 2*cross.y - (ascStart.y+ascEnd.y)/2}
			descPath0 := float64(row)/float64(energyNetRows-1) + float64(column-row)*.043
			descPath1 := float64(row+1)/float64(energyNetRows-1) + float64(column-row)*.043
			ascPath0 := float64(row)/float64(energyNetRows-1) + float64(column+row+1)*.037
			ascPath1 := float64(row+1)/float64(energyNetRows-1) + float64(column+row+1)*.037
			drawDescending := func(over bool) {
				w.drawEnergyNetFiber(descStart, descControl, descEnd, descPath0, descPath1, phase*2.55,
					energyNetFiberLight(float32(column)+.5, float32(row)+.5), energyNetVariation(seed, seed+energyNetColumns+1), over)
			}
			drawAscending := func(over bool) {
				w.drawEnergyNetFiber(ascStart, ascControl, ascEnd, ascPath0, ascPath1, .21-phase*2.15,
					energyNetFiberLight(float32(column)+.5, float32(row)+.5), energyNetVariation(seed+1, seed+energyNetColumns), over)
			}
			// Alternating draw order makes the broad dark seat of the later strand
			// cut a convincing underpass into the earlier one at each crossing.
			if (row+column)%2 == 0 {
				drawDescending(false)
				drawAscending(true)
			} else {
				drawAscending(false)
				drawDescending(true)
			}
		}
	}
	// The macro reference falls into deep shadow along the near lower fold. A
	// broad translucent occlusion wash supplies that depth cue while leaving the
	// upper fibers in the cool key light.
	w.canvas.RadialGradient(w.viewport.X+w.viewport.Width*.08, w.viewport.Y+w.viewport.Height*1.06, radius*.92,
		scene.ColorHex(0x01070b, .27), scene.ColorHex(0x01070b, 0))
}

func (w *Workspace) energyWallCoordinates(x, y float32) (float32, float32) {
	right, up, normal := applicationBasis()
	point := right.Mul(x).Add(up.Mul(y)).Add(normal.Mul(energyWallDepth))
	px, py, _, visible := w.camera.Project(point, w.viewport)
	u, v := float32(.5)+x/16, float32(.5)-y/10
	if visible {
		u = (px - w.viewport.X) / w.viewport.Width
		v = (py - w.viewport.Y) / w.viewport.Height
	}
	return u, v
}

func (w *Workspace) impactEnergyWall(x, y, strength float32) {
	if w.energyNet == nil {
		return
	}
	u, v := w.energyWallCoordinates(x, y)
	w.energyNet.impact(u, v, strength)
}

func (w *Workspace) queueEnergyWallImpact(x, y, strength float32, at time.Duration) {
	if w.energyNet == nil {
		return
	}
	u, v := w.energyWallCoordinates(x, y)
	w.energyWallImpacts = append(w.energyWallImpacts, energyWallImpact{at: at, u: u, v: v, strength: strength})
}

func (w *Workspace) updateEnergyNet(dt time.Duration) {
	if w.energyNet == nil || dt <= 0 {
		w.energyWallImpacts = w.energyWallImpacts[:0]
		return
	}
	sort.SliceStable(w.energyWallImpacts, func(i, j int) bool { return w.energyWallImpacts[i].at < w.energyWallImpacts[j].at })
	elapsed := time.Duration(0)
	for _, impact := range w.energyWallImpacts {
		at := max(time.Duration(0), min(dt, impact.at))
		w.energyNet.update(at - elapsed)
		w.energyNet.impact(impact.u, impact.v, impact.strength)
		elapsed = at
	}
	w.energyNet.update(dt - elapsed)
	w.energyWallImpacts = w.energyWallImpacts[:0]
}

func energyWallDepthDelta(view ApplicationViewState, selected uint32, delta float32) (float32, bool) {
	if !finite(float64(delta)) {
		return 0, false
	}
	if delta >= 0 || selected == 0 {
		return delta, false
	}
	rear := float32(math.MaxFloat32)
	for i, placement := range view.Layouts {
		if selected&(1<<i) != 0 {
			rear = min(rear, placement.Depth)
		}
	}
	if rear == math.MaxFloat32 {
		return delta, false
	}
	limit := energyWallDepth + energyWindowClearance
	target := rear + delta
	if target > limit+.00001 {
		return delta, false
	}
	// Reflect the overshoot so even one large wheel packet cannot tunnel through
	// the wall. The same coefficient is used by inertial depth throws.
	reflected := (limit - rear) + max(float32(0), limit-target)*energyWallRestitution
	front := float32(-math.MaxFloat32)
	for i, placement := range view.Layouts {
		if selected&(1<<i) != 0 {
			front = max(front, placement.Depth)
		}
	}
	return min(reflected, 40-front), true
}

func energyWallImpactPosition(view ApplicationViewState, selected uint32) (x, y float32, ok bool) {
	rear := float32(math.MaxFloat32)
	for i, placement := range view.Layouts {
		if selected&(1<<i) != 0 {
			rear = min(rear, placement.Depth)
		}
	}
	if rear == math.MaxFloat32 {
		return 0, 0, false
	}
	count := 0
	for i, placement := range view.Layouts {
		if selected&(1<<i) != 0 && abs(placement.Depth-rear) <= .0001 {
			x += placement.X
			y += placement.Y
			count++
		}
	}
	if count == 0 {
		return 0, 0, false
	}
	return x / float32(count), y / float32(count), true
}

// constrainEnergyWallPlacements migrates documents created before the rear
// wall existed. Ordinary groups translate together so their authored spacing
// survives; only an impossible group spanning more than the entire usable
// depth range needs individual clamping at the front limit.
func constrainEnergyWallPlacements(view *ApplicationViewState) {
	const frontLimit = float32(40)
	wallLimit := energyWallDepth + energyWindowClearance
	visited := uint32(0)
	for i, placement := range view.Layouts {
		if placement.Key == "" || visited&(1<<i) != 0 {
			continue
		}
		members := uint32(1 << i)
		if placement.Group != 0 {
			for j, candidate := range view.Layouts {
				if candidate.Key != "" && candidate.Space == placement.Space && candidate.Group == placement.Group {
					members |= 1 << j
				}
			}
		}
		visited |= members
		rear, front := float32(math.MaxFloat32), float32(-math.MaxFloat32)
		for j, candidate := range view.Layouts {
			if members&(1<<j) != 0 {
				rear, front = min(rear, candidate.Depth), max(front, candidate.Depth)
			}
		}
		if rear >= wallLimit {
			continue
		}
		shift := min(wallLimit-rear, frontLimit-front)
		for j := range view.Layouts {
			if members&(1<<j) == 0 {
				continue
			}
			view.Layouts[j].Depth = min(frontLimit, max(wallLimit, view.Layouts[j].Depth+shift))
		}
	}
	view.aliases()
}
