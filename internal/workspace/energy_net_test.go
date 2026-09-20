package workspace

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

func TestEnergyNetIsPinnedPhysicalAndFrameIndependent(t *testing.T) {
	fast, slow := newEnergyNet(), newEnergyNet()
	fast.impact(.42, .57, 1.1)
	slow.impact(.42, .57, 1.1)
	center := fast.index(energyNetColumns/2, energyNetRows/2)
	if fast.points[center].velocity.Z >= 0 || fast.impactCount != 1 {
		t.Fatal("impact did not inject normal momentum into the woven wall")
	}
	for i := 0; i < 100; i++ {
		fast.update(10 * time.Millisecond)
	}
	slow.update(time.Second)
	if fast.phase != slow.phase || fast.accumulator != slow.accumulator || fast.activeTicks != slow.activeTicks || !reflect.DeepEqual(fast.points, slow.points) {
		t.Fatal("fixed-step wall physics changed with frame granularity")
	}
	moved := false
	for row := 0; row < energyNetRows; row++ {
		for column := 0; column < energyNetColumns; column++ {
			point := fast.points[fast.index(column, row)]
			if point.pinned {
				if point.position != point.rest || point.velocity != (point.rest.Sub(point.rest)) {
					t.Fatal("a pinned wall edge moved")
				}
			} else if point.position != point.rest {
				moved = true
			}
		}
	}
	if !moved {
		t.Fatal("impact failed to propagate through the spring lattice")
	}
	fast.update(9 * time.Second)
	for _, point := range fast.points {
		if point.position != point.rest || point.velocity != (point.rest.Sub(point.rest)) {
			t.Fatal("long suspension did not safely settle transient wall motion")
		}
	}
}

func TestEnergyNetDrawsBeforeEverySceneAndStaysAmbient(t *testing.T) {
	w := desktop(t)
	apps := desktopApplications(t, w)
	before, history := w.Document(), w.historyPosition
	first := w.Draw(1440, 900)
	if w.energyNet == nil || len(first.Commands) < 3 || first.Commands[0].Kind != render.OverlayCommand {
		t.Fatal("desktop did not draw its woven wall as the earliest background layer")
	}
	dnaIndex, _, _ := dnaBackgroundCommand(t, w, first)
	applicationIndex := -1
	for i, command := range first.Commands {
		for _, draw := range command.Draws {
			if draw.Texture == apps.surfaces[0].Texture {
				applicationIndex = i
			}
		}
	}
	if dnaIndex <= 0 || applicationIndex <= dnaIndex {
		t.Fatal("woven wall, DNA landmark and application scene lost their rear-to-front ordering")
	}
	if len(first.Vertices) > 120000 {
		t.Fatalf("woven wall exceeded its bounded overlay budget: %d vertices", len(first.Vertices))
	}
	vertices := append([]render.Vertex(nil), first.Vertices...)
	w.Update(180 * time.Millisecond)
	second := w.Draw(1440, 900)
	if reflect.DeepEqual(vertices, second.Vertices) {
		t.Fatal("electrical flow and physical breathing left the woven wall static")
	}
	if w.Document() != before || w.historyPosition != history || w.CanUndo() {
		t.Fatal("ambient wall motion entered document or undo state")
	}
	study := study(t)
	if study.energyNet != nil {
		t.Fatal("standalone AXIAL allocated the desktop-only energy wall")
	}
}

func depthThrowGesture(t *testing.T, w *Workspace, surface experience.ApplicationSurface) {
	t.Helper()
	x, y := windowGripPoint(t, w, surface)
	for i, event := range []experience.Event{
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y, Time: 1000},
		{Kind: experience.PointerScroll, X: x, Y: y, ScrollY: 90, Time: 1020},
		{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x, Y: y, Time: 1040},
	} {
		if !w.Handle(event) {
			t.Fatalf("depth throw event %d was not consumed", i)
		}
	}
}

