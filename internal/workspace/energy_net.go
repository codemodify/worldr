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
	{1, 0, 72}, {0, 1, 72}, // woven structural strands
	{1, 1, 34}, {-1, 1, 34}, // shear keeps openings coherent
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
	return viewport.X + u*viewport.Width, viewport.Y + v*viewport.Height
}

func (w *Workspace) drawEnergyNetEdge(n *energyNet, a, b int, path0, path1, phase float64) {
	x0, y0 := n.screenPoint(a, w.viewport)
	x1, y1 := n.screenPoint(b, w.viewport)
	scale := w.scale
	w.canvas.Line(x0+0.7*scale, y0+0.9*scale, x1+0.7*scale, y1+0.9*scale, 1.55*scale, scene.ColorHex(0x01080d, .48))
	w.canvas.Line(x0, y0, x1, y1, .78*scale, scene.ColorHex(0x31b7cf, .14))
	// Resolve each cell into short conductive spans. Electricity now travels
	// along a strand instead of lighting a whole rectangular cell at once.
	const pieces = 4
	for piece := 0; piece < pieces; piece++ {
		t0, t1 := float32(piece)/pieces, float32(piece+1)/pieces
		pulse := energyPulse(path0+(path1-path0)*float64(t0+t1)/2, phase)
		if pulse <= .025 {
			continue
		}
		ax, ay := x0+(x1-x0)*t0, y0+(y1-y0)*t0
		bx, by := x0+(x1-x0)*t1, y0+(y1-y0)*t1
		w.canvas.Line(ax, ay, bx, by, (1.8+2.7*pulse)*scale, scene.ColorHex(0x28dcff, .035+.09*pulse))
		w.canvas.Line(ax, ay, bx, by, (.72+.52*pulse)*scale, scene.ColorHex(0xd1fbff, .18+.67*pulse))
	}
}

func (w *Workspace) drawEnergyNet() {
	n := w.energyNet
	if n == nil || w.viewport.Width <= 0 || w.viewport.Height <= 0 {
		return
	}
	phase := float64(n.phase) / float64(12*time.Second)
	for row := 0; row < energyNetRows; row++ {
		for column := 0; column < energyNetColumns-1; column++ {
			a, b := n.index(column, row), n.index(column+1, row)
			path0 := float64(column)/float64(energyNetColumns-1) + float64(row)*.071
			path1 := float64(column+1)/float64(energyNetColumns-1) + float64(row)*.071
			w.drawEnergyNetEdge(n, a, b, path0, path1, phase*3)
		}
	}
	for column := 0; column < energyNetColumns; column++ {
		for row := 0; row < energyNetRows-1; row++ {
			a, b := n.index(column, row), n.index(column, row+1)
			path0 := float64(row)/float64(energyNetRows-1) + float64(column)*.047
			path1 := float64(row+1)/float64(energyNetRows-1) + float64(column)*.047
			w.drawEnergyNetEdge(n, a, b, path0, path1, .18-phase*2)
		}
	}
	// Redraw a short alternating crossing segment to make the strands visibly
	// weave over and under instead of reading as a flat technical grid.
	for row := 1; row < energyNetRows-1; row++ {
		for column := 1; column < energyNetColumns-1; column++ {
			center := n.index(column, row)
			cx, cy := n.screenPoint(center, w.viewport)
			w.canvas.Circle(cx, cy, 1.18*w.scale, 0, scene.ColorHex(0x06131b, .62))
			var a, b int
			if (row+column)%2 == 0 {
				a, b = n.index(column-1, row), n.index(column+1, row)
			} else {
				a, b = n.index(column, row-1), n.index(column, row+1)
			}
			x0, y0 := n.screenPoint(a, w.viewport)
			x1, y1 := n.screenPoint(b, w.viewport)
			x0, y0 = cx+(x0-cx)*.105, cy+(y0-cy)*.105
			x1, y1 = cx+(x1-cx)*.105, cy+(y1-cy)*.105
			w.canvas.Line(x0, y0, x1, y1, .88*w.scale, scene.ColorHex(0x69d8e8, .29))
		}
	}
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
