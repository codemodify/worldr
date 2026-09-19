package workspace

import (
	"math"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
)

func terminalFrameWorkspace(t *testing.T, count int) (*Workspace, *fakeApplications) {
	t.Helper()
	w, apps := windowDragWorkspace(t, count)
	for i := range apps.surfaces {
		apps.surfaces[i].AppID = "worldr.native-terminal"
	}
	w.Draw(1440, 900)
	return w, apps
}

func TestCinematicFrameClassificationTracksMetadataWithoutReplacingNodes(t *testing.T) {
	w, apps := terminalFrameWorkspace(t, 1)
	appID := apps.surfaces[0].ID
	rootID, frameID, gripID := w.applicationNodes[appID], w.applicationFrames[appID], w.applicationDragHandles[appID]
	terminalMesh := w.scene.Node(frameID).Mesh
	terminalGrip := w.scene.Node(gripID).Mesh
	if terminalMesh == w.applicationFrameMesh || terminalGrip == w.applicationDragHandleMesh {
		t.Fatal("terminal did not receive distinct retained border and grip geometry")
	}
	for _, test := range []struct {
		appID     string
		cinematic bool
	}{
		{"worldr.project-browser", true},
		{"foot", true},
		{"footclient", true},
		{"org.kde.konsole", true},
		{"foot-not-a-terminal", false},
		{"", false},
		{"worldr.native-terminal", true},
	} {
		apps.surfaces[0].AppID = test.appID
		before, revision := w.Document(), apps.surfaces[0].Texture.Revision()
		w.Draw(1440, 900)
		border, grip := w.scene.Node(frameID), w.scene.Node(gripID)
		if w.applicationNodes[appID] != rootID || w.applicationFrames[appID] != frameID || w.applicationDragHandles[appID] != gripID {
			t.Fatal("changing AppID recreated application or decoration nodes")
		}
		if test.cinematic {
			if border.Mesh != terminalMesh || grip.Mesh != terminalGrip {
				t.Fatalf("cinematic identity %q did not reuse its retained style", test.appID)
			}
		} else if border.Mesh != w.applicationFrameMesh || grip.Mesh != w.applicationDragHandleMesh {
			t.Fatalf("unrecognized identity %q received terminal decorations", test.appID)
		}
		if !border.Unpickable || !border.DepthReadOnly || !border.Unlit || grip.Unpickable || len(w.scene.Children(rootID)) != 3 || len(w.scene.Children(gripID)) != 1 {
			t.Fatal("style switch changed decoration input/depth policy or ownership")
		}
		if before != w.Document() || apps.surfaces[0].Texture.Revision() != revision || len(apps.events) != 0 {
			t.Fatal("style metadata changed client pixels, document state or input")
		}
	}
}

// Clip a triangle against the content rectangle. Chamfer triangles can have
// overlapping bounding boxes while still leaving every client pixel untouched.
func terminalTriangleContentArea(triangle []scene.Vec3) float64 {
	polygon := append([]scene.Vec3(nil), triangle...)
	for _, boundary := range []struct {
		xAxis, greater bool
		value          float32
	}{{true, true, -.5}, {true, false, .5}, {false, true, -.5}, {false, false, .5}} {
		if len(polygon) == 0 {
			return 0
		}
		coordinate := func(point scene.Vec3) float32 {
			if boundary.xAxis {
				return point.X
			}
			return point.Y
		}
		inside := func(point scene.Vec3) bool {
			if boundary.greater {
				return coordinate(point) >= boundary.value
			}
			return coordinate(point) <= boundary.value
		}
		clipped := make([]scene.Vec3, 0, len(polygon)+1)
		previous := polygon[len(polygon)-1]
		for _, current := range polygon {
			if inside(previous) != inside(current) {
				fraction := (boundary.value - coordinate(previous)) / (coordinate(current) - coordinate(previous))
				clipped = append(clipped, previous.Add(current.Sub(previous).Mul(fraction)))
			}
			if inside(current) {
				clipped = append(clipped, current)
			}
			previous = current
		}
		polygon = clipped
	}
	var area float64
	for i, point := range polygon {
		next := polygon[(i+1)%len(polygon)]
		area += float64(point.X)*float64(next.Y) - float64(point.Y)*float64(next.X)
	}
	return math.Abs(area) / 2
}