func TestWindowDepthThrowStrikesEnergyWallAndBounces(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 1)
	before, history := w.Document(), w.historyPosition
	depthThrowGesture(t, w, apps.surfaces[0])
	if w.windowThrow == nil || w.windowThrow.vz >= 0 || w.windowThrow.wallCollisionTime < 0 || w.historyPosition != history+1 {
		t.Fatal("scrolling a held window toward the wall did not start one depth throw")
	}
	released := w.Document().View.Application.Layouts[0].Depth
	w.Update(120 * time.Millisecond)
	afterImpact := w.Document().View.Application.Layouts[0].Depth
	if afterImpact < energyWallDepth+energyWindowClearance-.0001 || afterImpact <= released || w.energyNet.impactCount == 0 || !w.windowThrow.wallImpactSent {
		t.Fatalf("depth throw failed to rebound and deform the wall: release=%g impact=%g count=%d", released, afterImpact, w.energyNet.impactCount)
	}
	w.Update(10 * time.Second)
	if w.windowThrow != nil || w.Document().View.Application.Layouts[0].Depth < energyWallDepth+energyWindowClearance {
		t.Fatal("rebounded window did not settle naturally in front of the wall")
	}
	after := w.Document()
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("one undo did not restore the complete depth drag, collision and rebound")
	}
	command(t, w, Action{Kind: Redo})
	if w.Document() != after || w.windowThrow != nil {
		t.Fatal("redo replayed depth momentum or lost the rebound resting place")
	}
}

func TestWindowDepthBounceIsFrameIndependentAndRigidForGroups(t *testing.T) {
	fast, fastApps := windowThrowWorkspace(t, 2)
	slow, slowApps := windowThrowWorkspace(t, 2)
	for _, fixture := range []struct {
		w    *Workspace
		apps *fakeApplications
	}{{fast, fastApps}, {slow, slowApps}} {
		command(t, fixture.w, Action{Kind: SelectApplication, ApplicationKey: fixture.apps.surfaces[1].Key, Additive: true})
		command(t, fixture.w, Action{Kind: GroupApplications})
		depthThrowGesture(t, fixture.w, fixture.apps.surfaces[0])
	}
	fastBefore := fast.windowThrow.origin
	for i := 0; i < 40; i++ {
		fast.Update(20 * time.Millisecond)
	}
	for i := 0; i < 10; i++ {
		slow.Update(80 * time.Millisecond)
	}
	if fast.Document() != slow.Document() {
		t.Fatal("rear-wall collision changed with update granularity")
	}
	if fast.energyNet.accumulator != slow.energyNet.accumulator || fast.energyNet.activeTicks != slow.energyNet.activeTicks || !reflect.DeepEqual(fast.energyNet.points, slow.energyNet.points) {
		t.Fatal("wall deformation changed with update granularity around the timed impact")
	}
	placements := fast.Document().View.Application.Layouts
	if placements[0].Depth-placements[1].Depth != fastBefore[0].Depth-fastBefore[1].Depth ||
		placements[0].X-placements[1].X != fastBefore[0].X-fastBefore[1].X ||
		placements[0].Y-placements[1].Y != fastBefore[0].Y-fastBefore[1].Y {
		t.Fatal("group collision distorted the windows' spatial arrangement")
	}
}

func TestEnergyNetSecondImpactDoesNotTeleportExistingDent(t *testing.T) {
	n := newEnergyNet()
	n.impact(.25, .35, 1)
	n.update(180 * time.Millisecond)
	viewport := scene.Viewport{Width: 1400, Height: 900}
	before := make([][2]float32, len(n.points))
	for i := range n.points {
		before[i][0], before[i][1] = n.screenPoint(i, viewport)
	}
	n.impact(.8, .7, .7)
	for i := range n.points {
		x, y := n.screenPoint(i, viewport)
		if before[i] != [2]float32{x, y} {
			t.Fatal("changing the impact location teleported existing deformation")
		}
	}
}

