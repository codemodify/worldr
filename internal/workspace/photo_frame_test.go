package workspace

import (
	"bytes"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
)

func nativePhotoBracketWorkspace(t *testing.T) (*Workspace, *fakeApplications, experience.ApplicationSurface) {
	t.Helper()
	w, apps, _ := photoDragWorkspace(t, 1)
	apps.surfaces[0].Frameless = false
	w.Update(0)
	w.Draw(1440, 900)
	return w, apps, apps.surfaces[0]
}

func TestPhotoBracketRetainsGeometryAcrossViewsWithoutGripOrImageChanges(t *testing.T) {
	w, apps, photo := nativePhotoBracketWorkspace(t)
	// A portrait photo also exercises the DragContent-only spatial size bound.
	if err := photo.Texture.Replace(180, 600, bytes.Repeat([]byte{75, 120, 190, 255}, 180*600)); err != nil {
		t.Fatal(err)
	}
	w.Draw(1440, 900)
	_, _, width, height := w.applicationTransformFor(photo)
	if abs(width-.9) > .0001 || abs(height-3) > .0001 {
		t.Fatalf("native photo lost its bounded aspect: %gx%g", width, height)
	}
	original, _ := photo.Texture.Snapshot(0)
	revision := photo.Texture.Revision()
	frameID, gripID := w.applicationFrames[photo.ID], w.applicationDragHandles[photo.ID]
	bracket := w.scene.Node(frameID).Mesh
	if bracket == nil || bracket != w.photoFrameMesh || bracket == w.applicationFrameMesh || bracket == w.terminalFrameMesh {
		t.Fatal("native photo did not receive its dedicated open-bracket geometry")
	}
	// Toggle out of Overview back to Read, then back to Space. The same nodes
	// and mesh must survive both view changes and host viewport resizes.
	for i, action := range []ActionKind{"", ToggleApplicationReading, ToggleApplicationOverview, ToggleApplicationOverview, ToggleApplicationReading} {
		if action != "" {
			command(t, w, Action{Kind: action})
		}
		frame := w.Draw(1440-i*60, 900-i*20)
		border, grip := w.scene.Node(frameID), w.scene.Node(gripID)
		if w.applicationFrames[photo.ID] != frameID || w.applicationDragHandles[photo.ID] != gripID || border.Mesh != bracket || border.Hidden || !border.Unlit || !border.Unpickable || !border.DepthReadOnly || !grip.Hidden {
			t.Fatalf("view %d rebuilt, hid or made the bracket interactive, or exposed its grip", i)
		}
		if draws := frameDraws(w, frame); len(draws) != 1 || draws[0].Geometry != bracket.Geometry() {
			t.Fatalf("view %d did not submit exactly the retained photo bracket", i)
		}
		found := false
		for _, command := range frame.Commands {
			for _, draw := range command.Draws {
				if draw.Texture == photo.Texture {
					found = true
					if draw.Color != [4]float32{1, 1, 1, 1} || draw.Glow != [3]float32{} {
						t.Fatal("photo bracket tinted or added emission to the photograph")
					}
				}
			}
		}
		current, _ := photo.Texture.Snapshot(0)
		if !found || apps.surfaces[0].Texture != photo.Texture || photo.Texture.Revision() != revision || !bytes.Equal(current.Pixels, original.Pixels) {
			t.Fatal("bracket/view changes altered or replaced the photo's retained pixels")
		}
	}
}

