package workspace

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/presentation"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

func frameDraws(w *Workspace, frame render.Frame) []render.Draw {
	var draws []render.Draw
	geometries := make(map[*render.Geometry]bool)
	for _, surface := range w.applicationSurfaces {
		geometries[w.frameMeshFor(surface).Geometry()] = true
	}
	for _, command := range frame.Commands {
		for _, draw := range command.Draws {
			if geometries[draw.Geometry] {
				draws = append(draws, draw)
			}
		}
	}
	return draws
}

func TestApplicationFramesShowIndependentFocusSelectionHoverAndIdle(t *testing.T) {
	w, apps := multipleApplications(t, 4)
	apps.surfaces[0].AppID = "worldr.native-terminal"
	apps.surfaces[1].AppID = "foot"
	// Keep the second app selected while making the first active. Clicking
	// that active app then grants keyboard focus without changing selection.
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[1].Key, Additive: true})
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[0].Key, Additive: true})
	w.Draw(1440, 900)
	x, y := visibleApplication(t, w, apps.surfaces[0])
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	x, y = visibleApplication(t, w, apps.surfaces[2])
	pointer(w, experience.PointerMove, x, y)
	if w.applicationFocusedID != apps.surfaces[0].ID || w.applicationHoveredID != apps.surfaces[2].ID || !w.OwnsKeyboard() {
		t.Fatal("fixture did not establish independent focus and hover")
	}
	events, focusCalls, textureRevision := len(apps.events), len(apps.focus), apps.surfaces[0].Texture.Revision()
	document := w.Document()
	draws := frameDraws(w, w.Draw(1440, 900))
	if len(draws) != 4 {
		t.Fatalf("expected one shared frame instance per app, got %d", len(draws))
	}
	var colors [4]scene.Color
	var emissions [4][3]float32
	for i, surface := range apps.surfaces {
		frameID := w.applicationFrames[surface.ID]
		border := w.scene.Node(frameID)
		if border == nil || border.Mesh != w.frameMeshFor(surface) || !border.Unlit || !border.Unpickable || !border.DepthReadOnly {
			t.Fatal("application frame lost shared geometry or safe depth/input policy")
		}
		children := w.scene.Children(w.applicationNodes[surface.ID])
		gripID := w.applicationDragHandles[surface.ID]
		if len(children) != 3 || children[0] != frameID || children[1] != gripID || children[2] != w.applicationResizeHandles[surface.ID] ||
			len(w.scene.Children(gripID)) != 1 || w.scene.Children(gripID)[0] != w.applicationWindowControls[surface.ID] {
			t.Fatal("application frame is not owned by its content node")
		}
		colors[i] = border.Color
		emissions[i] = border.Glow
		if draws[i].Glow != border.Glow {
			t.Fatal("frame instance lost its authored glow")
		}
		if draws[i].Model != [16]float32(w.scene.Node(w.applicationNodes[surface.ID]).Transform) || draws[i].Color != [4]float32{border.Color.R, border.Color.G, border.Color.B, border.Color.A} {
			t.Fatal("frame did not inherit its application's transform and own color")
		}
	}
	if !(colors[0].A > colors[1].A && colors[1].A > colors[2].A && colors[2].A > colors[3].A && colors[3].A > 0) {
		t.Fatalf("focus/selection/hover/idle states are not visibly distinguished: %+v", colors)
	}
	if colors[0].G <= colors[0].R || colors[0].B <= colors[0].R || colors[1].G <= colors[1].R {
		t.Fatal("focus and selection lost their cyan cue")
	}
	energy := func(value [3]float32) float32 { return value[0] + value[1] + value[2] }
	if !(energy(emissions[0]) > energy(emissions[1]) && energy(emissions[1]) > energy(emissions[2]) && energy(emissions[2]) > 0) || emissions[3] != [3]float32{} {
		t.Fatalf("focus/selection/hover glow did not remain distinct from nonemitting idle: %v", emissions)
	}
	for _, command := range w.Draw(1440, 900).Commands {
		for _, draw := range command.Draws {
			if draw.Texture != nil && draw.Glow != [3]float32{} {
				t.Fatal("authored frame glow leaked into a content texture")
			}
		}
	}
	if len(apps.events) != events || len(apps.focus) != focusCalls || w.Document() != document || apps.surfaces[0].Texture.Revision() != textureRevision {
		t.Fatal("drawing focus frames changed app input, document state, or pixels")
	}
	pointer(w, experience.PointerCancel, 0, 0)
	w.Draw(1440, 900)
	if got := w.scene.Node(w.applicationFrames[apps.surfaces[2].ID]).Color; got != colors[3] {
		t.Fatal("cancelled hover left the application edge highlighted")
	}
	if w.scene.Node(w.applicationFrames[apps.surfaces[0].ID]).Color != colors[0] || !w.OwnsKeyboard() {
		t.Fatal("pointer cancellation changed the keyboard-focus cue or ownership")
	}
	if w.scene.Node(w.applicationFrames[apps.surfaces[2].ID]).Glow != [3]float32{} || w.scene.Node(w.applicationFrames[apps.surfaces[0].ID]).Glow != emissions[0] {
		t.Fatal("pointer cancellation retained hover emission or changed independent focus emission")
	}
}