func TestEnergyPulseAndWallPhaseAreSeamless(t *testing.T) {
	for _, position := range []float64{0, .17, .63, 1.4} {
		if energyPulse(position, 0) != energyPulse(position, 3) || energyPulse(position, .18) != energyPulse(position, .18-2) {
			t.Fatal("integer electrical circuits did not close at the phase seam")
		}
	}
	n := newEnergyNet()
	n.phase = 12*time.Second - time.Nanosecond
	n.update(time.Nanosecond)
	if n.phase != 0 {
		t.Fatal("energy-wall phase did not wrap exactly")
	}
}

func TestEnergyNetRejectsInvalidImpactAndBoundsExtremeMotion(t *testing.T) {
	n := newEnergyNet()
	before := *n
	for _, impact := range [][3]float32{{float32(math.NaN()), .5, 1}, {.5, float32(math.Inf(1)), 1}, {.5, .5, 0}, {.5, .5, -1}} {
		n.impact(impact[0], impact[1], impact[2])
	}
	if *n != before {
		t.Fatal("invalid impact changed wall state")
	}
	for i := 0; i < 32; i++ {
		n.impact(float32(i%7)/6, float32(i%5)/4, 1000)
		n.update(25 * time.Millisecond)
	}
	for _, point := range n.points {
		for _, value := range [...]float32{point.position.X, point.position.Y, point.position.Z, point.velocity.X, point.velocity.Y, point.velocity.Z} {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				t.Fatal("repeated extreme impacts made wall state non-finite")
			}
		}
		if point.position.Sub(point.rest).Length() > .42001 {
			t.Fatal("extreme impact escaped the physical displacement guardrail")
		}
		if point.pinned && (point.position != point.rest || point.velocity != (scene.Vec3{})) {
			t.Fatal("extreme impacts moved a pinned edge")
		}
	}
	n.update(time.Duration(math.MaxInt64))
	if n.activeTicks != 0 {
		t.Fatal("maximum elapsed duration did not settle in bounded work")
	}
}

func TestHeldWheelCollisionDoesNotLaunchSecondInwardThrow(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 1)
	x, y := windowGripPoint(t, w, apps.surfaces[0])
	for _, event := range []experience.Event{
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y, Time: 1000},
		{Kind: experience.PointerScroll, X: x, Y: y, ScrollY: 200, Time: 1020},
		{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x, Y: y, Time: 1040},
	} {
		if !w.Handle(event) {
			t.Fatal("held wheel collision gesture was not consumed")
		}
	}
	if w.energyNet.impactCount != 1 || w.windowThrow != nil || w.Document().View.Application.Layouts[0].Depth < energyWallDepth+energyWindowClearance {
		t.Fatal("releasing an already-reflected wheel move caused a second wall throw")
	}
}

func TestSingleWheelPacketAfterLongHoldStartsDepthThrow(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 1)
	x, y := windowGripPoint(t, w, apps.surfaces[0])
	for _, event := range []experience.Event{
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y, Time: 1000},
		// The long pause deliberately ages out the pointer-down sample. A single
		// discrete wheel packet must still provide a usable depth impulse.
		{Kind: experience.PointerScroll, X: x, Y: y, ScrollY: 24, Time: 1600},
		{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x, Y: y, Time: 1620},
	} {
		if !w.Handle(event) {
			t.Fatal("single-packet depth gesture was not consumed")
		}
	}
	if w.windowThrow == nil || w.windowThrow.vz >= 0 || w.windowThrow.wallCollisionTime < 0 {
		t.Fatal("one wheel packet after a held pause did not start a rearward depth throw")
	}
	w.Update(500 * time.Millisecond)
	if w.energyNet.impactCount != 1 || w.Document().View.Application.Layouts[0].Depth < energyWallDepth+energyWindowClearance {
		t.Fatal("single-packet depth throw did not collide with and rebound from the wall")
	}
}