func TestTerminalFrameAndGripLeaveWholeClientRectangleIntact(t *testing.T) {
	w, apps := terminalFrameWorkspace(t, 1)
	id := apps.surfaces[0].ID
	for _, fixture := range []struct {
		nodeID scene.NodeID
		depth  bool
	}{
		{w.applicationFrames[id], true},
		{w.applicationDragHandles[id], false},
	} {
		nodeID := fixture.nodeID
		geometry := w.scene.Node(nodeID).Mesh.Geometry()
		vertices, indices := geometry.Vertices(), geometry.Indices()
		if len(indices) == 0 || len(indices)%3 != 0 {
			t.Fatal("terminal decoration has invalid triangle geometry")
		}
		minZ, maxZ := float32(1), float32(-1)
		for i := 0; i < len(indices); i += 3 {
			triangle := make([]scene.Vec3, 3)
			for j, index := range indices[i : i+3] {
				vertex := vertices[index]
				triangle[j] = scene.Vec3{X: vertex.X, Y: vertex.Y, Z: vertex.Z}
				minZ, maxZ = min(minZ, vertex.Z), max(maxZ, vertex.Z)
				if !finite(float64(vertex.X)) || !finite(float64(vertex.Y)) || !finite(float64(vertex.Z)) {
					t.Fatal("terminal decoration contains nonfinite geometry")
				}
				if vertex.Z > 0 {
					t.Fatal("terminal decoration extended in front of the client")
				}
			}
			if area := terminalTriangleContentArea(triangle); area > 1e-9 {
				t.Fatalf("terminal decoration triangle %d covers client pixels: area %g", i/3, area)
			}
		}
		if fixture.depth && (minZ >= -.1 || maxZ != 0) {
			t.Fatalf("cinematic frame lacks retained rearward depth: %g..%g", minZ, maxZ)
		}
		if !fixture.depth && (minZ != 0 || maxZ != 0) {
			t.Fatalf("pickable terminal grip was extruded with the frame: %g..%g", minZ, maxZ)
		}
	}
	texture, revision := apps.surfaces[0].Texture, apps.surfaces[0].Texture.Revision()
	for _, command := range w.Draw(1440, 900).Commands {
		for _, draw := range command.Draws {
			if draw.Texture == texture && (draw.Glow != [3]float32{} || draw.Color != [4]float32{1, 1, 1, 1}) {
				t.Fatal("terminal border tinted the client image or made it emit glow")
			}
		}
	}
	x, y := visibleApplication(t, w, apps.surfaces[0])
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if !w.OwnsKeyboard() || len(apps.events) < 2 || apps.events[0].event.Kind != experience.PointerDown || texture.Revision() != revision {
		t.Fatal("terminal decoration intercepted normal client input or changed its pixels")
	}
}

func TestTerminalDecorationDoesNotInterceptScenePicks(t *testing.T) {
	w, apps := terminalFrameWorkspace(t, 1)
	frameID := w.applicationFrames[apps.surfaces[0].ID]
	border := w.scene.Node(frameID)
	parent := w.scene.Node(w.applicationNodes[apps.surfaces[0].ID])
	vertices, indices := border.Mesh.Geometry().Vertices(), border.Mesh.Geometry().Indices()
	intersections := 0
	for i := 0; i < len(indices); i += 3 {
		center := scene.Vec3{}
		for _, index := range indices[i : i+3] {
			vertex := vertices[index]
			center = center.Add(scene.Vec3{X: vertex.X, Y: vertex.Y, Z: vertex.Z}.Mul(1.0 / 3))
		}
		x, y, _, visible := w.camera.Project(parent.Transform.Mul(border.Transform).TransformPoint(center), w.viewport)
		if !visible {
			continue
		}
		border.Unpickable = false
		probe, hit := w.scene.Pick(w.camera, w.viewport, x, y)
		border.Unpickable = true
		if !hit || probe.Node != frameID {
			continue
		}
		intersections++
		actual, actualHit := w.scene.Pick(w.camera, w.viewport, x, y)
		border.Hidden = true
		without, withoutHit := w.scene.Pick(w.camera, w.viewport, x, y)
		border.Hidden = false
		if actualHit != withoutHit || actual != without {
			t.Fatal("visible decorative frame changed the underlying scene pick")
		}
	}
	if intersections == 0 {
		t.Fatal("test did not probe visible terminal frame geometry")
	}
}

