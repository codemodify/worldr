package workspace

import (
	"fmt"
	"math"
	"time"

	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

const (
	ambientCatCycle        = 30 * time.Second
	ambientCatStrideCycles = 40
	ambientCatScale        = float32(.58)
	ambientCatSwatDuration = 440 * time.Millisecond
)

type ambientCatActivity uint8

const (
	ambientCatRunning ambientCatActivity = iota
	ambientCatPaused
	ambientCatNapping
)

type ambientCatBehaviorState struct {
	routePhase time.Duration
	activity   ambientCatActivity
	progress   float32
	blend      float32
}

// ambientCat is a retained low-poly rig for the desktop background scene. Its
// root is unpickable, so every child remains decorative even though the rig is
// made from ordinary 3D scene nodes. Geometry is immutable and shared by all
// repeated body, limb and ear instances; animation changes transforms only.
type ambientCat struct {
	scene         *scene.Scene
	phase         time.Duration
	shown         bool
	swatRemaining time.Duration
	swatLeg       int

	root, body, chest, head scene.NodeID
	legs                    [4]ambientCatLeg
	tail                    [5]scene.NodeID
	nodes                   []scene.NodeID

	bodyMesh, limbMesh, earMesh *scene.Mesh
}

type ambientCatLeg struct {
	hip, knee scene.NodeID
}

var ambientCatMaterial = render.Material{
	Specular: .58, Roughness: .38, Metallic: .16, RimStrength: .34,
	RimColor: [3]float32{.18, .78, .92},
}

// newAmbientCat installs one rig into background. The caller owns the scene;
// the returned object only retains node IDs and the three shared meshes.
func newAmbientCat(background *scene.Scene) (*ambientCat, error) {
	if background == nil {
		return nil, fmt.Errorf("ambient cat needs a background scene")
	}
	body, err := ambientCatBodyMesh()
	if err != nil {
		return nil, fmt.Errorf("build ambient cat body: %w", err)
	}
	limb, err := ambientCatLimbMesh()
	if err != nil {
		return nil, fmt.Errorf("build ambient cat limbs: %w", err)
	}
	ear, err := ambientCatEarMesh()
	if err != nil {
		return nil, fmt.Errorf("build ambient cat ears: %w", err)
	}

	c := &ambientCat{scene: background, shown: true, swatLeg: -1, bodyMesh: body, limbMesh: limb, earMesh: ear}
	c.root = c.add(0, scene.Node{Unpickable: true})
	c.body = c.addPart(c.root, body, scene.Scale(1, .42, .36), 0x69b9ca, .78, false)
	c.chest = c.addPart(c.root, body, scene.Translate(.58, .04, 0).Mul(scene.Scale(.48, .47, .41)), 0x83cedb, .8, false)

	// Keep the head's pivot unscaled so ears and face accents retain their own
	// proportions while the whole head bobs with the gait.
	c.head = c.add(c.root, scene.Node{Transform: scene.Translate(1.03, .29, 0)})
	c.addPart(c.head, body, scene.Scale(.43, .4, .38), 0x79c5d3, .84, false)
	// A compact muzzle and close-set triangular ears keep the silhouette feline
	// at the small background scale instead of reading as a fox or wolf.
	c.addPart(c.head, body, scene.Translate(.24, -.08, 0).Mul(scene.Scale(.2, .16, .25)), 0x91d5df, .84, false)
	c.addPart(c.head, body, scene.Translate(.42, -.07, 0).Mul(scene.Scale(.06, .052, .068)), 0xd99a68, .95, true)
	for _, side := range [...]float32{-1, 1} {
		c.addPart(c.head, body, scene.Translate(.22, .09, side*.3).Mul(scene.Scale(.055, .055, .045)), 0xf0bd68, 1, true)
		c.addPart(c.head, ear, scene.Translate(-.08, .3, side*.21).Mul(scene.Scale(.16, .29, .14)), 0x66afc1, .84, false)
	}

	anchors := [...]scene.Vec3{
		{X: .57, Y: -.24, Z: -.27}, {X: .57, Y: -.24, Z: .27},
		{X: -.57, Y: -.24, Z: -.27}, {X: -.57, Y: -.24, Z: .27},
	}
	for i, anchor := range anchors {
		hip := c.add(c.root, scene.Node{Transform: scene.Translate(anchor.X, anchor.Y, anchor.Z)})
		c.addPart(hip, limb, scene.Scale(.25, .5, .25), 0x5da8ba, .82, false)
		knee := c.add(hip, scene.Node{Transform: scene.Translate(0, -.46, 0)})
		c.addPart(knee, limb, scene.Scale(.21, .43, .21), 0x72bbca, .82, false)
		c.addPart(knee, body, scene.Translate(.07, -.42, 0).Mul(scene.Scale(.22, .12, .15)), 0x4a91a4, .86, false)
		c.legs[i] = ambientCatLeg{hip: hip, knee: knee}
	}

	parent := c.root
	for i := range c.tail {
		transform := scene.Translate(-.88, .13, 0).Mul(scene.RotateZ(-math.Pi / 2))
		if i > 0 {
			transform = scene.Translate(0, -.36, 0)
		}
		pivot := c.add(parent, scene.Node{Transform: transform})
		scale := float32(.19) - float32(i)*.022
		c.addPart(pivot, limb, scene.Scale(scale, .4, scale), 0x579fb2, .76, false)
		c.tail[i], parent = pivot, pivot
	}

	c.syncPose()
	return c, nil
}

func (c *ambientCat) add(parent scene.NodeID, node scene.Node) scene.NodeID {
	id := c.scene.Add(parent, node)
	c.nodes = append(c.nodes, id)
	return id
}

func (c *ambientCat) addPart(parent scene.NodeID, mesh *scene.Mesh, transform scene.Mat4, rgb uint32, alpha float32, unlit bool) scene.NodeID {
	return c.add(parent, scene.Node{
		Transform: transform, Mesh: mesh, Color: scene.ColorHex(rgb, alpha), Unlit: unlit,
		Material: ambientCatMaterial, WireColor: scene.ColorHex(0xa4f3ff, .18), WireWidth: .55,
	})
}

// update advances a bounded integer phase and derives the complete pose from
// it. Splitting the same elapsed time into different frame intervals therefore
// produces the same pose, and even a maximum time.Duration cannot overflow.
func (c *ambientCat) update(dt time.Duration) {
	if c == nil || dt <= 0 {
		return
	}
	if c.swatRemaining > 0 {
		if dt >= c.swatRemaining {
			c.swatRemaining, c.swatLeg = 0, -1
		} else {
			c.swatRemaining -= dt
		}
	}
	c.phase = (c.phase + dt%ambientCatCycle) % ambientCatCycle
	c.syncPose()
}

func (c *ambientCat) triggerSwat(leg int) {
	if c == nil || leg < 0 || leg > 1 {
		return
	}
	c.swatLeg, c.swatRemaining = leg, ambientCatSwatDuration
	c.syncPose()
}

func (c *ambientCat) setVisible(visible bool) {
	if c == nil || c.scene == nil {
		return
	}
	c.shown = visible
	if root := c.scene.Node(c.root); root != nil {
		root.Hidden = !visible
	}
}

func (c *ambientCat) visible() bool {
	return c != nil && c.shown
}

// ambientCatBehavior maps one bounded wall-clock cycle onto one complete route.
// Rest intervals hold a route point rather than accumulating numerical state, so
// split and combined updates always resolve to the same position and pose.
func ambientCatBehavior(phase time.Duration) ambientCatBehaviorState {
	phase %= ambientCatCycle
	if phase < 0 {
		phase += ambientCatCycle
	}
	seconds := phase.Seconds()
	state := ambientCatBehaviorState{activity: ambientCatRunning}
	switch {
	case seconds < 8:
		state.routePhase = time.Duration(seconds / 8 * 10 * float64(time.Second))
		state.progress = float32(seconds / 8)
	case seconds < 10:
		state.routePhase = 10 * time.Second
		state.activity = ambientCatPaused
		state.progress = float32((seconds - 8) / 2)
		state.blend = ambientCatRestBlend(seconds-8, 2)
	case seconds < 18:
		state.routePhase = 10*time.Second + time.Duration((seconds-10)/8*10*float64(time.Second))
		state.progress = float32((seconds - 10) / 8)
	case seconds < 23:
		state.routePhase = 20 * time.Second
		state.activity = ambientCatNapping
		state.progress = float32((seconds - 18) / 5)
		state.blend = ambientCatRestBlend(seconds-18, 5)
	default:
		state.routePhase = 20*time.Second + time.Duration((seconds-23)/7*10*float64(time.Second))
		state.progress = float32((seconds - 23) / 7)
	}
	return state
}

func ambientCatRestBlend(elapsed, duration float64) float32 {
	const transition = .38
	value := math.Min(1, math.Min(elapsed/transition, (duration-elapsed)/transition))
	if value <= 0 {
		return 0
	}
	// Smoothstep removes a visible hinge when the retained rig enters or leaves
	// a rest pose while retaining an exactly closed behavior cycle.
	return float32(value * value * (3 - 2*value))
}

// ambientCatPath is an analytic closed route through the background volume.
// It deliberately varies all three axes. Its nonzero tangent also supplies a
// stable forward direction without accumulating orientation error.
func ambientCatPath(phase time.Duration) (position, tangent scene.Vec3) {
	theta := 2 * math.Pi * float64(phase%ambientCatCycle) / float64(ambientCatCycle)
	position = scene.Vec3{
		X: 5.1 * float32(math.Sin(theta)),
		Y: .25 + 1.65*float32(math.Sin(2*theta+.35)),
		Z: 1.75 * float32(math.Sin(3*theta-.7)),
	}
	// The common d(theta)/dt factor is irrelevant for orientation.
	tangent = scene.Vec3{
		X: 5.1 * float32(math.Cos(theta)),
		Y: 3.3 * float32(math.Cos(2*theta+.35)),
		Z: 5.25 * float32(math.Cos(3*theta-.7)),
	}
	return
}

func (c *ambientCat) syncPose() {
	if c == nil || c.scene == nil || c.scene.Node(c.root) == nil {
		return
	}
	behavior := ambientCatBehavior(c.phase)
	position, tangent := ambientCatPath(behavior.routePhase)
	horizontal := float32(math.Hypot(float64(tangent.X), float64(tangent.Z)))
	yaw := float32(math.Atan2(float64(-tangent.Z), float64(tangent.X)))
	pitch := float32(math.Atan2(float64(tangent.Y), float64(horizontal)))
	if pitch > .68 {
		pitch = .68
	} else if pitch < -.68 {
		pitch = -.68
	}
	theta := 2 * math.Pi * float64(behavior.routePhase) / float64(ambientCatCycle)
	stride := float32(theta * ambientCatStrideCycles)
	rest := float32(0)
	if behavior.activity != ambientCatRunning {
		rest = behavior.blend
	}
	gait := 1 - rest
	gaitWave := float32(math.Sin(float64(2 * stride)))
	bounce := float32(.052) * (1 - float32(math.Cos(float64(2*stride)))) * gait
	bank := .09 * float32(math.Sin(2*theta)) * gait
	nap := float32(0)
	if behavior.activity == ambientCatNapping {
		nap = behavior.blend
	}
	c.scene.Node(c.root).Transform = scene.Translate(position.X, position.Y, position.Z).
		Mul(scene.RotateY(yaw)).Mul(scene.RotateZ(pitch)).Mul(scene.RotateX(bank + nap*.82)).
		Mul(scene.Translate(0, bounce-nap*.34, 0)).Mul(scene.Scale(ambientCatScale, ambientCatScale, ambientCatScale))

	c.scene.Node(c.body).Transform = scene.RotateZ(.025*gaitWave*gait - nap*.08).Mul(scene.Scale(1, .42, .36))
	c.scene.Node(c.chest).Transform = scene.Translate(.58, .04, 0).
		Mul(scene.RotateZ(-.018*gaitWave*gait - nap*.18)).Mul(scene.Scale(.48, .47, .41))
	headTurn := float32(0)
	if behavior.activity == ambientCatPaused {
		headTurn = .16 * behavior.blend * float32(math.Sin(float64(behavior.progress)*2*math.Pi))
	}
	c.scene.Node(c.head).Transform = scene.Translate(1.03-nap*.11, .29+.025*gaitWave*gait-nap*.32, 0).
		Mul(scene.RotateY(headTurn)).Mul(scene.RotateZ(.035*gaitWave*gait - nap*.22))

	// Diagonal pairs share a phase, producing a readable trot without mutable
	// per-foot state. Knees flex during the recovery half of each stride.
	phaseOffsets := [...]float32{0, math.Pi, math.Pi, 0}
	anchors := [...]scene.Vec3{
		{X: .57, Y: -.24, Z: -.27}, {X: .57, Y: -.24, Z: .27},
		{X: -.57, Y: -.24, Z: -.27}, {X: -.57, Y: -.24, Z: .27},
	}
	for i, leg := range c.legs {
		legPhase := stride + phaseOffsets[i]
		swing := .62 * float32(math.Sin(float64(legPhase)))
		recovery := float32(math.Sin(float64(legPhase + math.Pi/2)))
		if recovery < 0 {
			recovery = 0
		}
		kneeAngle := float32(-.12) - .52*recovery
		if behavior.activity == ambientCatPaused {
			pauseSwing := [...]float32{.06, -.06, -.04, .04}[i]
			swing += (pauseSwing - swing) * rest
			kneeAngle += (-.18 - kneeAngle) * rest
		}
		if nap > 0 {
			targetSwing := float32(.82)
			if i&1 != 0 {
				targetSwing = -.72
			}
			swing += (targetSwing - swing) * nap
			kneeAngle += (-1.02 - kneeAngle) * nap
		}
		if i == c.swatLeg && c.swatRemaining > 0 && behavior.activity == ambientCatRunning {
			progress := 1 - float64(c.swatRemaining)/float64(ambientCatSwatDuration)
			pulse := float32(math.Sin(math.Pi * min(1.0, max(0.0, progress))))
			swing += (1.08 - swing) * pulse
			kneeAngle += (-.04 - kneeAngle) * pulse
		}
		anchor := anchors[i]
		c.scene.Node(leg.hip).Transform = scene.Translate(anchor.X, anchor.Y, anchor.Z).Mul(scene.RotateZ(swing))
		c.scene.Node(leg.knee).Transform = scene.Translate(0, -.46, 0).Mul(scene.RotateZ(kneeAngle))
	}

	// The first tail pivot turns the canonical downward prism toward -X. Later
	// pivots inherit that heading and add only a traveling, tapered wave.
	for i, pivot := range c.tail {
		runWave := .13 * float32(math.Sin(float64(stride*.5+float32(i)*.58)))
		runSide := .085 * float32(math.Sin(float64(stride*.5+float32(i)*.71+1.2)))
		restClock := float32(math.Pi) * behavior.progress
		restWave := .035 * float32(math.Sin(float64(restClock+float32(i)*.38)))
		restSide := .022 * float32(math.Sin(float64(restClock+float32(i)*.31+1.2)))
		if behavior.activity == ambientCatNapping {
			restWave *= .5
			restSide *= .5
		}
		wave := runWave + (restWave-runWave)*rest
		side := runSide + (restSide-runSide)*rest
		transform := scene.Translate(-.88, .13, 0).Mul(scene.RotateZ(-math.Pi/2 + wave)).Mul(scene.RotateX(side))
		if i > 0 {
			transform = scene.Translate(0, -.36, 0).Mul(scene.RotateZ(wave)).Mul(scene.RotateX(side))
		}
		c.scene.Node(pivot).Transform = transform
	}
}

func (c *ambientCat) pawPosition(leg int) (scene.Vec3, bool) {
	if c == nil || c.scene == nil || leg < 0 || leg > 1 {
		return scene.Vec3{}, false
	}
	root, hip, knee := c.scene.Node(c.root), c.scene.Node(c.legs[leg].hip), c.scene.Node(c.legs[leg].knee)
	if root == nil || hip == nil || knee == nil {
		return scene.Vec3{}, false
	}
	world := root.Transform.Mul(hip.Transform).Mul(knee.Transform)
	return world.TransformPoint(scene.Vec3{X: .07, Y: -.43}), true
}

func ambientCatBodyMesh() (*scene.Mesh, error) {
	phi := float32((1 + math.Sqrt(5)) / 2)
	vertices := []scene.Vec3{
		{X: -1, Y: phi}, {X: 1, Y: phi}, {X: -1, Y: -phi}, {X: 1, Y: -phi},
		{Y: -1, Z: phi}, {Y: 1, Z: phi}, {Y: -1, Z: -phi}, {Y: 1, Z: -phi},
		{X: phi, Z: -1}, {X: phi, Z: 1}, {X: -phi, Z: -1}, {X: -phi, Z: 1},
	}
	for i := range vertices {
		vertices[i] = vertices[i].Normalize()
	}
	indices := []uint32{
		0, 11, 5, 0, 5, 1, 0, 1, 7, 0, 7, 10, 0, 10, 11,
		1, 5, 9, 5, 11, 4, 11, 10, 2, 10, 7, 6, 7, 1, 8,
		3, 9, 4, 3, 4, 2, 3, 2, 6, 3, 6, 8, 3, 8, 9,
		4, 9, 5, 2, 4, 11, 6, 2, 10, 8, 6, 7, 9, 8, 1,
	}
	return scene.NewMesh(vertices, indices, nil)
}

// ambientCatLimbMesh is a six-sided tapered prism with its joint at the origin
// and its distal end at Y=-1. The same mesh serves upper/lower legs and tail.
func ambientCatLimbMesh() (*scene.Mesh, error) {
	const sides = 6
	vertices := make([]scene.Vec3, 0, sides*2+2)
	for end, radius := range [...]float32{.5, .36} {
		y := -float32(end)
		for side := 0; side < sides; side++ {
			angle := 2 * math.Pi * float64(side) / sides
			vertices = append(vertices, scene.Vec3{X: radius * float32(math.Cos(angle)), Y: y, Z: radius * float32(math.Sin(angle))})
		}
	}
	topCenter, bottomCenter := uint32(len(vertices)), uint32(len(vertices)+1)
	vertices = append(vertices, scene.Vec3{}, scene.Vec3{Y: -1})
	indices := make([]uint32, 0, sides*12)
	for side := 0; side < sides; side++ {
		next := (side + 1) % sides
		a, d := uint32(side), uint32(next)
		b, cc := uint32(sides+side), uint32(sides+next)
		indices = append(indices, a, cc, b, a, d, cc)
		indices = append(indices, topCenter, d, a, bottomCenter, b, cc)
	}
	return scene.NewMesh(vertices, indices, nil)
}

func ambientCatEarMesh() (*scene.Mesh, error) {
	vertices := []scene.Vec3{
		{X: -.5, Z: -.3}, {X: .5, Z: -.3}, {Z: .36}, {Y: 1},
	}
	indices := []uint32{0, 2, 1, 0, 1, 3, 1, 2, 3, 2, 0, 3}
	return scene.NewMesh(vertices, indices, nil)
}
