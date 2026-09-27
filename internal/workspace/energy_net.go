package workspace

import (
	"math"
	"sort"
	"time"

	"github.com/codemodify/worldr/internal/scene"
)

const (
	energyNetColumns = 25
	energyNetRows    = 15
	energyNetAspect  = float32(1.64)
	// Hexes across the visible sheet. The count is the cell size; rows follow
	// from the pointy-top spacing so openings stay close to regular.
	energyWeaveAcross    = 64
	energyNetStep        = time.Second / 120
	energyNetSettleTicks = 8 * 120

	// Application Depth points toward the initial camera. The woven wall is a
	// rear barrier, and the small clearance keeps the back of a window in front
	// of its visible strands.
	energyWallDepth       = float32(-3.6)
	energyWindowClearance = float32(.18)
	energyWallRestitution = float32(.46)
	// The visible sheet is a world-space plane behind the windows. Its span is
	// wide enough that looking around reveals more of it instead of a screen
	// overlay sliding independently of the room.
	energyWallSpanX = float32(64)
	energyWallSpanY = float32(40)
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
	deformGen   uint64
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
		n.deformGen++
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
	n.deformGen++
}

func (n *energyNet) impact(u, v, strength float32) {
	if n == nil || !finite(float64(u)) || !finite(float64(v)) || !finite(float64(strength)) || strength <= 0 {
		return
	}
	u, v = max(float32(.03), min(float32(.97), u)), max(float32(.03), min(float32(.97), v))
	n.impactCount++
	n.deformGen++
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

func (n *energyNet) nodeWorld(index int) scene.Vec3 {
	point := n.points[index].position
	u, v := point.X/energyNetAspect, point.Y
	phase := float64(n.phase) / float64(12*time.Second)
	boundedU := min(float32(1), max(float32(0), u))
	boundedV := min(float32(1), max(float32(0), v))
	envelope := float32(math.Sin(float64(math.Pi*boundedU)) * math.Sin(float64(math.Pi*boundedV)))
	breath := .012 * envelope * float32(math.Sin(2*math.Pi*(2*phase+float64(u)*.7+float64(v)*.4)))
	right, up, normal := applicationBasis()
	// Positive sheet V is down the wall, matching the screen. Placement Y is up,
	// so the visible sheet uses the opposite sign and stays tied to window X/Y.
	return right.Mul((u - .5) * energyWallSpanX).
		Add(up.Mul((.5 - v) * energyWallSpanY)).
		Add(normal.Mul(energyWallDepth + (point.Z+breath)*.9))
}

type energyNetCanvasPoint struct{ x, y float32 }

type energyNetProjection struct {
	points [energyNetColumns * energyNetRows]scene.Vec3
}

func (n *energyNet) projection() energyNetProjection {
	var projected energyNetProjection
	for index := range projected.points {
		projected.points[index] = n.nodeWorld(index)
	}
	return projected
}

func (p *energyNetProjection) sample(column, row float32) scene.Vec3 {
	c0 := int(math.Floor(float64(column)))
	r0 := int(math.Floor(float64(row)))
	tx, ty := column-float32(c0), row-float32(r0)
	a, b := p.gridPoint(c0, r0), p.gridPoint(c0+1, r0)
	c, d := p.gridPoint(c0, r0+1), p.gridPoint(c0+1, r0+1)
	ab := a.Add(b.Sub(a).Mul(tx))
	cd := c.Add(d.Sub(c).Mul(tx))
	return ab.Add(cd.Sub(ab).Mul(ty))
}

func (w *Workspace) projectWall(point scene.Vec3) (energyNetCanvasPoint, bool) {
	forward := w.camera.Target.Sub(w.camera.Eye)
	if point.Sub(w.camera.Eye).Dot(forward) <= .05*forward.Length() {
		return energyNetCanvasPoint{}, false
	}
	x, y, _, _ := w.camera.Project(point, w.viewport)
	if !finite(float64(x)) || !finite(float64(y)) {
		return energyNetCanvasPoint{}, false
	}
	return energyNetCanvasPoint{x, y}, true
}

func (w *Workspace) weaveSample(projected *energyNetProjection, u, v float32) (energyNetCanvasPoint, bool) {
	// A shared parameter keeps neighboring hexes from splitting an edge. The
	// small irregularity is in sheet space, so it turns with the room.
	jx := float32(math.Sin(float64(u)*46.3+float64(v)*18.7)) * .004
	jy := float32(math.Sin(float64(u)*21.1-float64(v)*39.4+1.7)) * .003
	u += jx
	v += jy
	u = .5 + (u-.5)*1.22
	v = .5 + (v-.5)*1.22
	return w.projectWall(projected.sample(u*float32(energyNetColumns-1), v*float32(energyNetRows-1)))
}

func energyWeaveLight(u, v float32) float32 {
	// The reference falls off toward the corners and the near lower fold. Keep
	// the brightest strands well below UI cyan so the sheet stays background.
	vertical := float32(math.Pow(float64(max(float32(0), 1-.72*v)), 1.15))
	span := float32(math.Hypot(float64(u-.48), float64((v-.28)*1.15)))
	radial := 1 - .62*min(float32(1), span*1.45)
	return min(float32(.72), max(float32(.05), .06+vertical*radial*.62))
}

func (w *Workspace) drawEnergyWeaveStrand(a, b energyNetCanvasPoint, family int, light, phase, path float32) {
	margin := 6 * w.scale
	canvasWidth, canvasHeight := w.canvas.Size()
	if max(a.x, b.x) < -margin || min(a.x, b.x) > float32(canvasWidth)+margin ||
		max(a.y, b.y) < -margin || min(a.y, b.y) > float32(canvasHeight)+margin {
		return
	}
	dx, dy := b.x-a.x, b.y-a.y
	if float32(math.Hypot(float64(dx), float64(dy))) <= 1e-4 {
		return
	}
	pulse := energyPulse(float64(path), float64(phase)+float64(family)*.19)
	body := .045 + .09*light
	highlight := .05 + .12*light + .028*pulse
	dark := scene.ColorHex(0x0a333c, body)
	lit := scene.ColorHex(0x5c9ea6, highlight)
	electric := scene.ColorHex(0xc5eeee, lit.A)
	blend := .18 * pulse
	lit.R += (electric.R - lit.R) * blend
	lit.G += (electric.G - lit.G) * blend
	lit.B += (electric.B - lit.B) * blend
	width := (1.35 + .28*light) * w.scale
	if -dy+dx < 0 {
		w.canvas.ShadedLine(a.x, a.y, b.x, b.y, width, dark, lit)
	} else {
		w.canvas.ShadedLine(a.x, a.y, b.x, b.y, width, lit, dark)
	}
}

func (p *energyNetProjection) gridPoint(column, row int) scene.Vec3 {
	if column < 0 {
		p0, p1 := p.gridPoint(0, row), p.gridPoint(1, row)
		return p0.Add(p1.Sub(p0).Mul(float32(column)))
	}
	if column >= energyNetColumns {
		p0 := p.gridPoint(energyNetColumns-1, row)
		p1 := p.gridPoint(energyNetColumns-2, row)
		return p0.Add(p0.Sub(p1).Mul(float32(column - (energyNetColumns - 1))))
	}
	if row >= 0 && row < energyNetRows {
		return p.points[row*energyNetColumns+column]
	}
	// Continue the sheet past the pinned simulation boundary. These points never
	// participate in physics; they only keep the weave from showing a cut edge.
	if row < 0 {
		p0, p1 := p.points[column], p.points[energyNetColumns+column]
		return p0.Add(p1.Sub(p0).Mul(float32(row)))
	}
	p0 := p.points[(energyNetRows-1)*energyNetColumns+column]
	p1 := p.points[(energyNetRows-2)*energyNetColumns+column]
	return p0.Add(p0.Sub(p1).Mul(float32(row - (energyNetRows - 1))))
}

type energyWeaveKey struct {
	ex, ey, ez int32
	tx, ty, tz int32
	vx, vy     int32
	vw, vh     int32
	phase      int64
	deform     uint64
	scale      int32
}

func quantizeWeave(value, step float32) int32 {
	return int32(math.Round(float64(value / step)))
}

func (w *Workspace) energyWeaveKey() energyWeaveKey {
	n := w.energyNet
	return energyWeaveKey{
		ex: quantizeWeave(w.camera.Eye.X, .02), ey: quantizeWeave(w.camera.Eye.Y, .02), ez: quantizeWeave(w.camera.Eye.Z, .02),
		tx: quantizeWeave(w.camera.Target.X, .02), ty: quantizeWeave(w.camera.Target.Y, .02), tz: quantizeWeave(w.camera.Target.Z, .02),
		vx: int32(w.viewport.X), vy: int32(w.viewport.Y), vw: int32(w.viewport.Width), vh: int32(w.viewport.Height),
		phase: int64(n.phase / (100 * time.Millisecond)), deform: n.deformGen, scale: quantizeWeave(w.scale, .01),
	}
}

func (w *Workspace) drawEnergyNet() {
	n := w.energyNet
	if n == nil || w.viewport.Width <= 0 || w.viewport.Height <= 0 {
		return
	}
	// The sheet is static for a tenth of a second unless the view or the
	// springs move. Pointer frames then replay it instead of rebuilding it.
	key := w.energyWeaveKey()
	if key == w.weaveKey && len(w.weaveVerts) > 0 {
		w.canvas.AppendVertices(w.weaveVerts)
		return
	}
	recorded := w.canvas.VertexCount()
	phase := float32(float64(n.phase) / float64(12*time.Second))
	radius := max(w.viewport.Width, w.viewport.Height) * .62
	w.canvas.RadialGradient(w.viewport.X+w.viewport.Width*.48, w.viewport.Y+w.viewport.Height*.34, radius,
		scene.ColorHex(0x123f48, .02), scene.ColorHex(0x07121a, 0))
	projection := n.projection()
	size := 1 / (float32(energyWeaveAcross) * float32(math.Sqrt(3)))
	xStep, yStep := size*float32(math.Sqrt(3)), size*1.5
	minCol := int(math.Floor(-.28 / float64(xStep)))
	maxCol := int(math.Ceil(1.28 / float64(xStep)))
	minRow := int(math.Floor(-.32 / float64(yStep)))
	maxRow := int(math.Ceil(1.32 / float64(yStep)))
	seen := make(map[[4]int32]struct{}, (maxCol-minCol+1)*(maxRow-minRow+1)*3)
	quantize := func(value float32) int32 { return int32(math.Round(float64(value * 20000))) }
	for row := minRow; row <= maxRow; row++ {
		for col := minCol; col <= maxCol; col++ {
			cx := xStep * (float32(col) + .5*float32(row&1))
			cy := yStep * float32(row)
			var vertex [6][2]float32
			for i := 0; i < 6; i++ {
				angle := (float64(i)*60 + 30) * math.Pi / 180
				vertex[i][0] = cx + size*float32(math.Cos(angle))
				vertex[i][1] = cy + size*float32(math.Sin(angle))
			}
			light := energyWeaveLight(cx, cy)
			for i := 0; i < 6; i++ {
				u0, v0 := vertex[i][0], vertex[i][1]
				u1, v1 := vertex[(i+1)%6][0], vertex[(i+1)%6][1]
				a0, b0, a1, b1 := quantize(u0), quantize(v0), quantize(u1), quantize(v1)
				if a0 > a1 || (a0 == a1 && b0 > b1) {
					a0, b0, a1, b1 = a1, b1, a0, b0
				}
				edge := [4]int32{a0, b0, a1, b1}
				if _, ok := seen[edge]; ok {
					continue
				}
				seen[edge] = struct{}{}
				start, startOK := w.weaveSample(&projection, u0, v0)
				end, endOK := w.weaveSample(&projection, u1, v1)
				if !startOK || !endOK {
					continue
				}
				w.drawEnergyWeaveStrand(start, end, i%3, light, phase*1.15+float32(i)*.07, (v0+v1)*.5+(u0-u1)*.2)
			}
		}
	}
	w.canvas.RadialGradient(w.viewport.X+w.viewport.Width*.12, w.viewport.Y+w.viewport.Height*1.08, radius,
		scene.ColorHex(0x01070b, .34), scene.ColorHex(0x01070b, 0))
	w.weaveVerts = w.canvas.VertexSnapshot(recorded)
	w.weaveKey = key
}

func (w *Workspace) energyWallCoordinates(x, y float32) (float32, float32) {
	// Window X/Y are already the room axes of the rear sheet. A screen projection
	// would move the dent when the view turns, even though the window did not.
	return .5 + x/energyWallSpanX, .5 - y/energyWallSpanY
}

func (w *Workspace) impactEnergyWall(x, y, strength float32) {
	if w.energyNet == nil || w.skinBackdropVisible() {
		return
	}
	u, v := w.energyWallCoordinates(x, y)
	w.energyNet.impact(u, v, strength)
}

func (w *Workspace) queueEnergyWallImpact(x, y, strength float32, at time.Duration) {
	if w.energyNet == nil || w.skinBackdropVisible() {
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