func TestPhotoBracketStaysOutsidePixelsAndLeavesRightSideOpen(t *testing.T) {
	w, _, photo := nativePhotoBracketWorkspace(t)
	border := w.scene.Node(w.applicationFrames[photo.ID])
	geometry := border.Mesh.Geometry()
	vertices, indices := geometry.Vertices(), geometry.Indices()
	leftRail, topReturn, bottomReturn := false, false, false
	minZ, maxZ := float32(1), float32(-1)
	for i := 0; i < len(indices); i += 3 {
		triangle := make([]scene.Vec3, 3)
		minY, maxY, maxX := float32(1), float32(-1), float32(-1)
		for j, index := range indices[i : i+3] {
			vertex := vertices[index]
			if vertex.X >= 0 || vertex.Z > 0 {
				t.Fatal("photo bracket reached the right half or extended in front of the photo")
			}
			triangle[j] = scene.Vec3{X: vertex.X, Y: vertex.Y, Z: vertex.Z}
			minZ, maxZ = min(minZ, vertex.Z), max(maxZ, vertex.Z)
			minY, maxY, maxX = min(minY, vertex.Y), max(maxY, vertex.Y), max(maxX, vertex.X)
			topReturn = topReturn || vertex.X > -.4 && vertex.Y > .5
			bottomReturn = bottomReturn || vertex.X > -.4 && vertex.Y < -.5
		}
		leftRail = leftRail || maxX < -.5 && minY < 0 && maxY > 0
		if area := terminalTriangleContentArea(triangle); area > 1e-9 {
			t.Fatalf("photo bracket triangle %d covers photo pixels: %g", i/3, area)
		}
	}
	if !leftRail || !topReturn || !bottomReturn {
		t.Fatal("photo bracket lacks its left rail or one of its partial returns")
	}
	if minZ >= -.08 || maxZ != 0 {
		t.Fatalf("photo bracket lost its open rearward depth: %g..%g", minZ, maxZ)
	}
	command(t, w, Action{Kind: ToggleApplicationReading})
	w.Draw(1440, 900)
	parent := w.scene.Node(w.applicationNodes[photo.ID])
	for _, vertex := range vertices {
		x, y, _, visible := w.camera.Project(parent.Transform.TransformPoint(scene.Vec3{X: vertex.X, Y: vertex.Y, Z: vertex.Z}), w.viewport)
		if !visible || x < w.viewport.X || x > w.viewport.X+w.viewport.Width || y < w.viewport.Y || y > w.viewport.Y+w.viewport.Height {
			t.Fatal("Read camera clipped the open photo bracket")
		}
	}
	x, y, _, _ := w.camera.Project(parent.Transform.TransformPoint(scene.Vec3{X: -.525}), w.viewport)
	if hit, ok := w.scene.Pick(w.camera, w.viewport, x, y); ok && hit.Node == w.applicationFrames[photo.ID] {
		t.Fatal("decorative photo bracket intercepted a scene pick")
	}
	if surface, ok := w.applicationDragTarget(x, y); ok && surface.ID == photo.ID {
		t.Fatal("decorative bracket introduced an invisible drag grip outside the photo")
	}
}

func TestPhotoBracketPreservesContentDragAndUndoWithoutTouchingPixels(t *testing.T) {
	w, apps, photo := nativePhotoBracketWorkspace(t)
	x, y := visibleApplication(t, w, photo)
	before, history := w.Document(), w.historyPosition
	original, _ := photo.Texture.Snapshot(0)
	revision := photo.Texture.Revision()
	bracket := w.scene.Node(w.applicationFrames[photo.ID]).Mesh
	pointer(w, experience.PointerDown, x, y)
	if !w.pointer.windowDrag || w.OwnsKeyboard() {
		t.Fatal("photo bracket replaced direct image dragging with application input")
	}
	pointer(w, experience.PointerMove, x+45, y+20)
	pointer(w, experience.PointerUp, x+45, y+20)
	w.Draw(1440, 900)
	current, _ := photo.Texture.Snapshot(0)
	if w.Document().View.Application.Layouts == before.View.Application.Layouts || w.historyPosition != history+1 || len(apps.events) != 0 {
		t.Fatal("bracket changed photo dragging, its single undo entry or input ownership")
	}
	if photo.Texture.Revision() != revision || !bytes.Equal(current.Pixels, original.Pixels) || w.scene.Node(w.applicationFrames[photo.ID]).Mesh != bracket {
		t.Fatal("moving the photo changed its pixels or rebuilt its bracket")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("bracket prevented photo drag Undo from restoring placement")
	}
}