func TestDesktopLoadMigratesLegacyPlacementsInFrontOfEnergyWall(t *testing.T) {
	w, _ := windowThrowWorkspace(t, 2)
	first, second := &w.m.applicationState.Layouts[0], &w.m.applicationState.Layouts[1]
	first.Group, second.Group = 1, 1
	first.Depth, second.Depth = -12, -10.5
	w.m.applicationState.aliases()
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	loaded := desktop(t)
	if err := loaded.LoadState(data); err != nil {
		t.Fatal(err)
	}
	placements := loaded.Document().View.Application.Layouts
	limit := energyWallDepth + energyWindowClearance
	if abs(placements[0].Depth-limit) > .00001 || abs((placements[1].Depth-placements[0].Depth)-1.5) > .00001 {
		t.Fatalf("legacy group was not translated intact in front of the wall: %g %g", placements[0].Depth, placements[1].Depth)
	}
	if !loaded.Document().View.Application.Behind {
		t.Fatal("legacy wall migration did not refresh active-placement aliases")
	}
	var malformed desktopDocument
	if err := json.Unmarshal(data, &malformed); err != nil {
		t.Fatal(err)
	}
	malformed.Applications.Layouts[0].Depth = -41
	invalid, err := json.Marshal(malformed)
	if err != nil {
		t.Fatal(err)
	}
	before := loaded.Document()
	if err := loaded.LoadState(invalid); err == nil || loaded.Document() != before {
		t.Fatal("wall migration repaired and accepted an historically invalid saved depth")
	}
}

func TestStandaloneWorkspaceHasNoInvisibleEnergyWall(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	depthThrowGesture(t, w, apps.surfaces[0])
	if w.energyNet != nil || w.windowThrow == nil || w.windowThrow.wallCollisionTime >= 0 {
		t.Fatal("standalone AXIAL unexpectedly enabled the desktop energy wall")
	}
	w.Update(600 * time.Millisecond)
	if w.Document().View.Application.Layouts[0].Depth >= energyWallDepth+energyWindowClearance {
		t.Fatal("standalone depth throw bounced from an invisible desktop wall")
	}
}

func TestSideBoundaryWinsBeforeRearWallAtEveryFrameRate(t *testing.T) {
	setup := func(t *testing.T) (*Workspace, *windowThrow) {
		w, apps := windowThrowWorkspace(t, 1)
		i := w.m.applicationState.index(apps.surfaces[0].Key)
		w.m.applicationState.Layouts[i].X = 99.9
		w.m.applicationState.Layouts[i].Depth = -3
		origin := w.m.applicationState.Layouts
		motion := &windowThrow{origin: origin, selected: 1 << i, vx: 10, vz: -10, duration: 2}
		motion.liveIDs[i] = apps.surfaces[0].ID
		motion.wallCollisionTime, motion.wallCollisionSpeed = energyWallCollision(origin, motion.selected, motion.vz)
		w.windowThrow = motion
		return w, motion
	}
	coarse, coarseMotion := setup(t)
	fine, fineMotion := setup(t)
	coarse.Update(time.Second)
	for i := 0; i < 50; i++ {
		fine.Update(20 * time.Millisecond)
	}
	if coarse.Document() != fine.Document() || coarse.energyNet.impactCount != 0 || fine.energyNet.impactCount != 0 || coarseMotion.wallImpactSent || fineMotion.wallImpactSent {
		t.Fatal("a rear-wall impact was reported after the side boundary had already stopped the throw")
	}
}

func TestFrontDepthBoundaryStopsThrowAtEveryFrameRate(t *testing.T) {
	setup := func(t *testing.T) *Workspace {
		w, apps := windowThrowWorkspace(t, 1)
		i := w.m.applicationState.index(apps.surfaces[0].Key)
		w.m.applicationState.Layouts[i].Depth = 39
		origin := w.m.applicationState.Layouts
		motion := &windowThrow{origin: origin, selected: 1 << i, vz: 10, duration: 2, wallCollisionTime: -1}
		motion.liveIDs[i] = apps.surfaces[0].ID
		w.windowThrow = motion
		return w
	}
	coarse, fine := setup(t), setup(t)
	coarse.Update(time.Second)
	for i := 0; i < 50; i++ {
		fine.Update(20 * time.Millisecond)
	}
	if coarse.Document() != fine.Document() || coarse.windowThrow != nil || fine.windowThrow != nil {
		t.Fatal("front depth boundary changed the resting throw with frame granularity")
	}
	if depth := coarse.Document().View.Application.Layouts[0].Depth; abs(depth-40) > .00001 {
		t.Fatalf("forward throw did not stop at the depth bound: %g", depth)
	}
}