func TestApplicationFrameGeometryStaysOutsideContentAndCannotCaptureInput(t *testing.T) {
	w, apps := applicationStudy(t)
	geometry := w.applicationFrameMesh.Geometry()
	vertices, indices := geometry.Vertices(), geometry.Indices()
	minZ, maxZ := float32(1), float32(-1)
	wallTriangles := 0
	for triangle := 0; triangle < len(indices); triangle += 3 {
		minX, maxX := float32(1), float32(-1)
		minY, maxY := float32(1), float32(-1)
		triangleMinZ, triangleMaxZ := float32(1), float32(-1)
		for _, index := range indices[triangle : triangle+3] {
			vertex := vertices[index]
			minX, maxX = min(minX, vertex.X), max(maxX, vertex.X)
			minY, maxY = min(minY, vertex.Y), max(maxY, vertex.Y)
			minZ, maxZ = min(minZ, vertex.Z), max(maxZ, vertex.Z)
			triangleMinZ, triangleMaxZ = min(triangleMinZ, vertex.Z), max(triangleMaxZ, vertex.Z)
			if vertex.Z > 0 {
				t.Fatal("frame depth extended in front of application pixels")
			}
		}
		if triangleMinZ < triangleMaxZ {
			wallTriangles++
		}
		if !(maxX < -.5 || minX > .5 || maxY < -.5 || minY > .5) {
			t.Fatal("frame triangle covers application content")
		}
	}
	if minZ >= -.1 || maxZ != 0 || wallTriangles == 0 {
		t.Fatalf("frame did not retain a front face with visible rearward walls: z=%g..%g walls=%d", minZ, maxZ, wallTriangles)
	}
	command(t, w, Action{Kind: ToggleApplicationReading})
	w.Draw(1440, 900)
	parent := w.scene.Node(w.applicationNode)
	point := parent.Transform.TransformPoint(scene.Vec3{X: .5035})
	x, y, _, visible := w.camera.Project(point, w.viewport)
	if !visible {
		t.Fatal("frame fixture is outside the reading viewport")
	}
	if hit, ok := w.scene.Pick(w.camera, w.viewport, x, y); ok {
		t.Fatalf("visual frame intercepted a scene pick: %+v", hit)
	}
	count := len(apps.events)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if w.OwnsKeyboard() || len(apps.events) != count {
		t.Fatal("clicking a visual frame sent input to the application")
	}
	x, y = visibleApplicationPoint(t, w)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if !w.OwnsKeyboard() {
		t.Fatal("frame prevented normal content click-to-type")
	}
}