func TestTerminalGripSupportsThrowAndForegroundOcclusion(t *testing.T) {
	w, apps := terminalFrameWorkspace(t, 1)
	command(t, w, Action{Kind: SetReducedMotion, Enabled: false})
	x, y := windowGripPoint(t, w, apps.surfaces[0])
	parent := w.scene.Node(w.applicationNodes[apps.surfaces[0].ID])
	blocker := w.scene.Add(0, scene.Node{Surface: apps.surfaces[0].Texture, Transform: parent.Transform.Mul(scene.Translate(0, .55, .1))})
	if hit, ok := w.scene.Pick(w.camera, w.viewport, x, y); !ok || hit.Node != blocker {
		t.Fatal("foreground fixture did not cover the terminal grip")
	}
	if _, ok := w.applicationDragTarget(x, y); ok {
		t.Fatal("terminal grip could be grabbed through foreground content")
	}
	w.scene.Remove(blocker)
	before := w.Document()
	startWindowThrow(t, w, apps.surfaces[0])
	w.Update(100 * time.Millisecond)
	if w.windowThrow == nil || w.Document().View.Application.Layouts == before.View.Application.Layouts || len(apps.events) != 0 {
		t.Fatal("terminal grip lost throw behavior or routed dragging into client content")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("terminal style changed the drag/throw undo contract")
	}
}

func TestTerminalFrameRetainsResourcesFitsReadingAndFollowsLifecycle(t *testing.T) {
	w, apps := terminalFrameWorkspace(t, 2)
	id := apps.surfaces[0].ID
	frameID, gripID := w.applicationFrames[id], w.applicationDragHandles[id]
	border, grip := w.scene.Node(frameID), w.scene.Node(gripID)
	frameMesh, gripMesh := border.Mesh, grip.Mesh
	command(t, w, Action{Kind: ToggleApplicationReading})
	w.Draw(1440, 900)
	parent := w.scene.Node(w.applicationNodes[id])
	for _, vertex := range frameMesh.Geometry().Vertices() {
		x, y, _, visible := w.camera.Project(parent.Transform.Mul(border.Transform).TransformPoint(scene.Vec3{X: vertex.X, Y: vertex.Y, Z: vertex.Z}), w.viewport)
		if !visible || x < w.viewport.X || x > w.viewport.X+w.viewport.Width || y < w.viewport.Y || y > w.viewport.Y+w.viewport.Height {
			t.Fatal("terminal border is clipped by the Read camera")
		}
	}
	if !grip.Hidden || len(frameDraws(w, w.Draw(1440, 900))) != 1 {
		t.Fatal("Read exposed the drag grip or a background terminal's border")
	}
	command(t, w, Action{Kind: ToggleApplicationOverview})
	w.Draw(1440, 900)
	if !grip.Hidden || len(frameDraws(w, w.Draw(1440, 900))) != 2 {
		t.Fatal("Overview failed to show both frames or retained a drag grip")
	}
	command(t, w, Action{Kind: ToggleApplicationOverview})
	command(t, w, Action{Kind: ToggleApplicationReading})
	if err := apps.surfaces[0].Texture.Replace(600, 800, make([]byte, 600*800*4)); err != nil {
		t.Fatal(err)
	}
	w.Draw(1200, 800)
	if border.Mesh != frameMesh || grip.Mesh != gripMesh || grip.Hidden {
		t.Fatal("resize/view changes rebuilt the terminal style or left its grip hidden")
	}
	apps.surfaces = apps.surfaces[1:]
	w.Update(0)
	if w.scene.Node(frameID) != nil || w.scene.Node(gripID) != nil || w.applicationFrames[id] != 0 || w.applicationDragHandles[id] != 0 {
		t.Fatal("closing a terminal left its frame or grip attached")
	}
	w.Draw(1200, 800)
	remaining := apps.surfaces[0].ID
	if w.scene.Node(w.applicationFrames[remaining]).Mesh != frameMesh || w.scene.Node(w.applicationDragHandles[remaining]).Mesh != gripMesh {
		t.Fatal("closing one terminal changed its sibling's retained style")
	}
	w.SetApplications(nil)
	if len(w.applicationFrames) != 0 || len(w.applicationDragHandles) != 0 || len(frameDraws(w, w.Draw(1200, 800))) != 0 {
		t.Fatal("provider replacement retained visible terminal chrome")
	}
}
