package workspace

import (
	"math"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/scene"
)

func ambientCatFixture(t *testing.T) (*ambientCat, *scene.Scene) {
	t.Helper()
	background := scene.NewScene()
	cat, err := newAmbientCat(background)
	if err != nil {
		t.Fatal(err)
	}
	return cat, background
}

func ambientCatCamera() scene.Camera {
	return scene.Camera{Eye: scene.Vec3{Z: 15.5}, Up: scene.Vec3{Y: 1}, FOV: .69, Near: .1, Far: 40}
}

func ambientCatTransforms(cat *ambientCat) []scene.Mat4 {
	transforms := make([]scene.Mat4, len(cat.nodes))
	for i, id := range cat.nodes {
		transforms[i] = cat.scene.Node(id).Transform
	}
	return transforms
}

func TestAmbientCatBuildsOneRetainedUnpickableRig(t *testing.T) {
	if cat, err := newAmbientCat(nil); err == nil || cat != nil {
		t.Fatal("nil background scene created a cat")
	}
	cat, background := ambientCatFixture(t)
	if !cat.visible() || background.Node(cat.root) == nil || background.Node(cat.root).Hidden || !background.Node(cat.root).Unpickable {
		t.Fatal("cat did not start as a visible decorative root")
	}
	if len(cat.nodes) != 41 || cat.bodyMesh.TriangleCount() != 20 || cat.limbMesh.TriangleCount() != 24 || cat.earMesh.TriangleCount() != 4 {
		t.Fatalf("unexpected retained rig budget: nodes=%d triangles=%d/%d/%d", len(cat.nodes), cat.bodyMesh.TriangleCount(), cat.limbMesh.TriangleCount(), cat.earMesh.TriangleCount())
	}

	canvas, err := scene.NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Close()
	canvas.Reset(1440, 900)
	viewport := scene.Viewport{X: 24, Y: 48, Width: 1310, Height: 828}
	background.Draw(canvas, ambientCatCamera(), viewport)
	frame := canvas.Frame()
	if len(frame.Commands) != 1 || len(frame.Commands[0].Draws) != 26 {
		t.Fatalf("retained rig submitted an unexpected number of passes/draws: %d/%d", len(frame.Commands), func() int {
			if len(frame.Commands) == 0 {
				return 0
			}
			return len(frame.Commands[0].Draws)
		}())
	}
	allowed := map[any]bool{
		cat.bodyMesh.Geometry(): true,
		cat.limbMesh.Geometry(): true,
		cat.earMesh.Geometry():  true,
	}
	for _, draw := range frame.Commands[0].Draws {
		if !allowed[draw.Geometry] {
			t.Fatal("cat rebuilt or submitted geometry outside its three shared meshes")
		}
	}

	// Prove that the root, rather than 26 individual parts, owns interaction
	// exclusion. A ray through the torso hits when that inheritance is disabled.
	center := background.Node(cat.root).Transform.TransformPoint(scene.Vec3{})
	x, y, _, visible := ambientCatCamera().Project(center, viewport)
	if !visible {
		t.Fatal("initial cat pose is outside the background camera")
	}
	background.Node(cat.root).Unpickable = false
	if _, found := background.Pick(ambientCatCamera(), viewport, x, y); !found {
		t.Fatal("fixture ray did not intersect the visible cat")
	}
	background.Node(cat.root).Unpickable = true
	if _, found := background.Pick(ambientCatCamera(), viewport, x, y); found {
		t.Fatal("unpickable root did not exclude a child mesh")
	}
}

func TestAmbientCatRouteIsClosedBoundedAndUsesEveryAxis(t *testing.T) {
	minimum := scene.Vec3{X: math.MaxFloat32, Y: math.MaxFloat32, Z: math.MaxFloat32}
	maximum := scene.Vec3{X: -math.MaxFloat32, Y: -math.MaxFloat32, Z: -math.MaxFloat32}
	viewport := scene.Viewport{X: 24, Y: 48, Width: 1310, Height: 828}
	for sample := 0; sample < 600; sample++ {
		phase := ambientCatCycle * time.Duration(sample) / 600
		position, tangent := ambientCatPath(phase)
		minimum.X, minimum.Y, minimum.Z = min(minimum.X, position.X), min(minimum.Y, position.Y), min(minimum.Z, position.Z)
		maximum.X, maximum.Y, maximum.Z = max(maximum.X, position.X), max(maximum.Y, position.Y), max(maximum.Z, position.Z)
		if tangent.Length() < 2.4 {
			t.Fatalf("route lost a stable forward direction at %v: %+v", phase, tangent)
		}
		if _, _, _, visible := ambientCatCamera().Project(position, viewport); !visible {
			t.Fatalf("route center left the fixed background camera at %v: %+v", phase, position)
		}
	}
	if maximum.X-minimum.X < 10 || maximum.Y-minimum.Y < 3.2 || maximum.Z-minimum.Z < 3.4 {
		t.Fatalf("route did not traverse x/y/depth: min=%+v max=%+v", minimum, maximum)
	}
	start, startTangent := ambientCatPath(0)
	loop, loopTangent := ambientCatPath(ambientCatCycle)
	if start != loop || startTangent != loopTangent {
		t.Fatal("closed route has a seam")
	}

	cat, background := ambientCatFixture(t)
	forward := background.Node(cat.root).Transform.TransformVector(scene.Vec3{X: 1}).Normalize()
	if forward.Dot(startTangent.Normalize()) < .999 {
		t.Fatalf("cat did not face its route tangent: forward=%+v tangent=%+v", forward, startTangent.Normalize())
	}
	for _, axis := range []scene.Vec3{{X: 1}, {Y: 1}, {Z: 1}} {
		if got := background.Node(cat.root).Transform.TransformVector(axis).Length(); math.Abs(float64(got-ambientCatScale)) > 1e-5 {
			t.Fatalf("cat root scale = %g, want %g", got, ambientCatScale)
		}
	}
}

