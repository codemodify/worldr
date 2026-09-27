package workspace

import (
	"bytes"
	"math"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func panelFieldWorkspace(t *testing.T) (*Workspace, *fakeApplications) {
	t.Helper()
	w, apps := windowDragWorkspace(t, 1)
	selected, err := skin.Builtin("future-panels")
	if err != nil {
		t.Fatal(err)
	}
	selected.ID = "custom.future-panel-field"
	if err := w.SetSkin(selected); err != nil {
		t.Fatal(err)
	}
	w.Draw(1440, 900)
	if !w.panelFieldVisible() || w.panelField == nil {
		t.Fatal("custom skin ID with panel-field trait failed to create retained scenery")
	}
	return w, apps
}

func panelFieldTextures(p *panelField) map[uint64]*render.Texture {
	textures := map[uint64]*render.Texture{}
	for _, id := range p.scene.Children(0) {
		if texture := p.scene.Node(id).Surface; texture != nil {
			textures[texture.ID()] = texture
		}
	}
	return textures
}

func TestPanelFieldRetainsImmutableTexturesAcrossViewportAndCameraChanges(t *testing.T) {
	w, _ := panelFieldWorkspace(t)
	field := w.panelField
	textures := panelFieldTextures(field)
	if len(textures) != 8 {
		t.Fatalf("expected eight shared authored sheets, got %d textures", len(textures))
	}
	before := map[uint64]render.TextureUpdate{}
	for id, texture := range textures {
		before[id], _ = texture.Snapshot(0)
	}
	transforms := map[scene.NodeID]scene.Mat4{}
	for _, id := range field.scene.Children(0) {
		transforms[id] = field.scene.Node(id).Transform
	}
	for _, size := range [][2]int{{2048, 896}, {900, 1440}, {2880, 1800}, {1440, 900}} {
		w.m.cameraX += .35
		w.m.cameraY -= .12
		w.m.cameraDepth += .2
		w.Draw(size[0], size[1])
		if w.panelField != field {
			t.Fatal("viewport/camera change rebuilt retained field")
		}
		for id, texture := range panelFieldTextures(w.panelField) {
			original, ok := before[id]
			current, _ := texture.Snapshot(0)
			if !ok || texture != textures[id] || current.Revision != original.Revision || !bytes.Equal(current.Pixels, original.Pixels) {
				t.Fatalf("camera travel or resize changed uploaded sheet %d", id)
			}
		}
		for id, transform := range transforms {
			if field.scene.Node(id).Transform != transform {
				t.Fatal("camera travel moved scenery instead of viewing fixed world geometry")
			}
		}
	}
}

func TestPanelFieldPaletteChangeRebakesOnlyItsOwnedScenery(t *testing.T) {
	w, _ := panelFieldWorkspace(t)
	original := w.panelField
	before := map[uint64]render.TextureUpdate{}
	for id, texture := range panelFieldTextures(original) {
		before[id], _ = texture.Snapshot(0)
	}
	selected := w.CurrentSkin()
	selected.Palette["desktop-glow"] = "#ED9011"
	if err := w.SetSkin(*selected); err != nil {
		t.Fatal(err)
	}
	w.Draw(1440, 900)
	if w.panelField != original {
		t.Fatal("unrelated desktop glow unnecessarily rebuilt field textures")
	}
	selected.Palette["field-line"] = "#FF9040C0"
	if err := w.SetSkin(*selected); err != nil {
		t.Fatal(err)
	}
	w.Draw(1440, 900)
	if w.panelField == original || w.panelField.palette == original.palette {
		t.Fatal("field palette edit failed to invalidate retained bake")
	}
	for id := range panelFieldTextures(w.panelField) {
		if _, reused := before[id]; reused {
			t.Fatal("new palette reused an old authored texture")
		}
	}
	for id, texture := range panelFieldTextures(original) {
		current, _ := texture.Snapshot(0)
		if current.Revision != before[id].Revision || !bytes.Equal(current.Pixels, before[id].Pixels) {
			t.Fatal("rebaking mutated an already submitted retained texture")
		}
	}
	oldSheet := original.scene.Node(original.scene.Children(0)[0]).Surface
	newSheet := w.panelField.scene.Node(w.panelField.scene.Children(0)[0]).Surface
	oldPixels, _ := oldSheet.Snapshot(0)
	newPixels, _ := newSheet.Snapshot(0)
	if bytes.Equal(oldPixels.Pixels, newPixels.Pixels) {
		t.Fatal("field-line palette edit did not change sheet pixels")
	}
}

func TestPanelFieldUsesWorkspaceCameraWithDepthParallaxAndNoInputTargets(t *testing.T) {
	w, apps := panelFieldWorkspace(t)
	textures := panelFieldTextures(w.panelField)
	findViews := func(frame render.Frame) (field, application render.View) {
		t.Helper()
		fieldFound, applicationFound := false, false
		for _, command := range frame.Commands {
			if command.Kind != render.SceneCommand {
				continue
			}
			for _, draw := range command.Draws {
				if draw.Texture == nil {
					continue
				}
				if _, ok := textures[draw.Texture.ID()]; ok {
					field, fieldFound = command.View, true
					if !draw.Translucent {
						t.Fatal("field sheet discarded authored alpha")
					}
				}
				if draw.Texture == apps.surfaces[0].Texture {
					application, applicationFound = command.View, true
				}
			}
		}
		if !fieldFound || !applicationFound {
			t.Fatal("missing field or real application camera pass")
		}
		if field.Projection != application.Projection || field.Eye != application.Eye || field.Viewport != application.Viewport {
			t.Fatal("backdrop drifted onto a different camera than working apps")
		}
		return
	}
	initialView, _ := findViews(w.Draw(1440, 900))
	var near, far scene.Vec3
	nearDistance, farDistance := float32(math.MaxFloat32), float32(0)
	visible := 0
	for _, id := range w.panelField.scene.Children(0) {
		node := w.panelField.scene.Node(id)
		if !node.Unpickable {
			t.Fatalf("decorative node %d can intercept input", id)
		}
		point := node.Transform.TransformPoint(scene.Vec3{})
		x, y, _, onScreen := w.camera.Project(point, w.viewport)
		if onScreen {
			visible++
			if hit, ok := w.panelField.scene.Pick(w.camera, w.viewport, x, y); ok {
				t.Fatalf("decorative field produced pick %+v", hit)
			}
		}
		if node.Surface == nil {
			continue
		}
		distance := point.Sub(w.camera.Eye).Length()
		if distance < nearDistance {
			near, nearDistance = point, distance
		}
		if distance > farDistance {
			far, farDistance = point, distance
		}
	}
	if visible == 0 || farDistance-nearDistance < 10 {
		t.Fatal("field does not contain visible world geometry at different depths")
	}
	nearX, _, _, _ := w.camera.Project(near, w.viewport)
	farX, _, _, _ := w.camera.Project(far, w.viewport)
	w.m.cameraX += 1
	movedView, _ := findViews(w.Draw(1440, 900))
	if movedView.Projection == initialView.Projection || movedView.Eye == initialView.Eye {
		t.Fatal("camera travel did not update field projection and eye")
	}
	newNearX, _, _, _ := w.camera.Project(near, w.viewport)
	newFarX, _, _, _ := w.camera.Project(far, w.viewport)
	nearMotion, farMotion := math.Abs(float64(newNearX-nearX)), math.Abs(float64(newFarX-farX))
	if farMotion < .1 || nearMotion < farMotion*1.4 {
		t.Fatalf("depth did not produce differential parallax: near=%f, far=%f", nearMotion, farMotion)
	}
}

func TestPanelFieldSkinExitRestoresOpaqueApplicationsAndAmbientPreferences(t *testing.T) {
	w, apps := panelFieldWorkspace(t)
	environment, phase := w.environment, w.backgroundPhase
	field := w.panelField
	if !w.scene.Node(w.applicationNodes[apps.surfaces[0].ID]).Translucent {
		t.Fatal("panel-field did not enable authored application alpha")
	}
	w.updateBackground(time.Second)
	if w.backgroundPhase != phase || w.environment != environment {
		t.Fatal("field backdrop advanced hidden ambient simulation or changed preferences")
	}
	selected, err := skin.Builtin("plasma")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetSkin(selected); err != nil {
		t.Fatal(err)
	}
	frame := w.Draw(1440, 900)
	if w.panelFieldVisible() || w.scene.Node(w.applicationNodes[apps.surfaces[0].ID]).Translucent {
		t.Fatal("switching away retained floating-panel application coverage")
	}
	oldTextures := panelFieldTextures(field)
	for _, command := range frame.Commands {
		for _, draw := range command.Draws {
			if draw.Texture != nil && oldTextures[draw.Texture.ID()] != nil {
				t.Fatal("previous field scenery remained visible under another skin")
			}
		}
	}
	w.updateBackground(time.Millisecond)
	if w.backgroundPhase == phase || w.environment != environment {
		t.Fatal("switching away failed to resume unchanged environment preferences")
	}
}

func TestPanelFieldSkinSwitchPreservesApplicationAuthoredTranslucency(t *testing.T) {
	w, apps := windowDragWorkspace(t, 2)
	apps.surfaces[0].Translucent = true
	for _, skinID := range []string{"plasma", "future-panels", "plasma"} {
		selected, err := skin.Builtin(skinID)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.SetSkin(selected); err != nil {
			t.Fatal(err)
		}
		w.Draw(1440, 900)
		if !w.scene.Node(w.applicationNodes[apps.surfaces[0].ID]).Translucent {
			t.Fatalf("skin %q discarded the application's authored translucency", skinID)
		}
		if got := w.scene.Node(w.applicationNodes[apps.surfaces[1].ID]).Translucent; got != (skinID == "future-panels") {
			t.Fatalf("skin %q gave the opaque application unexpected coverage: translucent=%v", skinID, got)
		}
		if !apps.surfaces[0].Translucent || apps.surfaces[1].Translucent {
			t.Fatal("skin switching mutated provider-owned surface preferences")
		}
	}
}

func TestPanelFieldRepeatedPaletteEditsRetireSupersededResourcesExactlyOnce(t *testing.T) {
	w, _ := panelFieldWorkspace(t)
	w.RetiredTextures()
	w.RetiredGeometryIDs()
	oldTextures, oldGeometry := map[uint64]bool{}, map[uint64]bool{}
	var current render.Frame
	for _, value := range []skin.Color{"#D68050B0", "#70A4D2C0"} {
		for _, id := range w.panelField.scene.Children(0) {
			node := w.panelField.scene.Node(id)
			if node.Surface != nil {
				oldTextures[node.Surface.ID()] = true
			}
			if node.Mesh != nil {
				oldGeometry[node.Mesh.Geometry().ID()] = true
			}
		}
		selected := w.CurrentSkin()
		selected.Palette["field-line"] = value
		if err := w.SetSkin(*selected); err != nil {
			t.Fatal(err)
		}
		current = w.Draw(1440, 900)
		// Do not drain here: a second edit must preserve the first edit's IDs.
	}
	if len(oldTextures) != 16 || len(oldGeometry) != 2 {
		t.Fatalf("fixture did not replace two complete field generations: textures=%d geometry=%d", len(oldTextures), len(oldGeometry))
	}
	liveTextures, liveGeometry := map[uint64]bool{}, map[uint64]bool{}
	for _, command := range current.Commands {
		if command.Kind == render.ImageCommand && command.Image.Texture != nil {
			liveTextures[command.Image.Texture.ID()] = true
		}
		for _, draw := range command.Draws {
			if draw.Texture != nil {
				liveTextures[draw.Texture.ID()] = true
			}
			if draw.Geometry != nil {
				liveGeometry[draw.Geometry.ID()] = true
			}
		}
	}
	check := func(label string, retired []uint64, expected, live map[uint64]bool) {
		t.Helper()
		seen := map[uint64]bool{}
		for _, id := range retired {
			if id == 0 || seen[id] || live[id] {
				t.Fatalf("invalid %s retirement %d: duplicate=%v live=%v", label, id, seen[id], live[id])
			}
			seen[id] = true
		}
		for id := range expected {
			if !seen[id] {
				t.Fatalf("superseded %s %d was lost before retirement drain", label, id)
			}
		}
	}
	check("texture", w.RetiredTextures(), oldTextures, liveTextures)
	check("geometry", w.RetiredGeometryIDs(), oldGeometry, liveGeometry)
	if len(w.RetiredTextures()) != 0 || len(w.RetiredGeometryIDs()) != 0 {
		t.Fatal("retirement drain returned an already retired resource again")
	}
}

func TestPanelFieldMissingOptionalPaletteUsesColoredTranslucentDefaults(t *testing.T) {
	w, _ := windowDragWorkspace(t, 1)
	selected, err := skin.Builtin("future-panels")
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"field-panel", "field-slate", "field-line", "field-light"} {
		delete(selected.Palette, token)
	}
	if err := w.SetSkin(selected); err != nil {
		t.Fatal(err)
	}
	w.Draw(1440, 900)
	if w.panelField == nil || len(panelFieldTextures(w.panelField)) != 8 {
		t.Fatal("missing optional colors prevented field creation")
	}
	for _, texture := range panelFieldTextures(w.panelField) {
		pixels, _ := texture.Snapshot(0)
		i := (8*pixels.Width + 8) * 4 // Plain interior away from linework/text.
		pixel := pixels.Pixels[i : i+4]
		if pixel[3] == 0 || pixel[3] == 255 || int(pixel[0])+int(pixel[1])+int(pixel[2]) == 0 {
			t.Fatalf("optional color fallback produced invisible, black, or opaque panel: %v", pixel)
		}
	}
	for _, id := range w.panelField.scene.Children(0) {
		node := w.panelField.scene.Node(id)
		if node.Mesh != nil && (node.Color.R <= 0 || node.Color.G <= 0 || node.Color.B <= 0 || node.Color.A != 1) {
			t.Fatalf("optional lamp color fallback disappeared: %+v", node.Color)
		}
	}
}