func TestApplicationFramesRetainResourcesAcrossViewResizeAndLifecycle(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	for i := range apps.surfaces {
		apps.surfaces[i].AppID = "worldr.generic-test"
	}
	w.Draw(1440, 900)
	geometry := w.applicationFrameMesh.Geometry()
	firstID, secondID := apps.surfaces[0].ID, apps.surfaces[1].ID
	firstFrame, secondFrame := w.applicationFrames[firstID], w.applicationFrames[secondID]
	firstRoot := w.applicationNodes[firstID]
	command(t, w, Action{Kind: MoveApplications, DeltaX: .4, DeltaY: -.2, DeltaDepth: .5})
	if err := apps.surfaces[0].Texture.Replace(600, 800, make([]byte, 600*800*4)); err != nil {
		t.Fatal(err)
	}
	for _, reading := range []bool{false, true} {
		if reading {
			command(t, w, Action{Kind: ToggleApplicationReading})
		}
		draws := frameDraws(w, w.Draw(1440, 900))
		want := 2
		if reading {
			want = 1
		}
		if len(draws) != want || w.applicationFrames[firstID] != firstFrame || w.applicationFrames[secondID] != secondFrame {
			t.Fatal("read/resize/placement changed frame identity or failed to inherit hiding")
		}
		for _, draw := range draws {
			if draw.Geometry != geometry {
				t.Fatal("frame geometry was recreated during view or extent changes")
			}
		}
	}
	apps.surfaces = apps.surfaces[1:]
	w.Update(0)
	if w.scene.Node(firstRoot) != nil || w.scene.Node(firstFrame) != nil || w.applicationFrames[firstID] != 0 {
		t.Fatal("removed content left its child frame or bookkeeping behind")
	}
	if got := frameDraws(w, w.Draw(1440, 900)); len(got) != 1 || got[0].Geometry != geometry {
		t.Fatal("closing a sibling removed or recreated the remaining frame")
	}
	apps.surfaces = nil
	w.Update(0)
	if w.scene.Node(secondFrame) != nil || len(w.applicationFrames) != 0 || len(frameDraws(w, w.Draw(1440, 900))) != 0 {
		t.Fatal("last application close retained a visible frame")
	}
	texture, err := render.NewTexture(320, 200, make([]byte, 320*200*4))
	if err != nil {
		t.Fatal(err)
	}
	replacement := &fakeApplications{surfaces: []experience.ApplicationSurface{{ID: 901, Key: "replacement", Texture: texture}}}
	w.SetApplications(replacement)
	if got := frameDraws(w, w.Draw(1440, 900)); len(got) != 1 || got[0].Geometry != geometry {
		t.Fatal("reopened application did not reuse the workspace's border mesh")
	}
	newFrame := w.applicationFrames[901]
	w.SetApplications(nil)
	if w.scene.Node(newFrame) != nil || len(w.applicationFrames) != 0 {
		t.Fatal("provider replacement left a frame child behind")
	}
}

func TestAdaptiveReadingKeepsQuietFocusFrameWithoutChangingContent(t *testing.T) {
	w, apps := applicationStudy(t)
	command(t, w, Action{Kind: ToggleApplicationReading})
	w.Draw(1440, 900)
	x, y := visibleApplicationPoint(t, w)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	w.Draw(1440, 900)
	frameID := w.applicationFrames[777]
	bright := w.scene.Node(frameID).Color
	if w.scene.Node(frameID).Glow == [3]float32{} {
		t.Fatal("cinematic focused application did not emit its frame halo")
	}
	texture, revision := apps.surfaces[0].Texture, apps.surfaces[0].Texture.Revision()
	command(t, w, Action{Kind: SetPresentation, Presentation: presentation.Adaptive})
	w.Update(time.Second)
	frame := w.Draw(1440, 900)
	quiet := w.scene.Node(frameID).Color
	if quiet.A >= bright.A || quiet.A < .5 || !w.OwnsKeyboard() {
		t.Fatal("Adaptive reading removed its focus cue or did not quiet it")
	}
	for _, command := range frame.Commands {
		for _, draw := range command.Draws {
			if draw.Glow != [3]float32{} {
				t.Fatal("Adaptive reading retained a visible authored emitter")
			}
			if draw.Texture == texture && draw.Color != [4]float32{1, 1, 1, 1} {
				t.Fatal("focus frame modified the application's content tint/alpha")
			}
		}
	}
	if texture.Revision() != revision || w.scene.Node(w.applicationNode).Surface != texture {
		t.Fatal("focus cue changed or replaced the content image")
	}
	w.Handle(experience.Event{Kind: experience.KeyboardCancel})
	w.Draw(1440, 900)
	if w.scene.Node(frameID).Color.A >= quiet.A {
		t.Fatal("keyboard cancellation retained the focused frame state")
	}
}