func TestStandaloneRearDepthBoundaryStopsThrowAtEveryFrameRate(t *testing.T) {
	setup := func(t *testing.T) *Workspace {
		w, apps := multipleApplications(t, 1)
		w.m.playing = false
		i := w.m.applicationState.index(apps.surfaces[0].Key)
		w.m.applicationState.Layouts[i].Depth = -39
		origin := w.m.applicationState.Layouts
		motion := &windowThrow{origin: origin, selected: 1 << i, vz: -10, duration: 2, wallCollisionTime: -1}
		motion.liveIDs[i] = apps.surfaces[0].ID
		w.windowThrow = motion
		return w
	}
	coarse, fine := setup(t), setup(t)
	coarse.Update(time.Second)
	for i := 0; i < 50; i++ {
		fine.Update(20 * time.Millisecond)
	}
	if coarse.Document() != fine.Document() || coarse.windowThrow != nil || fine.windowThrow != nil {
		t.Fatalf("standalone rear depth boundary changed with frame granularity: coarse=%g fine=%g coarse-moving=%t fine-moving=%t",
			coarse.Document().View.Application.Layouts[0].Depth, fine.Document().View.Application.Layouts[0].Depth,
			coarse.windowThrow != nil, fine.windowThrow != nil)
	}
	if depth := coarse.Document().View.Application.Layouts[0].Depth; abs(depth+40) > .00001 {
		t.Fatalf("standalone rearward throw did not stop at the document bound: %g", depth)
	}
}

func TestWideGroupReboundHitsWallBeforeFrontLimitAtEveryFrameRate(t *testing.T) {
	setup := func(t *testing.T) *Workspace {
		w, apps := windowThrowWorkspace(t, 2)
		first := w.m.applicationState.index(apps.surfaces[0].Key)
		second := w.m.applicationState.index(apps.surfaces[1].Key)
		w.m.applicationState.Layouts[first].Depth = -3
		w.m.applicationState.Layouts[second].Depth = 39
		origin := w.m.applicationState.Layouts
		selected := uint32(1<<first | 1<<second)
		motion := &windowThrow{origin: origin, selected: selected, vz: -10, duration: 2}
		motion.wallCollisionTime, motion.wallCollisionSpeed = energyWallCollision(origin, selected, motion.vz)
		motion.liveIDs[first], motion.liveIDs[second] = apps.surfaces[0].ID, apps.surfaces[1].ID
		w.windowThrow = motion
		return w
	}
	coarse, fine := setup(t), setup(t)
	coarse.Update(2 * time.Second)
	for i := 0; i < 100; i++ {
		fine.Update(20 * time.Millisecond)
	}
	if coarse.Document() != fine.Document() || coarse.windowThrow != nil || fine.windowThrow != nil {
		t.Fatal("wide-group wall rebound changed with frame granularity")
	}
	if coarse.energyNet.impactCount != 1 || fine.energyNet.impactCount != 1 {
		t.Fatal("front depth limit suppressed or duplicated the earlier rear-wall impact")
	}
	placements := coarse.Document().View.Application.Layouts
	if abs(placements[1].Depth-40) > .00001 || abs((placements[1].Depth-placements[0].Depth)-42) > .00001 {
		t.Fatal("front depth stop distorted the rebounding group's arrangement")
	}
}