func TestAmbientCatBehaviorRunsPausesNapsAndClosesExactly(t *testing.T) {
	tests := []struct {
		phase    time.Duration
		activity ambientCatActivity
		route    time.Duration
	}{
		{4 * time.Second, ambientCatRunning, 5 * time.Second},
		{9 * time.Second, ambientCatPaused, 10 * time.Second},
		{14 * time.Second, ambientCatRunning, 15 * time.Second},
		{20 * time.Second, ambientCatNapping, 20 * time.Second},
		{26500 * time.Millisecond, ambientCatRunning, 25 * time.Second},
		{ambientCatCycle, ambientCatRunning, 0},
	}
	for _, test := range tests {
		state := ambientCatBehavior(test.phase)
		if state.activity != test.activity || state.routePhase != test.route {
			t.Fatalf("behavior at %v = activity %d route %v, want %d/%v", test.phase, state.activity, state.routePhase, test.activity, test.route)
		}
	}

	cat, background := ambientCatFixture(t)
	cat.phase = 8500 * time.Millisecond
	cat.syncPose()
	pauseStart := background.Node(cat.root).Transform.TransformPoint(scene.Vec3{})
	cat.phase = 9500 * time.Millisecond
	cat.syncPose()
	pauseEnd := background.Node(cat.root).Transform.TransformPoint(scene.Vec3{})
	if pauseStart.Sub(pauseEnd).Length() > 1e-5 {
		t.Fatal("standing pause drifted along the route")
	}
	cat.phase = 18 * time.Second
	cat.syncPose()
	upright := background.Node(cat.root).Transform
	cat.phase = 20 * time.Second
	cat.syncPose()
	if background.Node(cat.root).Transform == upright || background.Node(cat.legs[0].hip).Transform == scene.Identity() {
		t.Fatal("nap did not lower/curl the retained rig")
	}
}

func TestAmbientCatSwatUsesOneFrontPawAndExpires(t *testing.T) {
	cat, background := ambientCatFixture(t)
	cat.phase = 3 * time.Second
	cat.syncPose()
	baseline := ambientCatTransforms(cat)
	cat.triggerSwat(1)
	cat.swatRemaining = ambientCatSwatDuration / 2
	cat.syncPose()
	if background.Node(cat.legs[1].hip).Transform == baseline[indexOfCatNode(cat, cat.legs[1].hip)] {
		t.Fatal("swat did not move the selected front leg")
	}
	if background.Node(cat.legs[0].hip).Transform != baseline[indexOfCatNode(cat, cat.legs[0].hip)] {
		t.Fatal("swat moved the other front leg")
	}
	cat.swatRemaining, cat.swatLeg = 0, -1
	cat.syncPose()
	for index, transform := range ambientCatTransforms(cat) {
		if transform != baseline[index] {
			t.Fatalf("expired swat left node %d displaced", index)
		}
	}
}

func TestAmbientCatRestTransitionsBlendJointAnglesWithoutStrideSweeps(t *testing.T) {
	cat, background := ambientCatFixture(t)
	transitions := []struct {
		name       string
		start, end time.Duration
	}{
		{"pause entry", 7800 * time.Millisecond, 8500 * time.Millisecond},
		{"pause exit", 9500 * time.Millisecond, 10200 * time.Millisecond},
		{"nap entry", 17800 * time.Millisecond, 18500 * time.Millisecond},
		{"nap exit", 22500 * time.Millisecond, 23200 * time.Millisecond},
	}
	jointAngles := func() [8]float64 {
		var angles [8]float64
		for index, leg := range cat.legs {
			hip, knee := background.Node(leg.hip).Transform, background.Node(leg.knee).Transform
			angles[index*2] = math.Atan2(float64(hip[1]), float64(hip[0]))
			angles[index*2+1] = math.Atan2(float64(knee[1]), float64(knee[0]))
		}
		return angles
	}
	angleDelta := func(a, b float64) float64 {
		return math.Mod(b-a+math.Pi, 2*math.Pi) - math.Pi
	}

	for _, transition := range transitions {
		t.Run(transition.name, func(t *testing.T) {
			cat.phase = transition.start
			cat.syncPose()
			previous := jointAngles()
			var travel [8]float64
			for phase := transition.start + 10*time.Millisecond; phase <= transition.end; phase += 10 * time.Millisecond {
				cat.phase = phase
				cat.syncPose()
				current := jointAngles()
				for joint := range current {
					delta := math.Abs(angleDelta(previous[joint], current[joint]))
					if delta > .16 {
						t.Fatalf("joint %d swept %g radians in 10ms at %v", joint, delta, phase)
					}
					travel[joint] += delta
				}
				previous = current
			}
			for joint, distance := range travel {
				if distance > 6 {
					t.Fatalf("joint %d accumulated %g radians across one rest transition", joint, distance)
				}
			}
		})
	}
}

