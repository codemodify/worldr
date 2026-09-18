package workspace

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/presentation"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

func glowGeometryDraw(frame render.Frame, geometry *render.Geometry) (render.Draw, bool) {
	for _, command := range frame.Commands {
		for _, draw := range command.Draws {
			if draw.Geometry == geometry {
				return draw, true
			}
		}
	}
	return render.Draw{}, false
}

func TestHousingGlowAccentRetainsGeometryAndFollowsParentExplosion(t *testing.T) {
	w := study(t)
	command(t, w, Action{Kind: SetPlayback, Enabled: false})
	command(t, w, Action{Kind: SetReducedMotion, Enabled: true})
	accent, housing := w.scene.Node(w.accentNode), w.scene.Node(w.nodes[0])
	geometry := accent.Mesh.Geometry()
	children := w.scene.Children(w.nodes[0])
	if len(children) != 2 || children[0] != w.accentNode || children[1] != w.hologramNode || !accent.Unlit || !accent.Unpickable || !accent.DepthReadOnly {
		t.Fatal("housing accent lost retained parent ownership or decorative input/depth policy")
	}
	initial, ok := glowGeometryDraw(w.Draw(1440, 900), geometry)
	if !ok || initial.Glow == [3]float32{} || initial.Model != [16]float32(housing.Transform.Mul(accent.Transform)) {
		t.Fatal("cinematic housing accent did not draw in its parent's space")
	}
	command(t, w, Action{Kind: SetExploded, Enabled: true})
	exploded, ok := glowGeometryDraw(w.Draw(1440, 900), geometry)
	if !ok || exploded.Model == initial.Model || exploded.Model != [16]float32(housing.Transform.Mul(accent.Transform)) || accent.Mesh.Geometry() != geometry {
		t.Fatal("explosion left the accent behind or rebuilt its geometry")
	}
	scaled, ok := glowGeometryDraw(w.Draw(2880, 1800), geometry)
	if !ok || scaled.Model != exploded.Model || scaled.Glow != exploded.Glow || scaled.Geometry != initial.Geometry {
		t.Fatal("viewport resize changed the retained world-space accent")
	}
	command(t, w, Action{Kind: SetExploded, Enabled: false})
	restored, ok := glowGeometryDraw(w.Draw(1440, 900), geometry)
	if !ok || restored.Model != initial.Model || restored.Glow != initial.Glow {
		t.Fatal("returning from explosion did not restore the authored accent instance")
	}
}

func TestHousingGlowAccentDoesNotInterceptActualSurfacePicks(t *testing.T) {
	w := study(t)
	w.Draw(1440, 900)
	accent := w.scene.Node(w.accentNode)
	geometry := accent.Mesh.Geometry()
	vertices, indices := geometry.Vertices(), geometry.Indices()
	transform := w.scene.Node(w.nodes[0]).Transform.Mul(accent.Transform)
	intersections := 0
	for triangle := 0; triangle < len(indices); triangle += 12 {
		center := scene.Vec3{}
		for _, index := range indices[triangle : triangle+3] {
			vertex := vertices[index]
			center = center.Add(scene.Vec3{X: vertex.X, Y: vertex.Y, Z: vertex.Z}.Mul(1.0 / 3))
		}
		x, y, _, visible := w.camera.Project(transform.TransformPoint(center), w.viewport)
		if !visible {
			continue
		}
		// Confirm these rays would hit the accent if it were interactive;
		// testing only empty background would not exercise interception.
		accent.Unpickable = false
		probe, hitAccent := w.scene.Pick(w.camera, w.viewport, x, y)
		accent.Unpickable = true
		if !hitAccent || probe.Node != w.accentNode {
			continue
		}
		intersections++
		actual, actualHit := w.scene.Pick(w.camera, w.viewport, x, y)
		accent.Hidden = true
		withoutAccent, withoutHit := w.scene.Pick(w.camera, w.viewport, x, y)
		accent.Hidden = false
		if actualHit != withoutHit || actual != withoutAccent || actual.Node == w.accentNode {
			t.Fatal("authored accent changed the underlying scene's pick result")
		}
	}
	if intersections == 0 {
		t.Fatal("fixture did not exercise a visible accent intersection")
	}
}

func TestWorkspaceGlowGuidesHideInReadingOverviewAndAdaptiveFocus(t *testing.T) {
	w, _ := multipleApplications(t, 1)
	accentGeometry := w.scene.Node(w.accentNode).Mesh.Geometry()
	stageGeometry := w.scene.Node(w.stageNode).Mesh.Geometry()
	check := func(visible bool) {
		t.Helper()
		frame := w.Draw(1440, 900)
		for _, geometry := range []*render.Geometry{accentGeometry, stageGeometry} {
			draw, found := glowGeometryDraw(frame, geometry)
			if found != visible || found && draw.Glow == [3]float32{} {
				t.Fatalf("authored workspace guide visibility/emission: visible=%v found=%v glow=%v", visible, found, draw.Glow)
			}
		}
	}
	check(true)
	command(t, w, Action{Kind: ToggleApplicationOverview})
	check(false)
	command(t, w, Action{Kind: ToggleApplicationOverview})
	check(true)
	command(t, w, Action{Kind: ToggleApplicationReading})
	check(false)
	command(t, w, Action{Kind: ToggleApplicationReading})
	check(true)

	w.SetApplications(nil)
	command(t, w, Action{Kind: SetPlayback, Enabled: false})
	command(t, w, Action{Kind: SetPresentation, Presentation: presentation.Adaptive})
	command(t, w, Action{Kind: SetFocused, Enabled: true})
	w.Update(time.Second)
	check(false)
	if w.scene.Node(w.accentNode).Glow != [3]float32{} || w.scene.Node(w.stageNode).Glow != [3]float32{} {
		t.Fatal("focused Adaptive view retained emitter constants")
	}
	command(t, w, Action{Kind: SetFocused, Enabled: false})
	w.Update(time.Second)
	check(true)
	if w.scene.Node(w.accentNode).Mesh.Geometry() != accentGeometry || w.scene.Node(w.stageNode).Mesh.Geometry() != stageGeometry {
		t.Fatal("quiet-mode transitions recreated retained guides")
	}
}