func TestSuperWheelCannotTunnelThroughEnergyWall(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 1)
	x, y := visibleApplication(t, w, apps.surfaces[0])
	beforeImpacts := w.energyNet.impactCount
	if !w.Handle(experience.Event{Kind: experience.PointerScroll, Modifiers: experience.ModSuper, X: x, Y: y, ScrollY: 200}) {
		t.Fatal("Super+wheel depth impact was not consumed")
	}
	placement := w.Document().View.Application.Layouts[0]
	if placement.Depth < energyWallDepth+energyWindowClearance || w.energyNet.impactCount != beforeImpacts+1 {
		t.Fatal("direct depth gesture tunneled through the wall or failed to excite it")
	}
}

func TestNonFiniteDepthWheelIsIgnored(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 1)
	view := w.m.applicationState
	for _, delta := range []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))} {
		if constrained, collision := energyWallDepthDelta(view, 1, delta); constrained != 0 || collision {
			t.Fatal("non-finite depth delta became a wall movement or collision")
		}
	}
	x, y := windowGripPoint(t, w, apps.surfaces[0])
	before := w.Document()
	for _, event := range []experience.Event{
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y, Time: 1000},
		{Kind: experience.PointerScroll, X: x, Y: y, ScrollY: float32(math.Inf(1)), Time: 1020},
		{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x, Y: y, Time: 1040},
	} {
		if !w.Handle(event) {
			t.Fatal("held non-finite wheel event was not safely consumed")
		}
	}
	if w.Document() != before || w.windowThrow != nil || w.energyNet.impactCount != 0 {
		t.Fatal("non-finite wheel input changed placement, launched motion, or hit the wall")
	}
}

func TestUntimedWheelMovesDepthWithoutManufacturingThrowVelocity(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 1)
	x, y := windowGripPoint(t, w, apps.surfaces[0])
	before, history := w.Document(), w.historyPosition
	for _, event := range []experience.Event{
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y},
		{Kind: experience.PointerScroll, X: x, Y: y, ScrollY: 24},
		{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x, Y: y},
	} {
		if !w.Handle(event) {
			t.Fatal("untimed depth gesture was not consumed")
		}
	}
	after := w.Document()
	if after.View.Application.Layouts[0].Depth == before.View.Application.Layouts[0].Depth || w.windowThrow != nil || w.historyPosition != history+1 {
		t.Fatal("untimed wheel failed to commit depth once or manufactured throw velocity")
	}
}

func TestDepthWheelRetainsMomentumAcrossTimestampWrap(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 1)
	x, y := windowGripPoint(t, w, apps.surfaces[0])
	for _, event := range []experience.Event{
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y, Time: math.MaxUint32 - 10},
		{Kind: experience.PointerScroll, X: x, Y: y, ScrollY: 24, Time: 0},
		{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x, Y: y, Time: 20},
	} {
		if !w.Handle(event) {
			t.Fatal("wrapped depth gesture was not consumed")
		}
	}
	if w.windowThrow == nil || w.windowThrow.vz >= 0 || w.windowThrow.wallCollisionTime < 0 {
		t.Fatal("uint32 timestamp wrap suppressed valid depth momentum")
	}
}

func TestExtremeSuperWheelReboundRemainsValid(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 1)
	x, y := visibleApplication(t, w, apps.surfaces[0])
	if !w.Handle(experience.Event{Kind: experience.PointerScroll, Modifiers: experience.ModSuper, X: x, Y: y, ScrollY: 10000}) {
		t.Fatal("extreme finite depth gesture was not consumed")
	}
	placement := w.Document().View.Application.Layouts[0]
	if placement.Depth < energyWallDepth+energyWindowClearance || placement.Depth > 40 || w.Document().Validate() != nil || w.energyNet.impactCount != 1 {
		t.Fatal("extreme finite depth rebound produced invalid placement or impact state")
	}
	view := w.m.applicationState
	delta := energyWallDepth + energyWindowClearance - view.Layouts[0].Depth
	if constrained, collision := energyWallDepthDelta(view, 1, delta); !collision || abs(constrained-delta) > .00001 {
		t.Fatalf("landing exactly on the wall did not register contact: delta=%g constrained=%g collision=%t", delta, constrained, collision)
	}
}