func indexOfCatNode(cat *ambientCat, id scene.NodeID) int {
	for index, candidate := range cat.nodes {
		if candidate == id {
			return index
		}
	}
	return -1
}

func TestAmbientCatTimingIsFrameIndependentBoundedAndDrawPure(t *testing.T) {
	fast, _ := ambientCatFixture(t)
	slow, _ := ambientCatFixture(t)
	for i := 0; i < 100; i++ {
		fast.update(10 * time.Millisecond)
	}
	slow.update(time.Second)
	if fast.phase != slow.phase {
		t.Fatalf("split updates changed phase: %v != %v", fast.phase, slow.phase)
	}
	fastTransforms, slowTransforms := ambientCatTransforms(fast), ambientCatTransforms(slow)
	for i := range fastTransforms {
		if fastTransforms[i] != slowTransforms[i] {
			t.Fatalf("split updates changed node %d pose", i)
		}
	}

	initial, _ := ambientCatFixture(t)
	initialTransforms := ambientCatTransforms(initial)
	initial.update(ambientCatCycle)
	if initial.phase != 0 {
		t.Fatal("whole route cycle did not wrap exactly")
	}
	for i, transform := range ambientCatTransforms(initial) {
		if transform != initialTransforms[i] {
			t.Fatalf("whole route cycle left a pose seam at node %d", i)
		}
	}
	before := ambientCatTransforms(initial)
	initial.update(0)
	initial.update(-time.Second)
	if initial.phase != 0 {
		t.Fatal("zero or negative update advanced the cat")
	}
	for i, transform := range ambientCatTransforms(initial) {
		if transform != before[i] {
			t.Fatalf("non-positive update changed node %d", i)
		}
	}

	initial.update(7 * time.Second)
	phase := initial.phase
	initial.update(time.Duration(math.MaxInt64))
	want := (phase + time.Duration(math.MaxInt64)%ambientCatCycle) % ambientCatCycle
	if initial.phase != want || initial.phase < 0 || initial.phase >= ambientCatCycle {
		t.Fatalf("maximum duration overflowed bounded phase: got %v want %v", initial.phase, want)
	}
	for _, transform := range ambientCatTransforms(initial) {
		for _, value := range transform {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				t.Fatal("long update produced a non-finite transform")
			}
		}
	}

	// Rendering the retained nodes does not own time and must stay pure.
	canvas, err := scene.NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Close()
	pose, phase := ambientCatTransforms(initial), initial.phase
	for i := 0; i < 3; i++ {
		canvas.Reset(1440, 900)
		initial.scene.Draw(canvas, ambientCatCamera(), scene.Viewport{Width: 1440, Height: 900})
	}
	if initial.phase != phase {
		t.Fatal("draw advanced ambient cat time")
	}
	for i, transform := range ambientCatTransforms(initial) {
		if transform != pose[i] {
			t.Fatalf("draw changed node %d", i)
		}
	}
}

func TestAmbientCatToggleHidesTheRigWithoutStoppingItsClock(t *testing.T) {
	cat, background := ambientCatFixture(t)
	root, geometry := cat.root, cat.bodyMesh.Geometry()
	cat.setVisible(false)
	if cat.visible() || !background.Node(root).Hidden {
		t.Fatal("toggle did not hide the retained root")
	}
	before, phase := background.Node(root).Transform, cat.phase
	cat.update(1375 * time.Millisecond)
	if cat.phase == phase || background.Node(root).Transform == before {
		t.Fatal("hidden cat stopped its ambient clock or pose")
	}
	canvas, err := scene.NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Close()
	canvas.Reset(640, 360)
	background.Draw(canvas, ambientCatCamera(), scene.Viewport{Width: 640, Height: 360})
	if len(canvas.Frame().Commands) != 0 {
		t.Fatal("hidden root submitted descendants")
	}
	cat.setVisible(true)
	if !cat.visible() || background.Node(root).Hidden || cat.bodyMesh.Geometry() != geometry || cat.root != root {
		t.Fatal("reenabling cat rebuilt resources or failed to reveal the root")
	}
	canvas.Reset(640, 360)
	background.Draw(canvas, ambientCatCamera(), scene.Viewport{Width: 640, Height: 360})
	if len(canvas.Frame().Commands) != 1 {
		t.Fatal("reenabled cat did not return to the background pass")
	}
}
