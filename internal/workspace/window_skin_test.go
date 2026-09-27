package workspace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func testWindowSkin(t *testing.T, id string) skin.Skin {
	t.Helper()
	s, err := skin.Builtin(id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func assertWindowSkinOutsideClient(t *testing.T, mesh *scene.Mesh) {
	t.Helper()
	vertices, indices := mesh.Geometry().Vertices(), mesh.Geometry().Indices()
	for i := 0; i < len(indices); i += 3 {
		var triangle []scene.Vec3
		for _, index := range indices[i : i+3] {
			v := vertices[index]
			if !finite(float64(v.X)) || !finite(float64(v.Y)) || !finite(float64(v.Z)) || v.Z > 0 {
				t.Fatalf("invalid chrome vertex: %+v", v)
			}
			triangle = append(triangle, scene.Vec3{X: v.X, Y: v.Y, Z: v.Z})
		}
		if area := terminalTriangleContentArea(triangle); area > 1e-9 {
			t.Fatalf("chrome triangle covers client pixels: area=%g triangle=%v", area, triangle)
		}
	}
}

func TestWindowSkinsHaveDistinctRecipeGeometryAndProtectClientAperture(t *testing.T) {
	seen := map[string]*windowSkinMeshes{}
	for _, id := range []string{"merrick", "advanced", "hologram", "plasma", "future-panels"} {
		t.Run(id, func(t *testing.T) {
			s := testWindowSkin(t, id)
			for _, aspect := range []float32{.5, 1, 1.6, 3.2, 5} {
				meshes := buildSkinWindowMeshes(&s, aspect, "normal")
				for _, part := range []windowSkinPart{meshes.frame, meshes.grip, meshes.controls, meshes.resize} {
					assertWindowSkinOutsideClient(t, part.mesh)
				}
				assertSafeWindowBorderMesh(t, meshes.frame.mesh)
				if aspect == 1.6 {
					for previous, other := range seen {
						if sameWindowChromeShape(meshes.frame.mesh, other.frame.mesh) || sameWindowChromeShape(meshes.controls.mesh, other.controls.mesh) || sameWindowChromeShape(meshes.grip.mesh, other.grip.mesh) {
							t.Fatalf("%s reuses %s's frame, controls, or grip silhouette", id, previous)
						}
					}
					seen[id] = meshes
				}
			}
		})
	}
}

func windowSkinPoint(t *testing.T, w *Workspace, surface experience.ApplicationSurface, bounds box, fx, fy float32) (float32, float32) {
	t.Helper()
	root := w.scene.Node(w.applicationNodes[surface.ID])
	point := root.Transform.TransformPoint(scene.Vec3{X: bounds.x + bounds.w*fx, Y: bounds.y + bounds.h*fy})
	x, y, _, visible := w.camera.Project(point, w.viewport)
	if !visible {
		t.Fatal("skin control left the visible scene")
	}
	return x, y
}

func TestWindowSkinControlsPickTheirSharedBoxesAndKeepInputSemantics(t *testing.T) {
	for _, id := range []string{"merrick", "advanced", "hologram", "plasma", "future-panels"} {
		t.Run(id, func(t *testing.T) {
			w, apps := windowDragWorkspace(t, 1)
			closer := &closingApplications{fakeApplications: apps}
			w.SetApplications(closer)
			if err := w.SetSkin(testWindowSkin(t, id)); err != nil {
				t.Fatal(err)
			}
			w.Draw(1440, 900)
			surface := apps.surfaces[0]
			controls := w.skinWindowMeshes(surface).layout.controls
			for _, target := range controls {
				for _, fraction := range [][2]float32{{.5, .5}, {.08, .08}, {.92, .92}} {
					x, y := windowSkinPoint(t, w, surface, target.box, fraction[0], fraction[1])
					got, kind, _, ok := w.applicationWindowControlAt(x, y)
					if !ok || got.ID != surface.ID || kind != target.kind {
						t.Fatalf("%s control %s has a pick hole at %v: got %v kind=%v hit=%v", id, target.name, fraction, got.ID, kind, ok)
					}
				}
			}
			before := w.Document()
			for _, target := range controls {
				x, y := windowSkinPoint(t, w, surface, target.box, .5, .5)
				pointer(w, experience.PointerDown, x, y)
				if w.pointer.kind != captureApplicationWindowControl {
					t.Fatalf("%s did not capture %s", id, target.name)
				}
				pointer(w, experience.PointerUp, x, y)
				switch target.kind {
				case windowControlMinimize:
					if !w.m.applicationState.Layouts[w.m.applicationState.index(surface.Key)].Minimized {
						t.Fatal("skin minimize did not minimize")
					}
				case windowControlRead:
					if !w.m.applicationState.Reading {
						t.Fatal("skin maximize did not enter Read")
					}
				case windowControlClose:
					if len(closer.closed) != 1 || closer.closed[0] != surface.ID {
						t.Fatal("skin close did not reach the provider")
					}
				}
				w.install(before, false)
				w.Draw(1440, 900)
			}
			if len(apps.events) != 0 {
				t.Fatal("window chrome leaked pointer input into client content")
			}
			grip := w.skinWindowMeshes(surface).layout.grip
			x, y := windowSkinPoint(t, w, surface, grip, .5, .5)
			pointer(w, experience.PointerDown, x, y)
			if !w.pointer.windowDrag {
				t.Fatal("skin grip did not start window placement")
			}
			pointer(w, experience.PointerMove, x+24, y+12)
			pointer(w, experience.PointerUp, x+24, y+12)
			if w.Document().View.Application.Layouts == before.View.Application.Layouts {
				t.Fatal("skin grip did not move the window")
			}
			w.Draw(1440, 900)
			resize := w.skinWindowMeshes(surface).layout.resize
			x, y = windowSkinPoint(t, w, surface, resize, .5, .5)
			if got, ok := w.applicationResizeTarget(x, y); !ok || got.ID != surface.ID {
				t.Fatal("skin resize grip is not pickable")
			}
			pointer(w, experience.PointerDown, x, y)
			if w.pointer.kind != captureApplicationResize {
				t.Fatal("skin resize grip did not reserve resizing")
			}
			pointer(w, experience.PointerMove, x+30, y+20)
			pointer(w, experience.PointerUp, x+30, y+20)
			if len(apps.resizes) == 0 {
				t.Fatal("skin resize did not notify the provider")
			}
		})
	}
}

func TestLoadedWindowSkinRecipeControlsGeometryAndRetiresReplacedResources(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	custom := testWindowSkin(t, "merrick")
	custom.ID = "studio.user-authored"
	custom.Name = "Studio custom"
	custom.Window.Layout.ButtonsSide = "left"
	custom.Window.Layout.ButtonOrder = []string{"close", "maximize", "minimize"}
	custom.Window.Layout.TitlebarHeight = 68
	custom.Window.Frame.Layers[0].Geometry = skin.Geometry{Kind: "notched", Corner: .025, Notch: .012}
	custom.Palette["surface"] = skin.Color("#854d73")
	encoded, err := json.Marshal(custom)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := skin.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetSkin(loaded); err != nil {
		t.Fatal(err)
	}
	w.Draw(1440, 900)
	surface := apps.surfaces[0]
	first := w.skinWindowMeshes(surface)
	if first.layout.controls[0].kind != windowControlClose || first.layout.controls[0].box.x > 0 {
		t.Fatal("loaded package's control order/side did not drive host layout")
	}
	if first != w.skinWindowMeshes(surface) {
		t.Fatal("unchanged recipe rebuilt its retained geometry")
	}
	if err := w.SetSkin(loaded.Clone()); err != nil {
		t.Fatal(err)
	}
	w.Draw(1440, 900)
	if first != w.skinWindowMeshes(surface) || len(w.RetiredGeometryIDs()) != 0 {
		t.Fatal("identical recipe content invalidated the mesh cache")
	}
	loaded.Palette["surface"] = skin.Color("#438355")
	loaded.Window.Frame.Layers[0].Geometry = skin.Geometry{Kind: "rounded", Radius: .025}
	if err := w.SetSkin(loaded); err != nil {
		t.Fatal(err)
	}
	w.Draw(1440, 900)
	second := w.skinWindowMeshes(surface)
	if first == second || sameWindowChromeShape(first.frame.mesh, second.frame.mesh) || reflect.DeepEqual(first.frame.mesh.Geometry().Vertices(), second.frame.mesh.Geometry().Vertices()) {
		t.Fatal("editing a loaded recipe with the same ID did not change geometry and palette")
	}
	retired := w.RetiredGeometryIDs()
	if len(retired) != 4 || len(w.RetiredGeometryIDs()) != 0 {
		t.Fatalf("replaced meshes were not retired once: %v", retired)
	}
}

func TestWindowSkinGlassUsesSupportedSceneMaterialAndRestoresLegacy(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	if err := w.SetSkin(testWindowSkin(t, "hologram")); err != nil {
		t.Fatal(err)
	}
	frame := w.Draw(1440, 900)
	border := w.scene.Node(w.applicationFrames[apps.surfaces[0].ID])
	if !border.Translucent || border.Unlit || border.Material.Transmission == 0 || border.Material.Refraction == 0 || border.Glow == [3]float32{} {
		t.Fatalf("glass intent was not installed as supported lit translucent geometry: %+v", border)
	}
	for _, command := range frame.Commands {
		for _, draw := range command.Draws {
			if draw.Texture != nil && (draw.Material != (render.Material{}) || draw.Glow != [3]float32{}) {
				t.Fatal("skin glass or glow reached client pixels")
			}
		}
	}
	w.activeSkin = nil
	w.Draw(1440, 900)
	if border.Translucent || !border.Unlit || border.Material != (render.Material{}) || border.Mesh != w.windowBorderFrameMesh(windowBorderInstrument) {
		t.Fatal("legacy border inherited optics from a previously selected skin")
	}
	if len(w.RetiredGeometryIDs()) == 0 {
		t.Fatal("returning to legacy chrome did not retire skin geometry")
	}
}

func TestWindowSkinReadIncludesItsAuthoredTitlebarEnvelope(t *testing.T) {
	for _, id := range []string{"merrick", "advanced", "hologram", "plasma", "future-panels"} {
		t.Run(id, func(t *testing.T) {
			w, apps := windowDragWorkspace(t, 1)
			if err := w.SetSkin(testWindowSkin(t, id)); err != nil {
				t.Fatal(err)
			}
			command(t, w, Action{Kind: ToggleApplicationReading})
			w.Draw(1200, 800)
			surface := apps.surfaces[0]
			root := w.scene.Node(w.applicationNodes[surface.ID])
			border := w.scene.Node(w.applicationFrames[surface.ID])
			_, aspect, _, logicalWidth := w.windowSkinDimensions(surface)
			layout := skinWindowLayoutForWidth(w.activeSkin, aspect, logicalWidth)
			readLayout := w.skinWindowMeshes(surface).layout
			if abs(readLayout.outer.h-(layout.frame.h-layout.topbar.h)) > 1e-6 {
				t.Fatal("Read retained an empty titlebar band above its client")
			}
			if abs(readLayout.outer.x-layout.frame.x) > 1e-6 || abs(readLayout.outer.w-layout.frame.w) > 1e-6 {
				t.Fatal("Read reserved space for a hidden vertical title tab")
			}
			for _, vertex := range border.Mesh.Geometry().Vertices() {
				if vertex.Y > readLayout.outer.y+readLayout.outer.h+.004 {
					t.Fatal("Read frame geometry retained the hidden titlebar envelope")
				}
				point := root.Transform.TransformPoint(scene.Vec3{X: vertex.X, Y: vertex.Y, Z: vertex.Z})
				x, y, _, visible := w.camera.Project(point, w.viewport)
				if !visible || x < w.viewport.X || y < w.viewport.Y || x > w.viewport.X+w.viewport.Width || y > w.viewport.Y+w.viewport.Height {
					t.Fatalf("Read clipped authored %s border at %g,%g", id, x, y)
				}
			}
		})
	}
}

func TestWindowSkinTitlesAreRetainedOccludedSceneGeometry(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	apps.surfaces[0].Title = "Research / pressure trace"
	if err := w.SetSkin(testWindowSkin(t, "merrick")); err != nil {
		t.Fatal(err)
	}
	w.Draw(1440, 900)
	surface := apps.surfaces[0]
	label := w.windowSkinCache.titles[surface.ID]
	if label == nil || label.mesh == nil || label.parent != w.applicationDragHandles[surface.ID] {
		t.Fatal("skin title was not attached to its actual scene grip")
	}
	node := w.scene.Node(label.node)
	if !node.Unpickable || node.Surface != nil || node.DepthReadOnly || node.Translucent {
		t.Fatal("skin title does not preserve opaque scene depth and input")
	}
	assertWindowSkinOutsideClient(t, label.mesh)
	w.Draw(1440, 900)
	if w.windowSkinCache.titles[surface.ID] != label {
		t.Fatal("unchanged window title rebuilt its glyph geometry")
	}
	apps.surfaces[0].Title = "Research / updated trace"
	w.Update(0)
	w.Draw(1440, 900)
	if updated := w.windowSkinCache.titles[surface.ID]; updated == nil || updated.mesh == label.mesh || updated.text != apps.surfaces[0].Title {
		t.Fatal("provider title update did not replace its retained text")
	}
	retired := w.RetiredGeometryIDs()
	if len(retired) != 1 || retired[0] != label.mesh.Geometry().ID() {
		t.Fatalf("replaced title geometry was not retired exactly once: %v", retired)
	}
	apps.surfaces = nil
	w.Update(0)
	w.Draw(1440, 900)
	retired = w.RetiredGeometryIDs()
	if len(w.windowSkinCache.titles) != 0 || len(retired) != 1 {
		t.Fatal("closed application retained title resources")
	}
}

func TestWindowSkinContinuousResizeHasBoundedRetainedGeometry(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	apps.surfaces[0].MinWidth, apps.surfaces[0].MinHeight = 96, 64
	if err := w.SetSkin(testWindowSkin(t, "merrick")); err != nil {
		t.Fatal(err)
	}
	surface := apps.surfaces[0]
	index := w.m.applicationState.index(surface.Key)
	for bucket := 20; bucket < 100; bucket++ {
		w.m.applicationState.Layouts[index].Width = bucket * 20
		w.m.applicationState.Layouts[index].Height = 800
		w.skinWindowMeshes(surface)
	}
	if len(w.windowSkinCache.meshes) != 64 {
		t.Fatalf("resize retained %d chrome mesh sets, want the bounded 64", len(w.windowSkinCache.meshes))
	}
	retired := w.RetiredGeometryIDs()
	if len(retired) != (80-64)*4 {
		t.Fatalf("resize did not retire evicted mesh sets: got %d", len(retired))
	}
	for _, id := range retired {
		for _, meshes := range w.windowSkinCache.meshes {
			for _, part := range []windowSkinPart{meshes.frame, meshes.grip, meshes.controls, meshes.resize} {
				if part.mesh.Geometry().ID() == id {
					t.Fatal("a retained mesh was prematurely retired")
				}
			}
		}
	}
}

func TestFuturePanelsSkinSharesAndRetainsFiveWindowChrome(t *testing.T) {
	w, apps := windowDragWorkspace(t, 5)
	for i := range apps.surfaces {
		apps.surfaces[i].Title = fmt.Sprintf("Panel %d", i+1)
	}
	if err := w.SetSkin(testWindowSkin(t, "future-panels")); err != nil {
		t.Fatal(err)
	}
	w.Draw(1440, 900)
	warm := make(map[uint64]*windowSkinMeshes)
	unique := make(map[*windowSkinMeshes]bool)
	for _, surface := range apps.surfaces {
		meshes := w.skinWindowMeshes(surface)
		warm[surface.ID], unique[meshes] = meshes, true
		if !meshes.frame.translucent || !meshes.grip.translucent {
			t.Fatal("authored glass frame or title lost scene transparency")
		}
		if meshes.controls.translucent || meshes.resize.translucent {
			t.Fatal("compact control glyphs acquired unnecessary glass passes")
		}
	}
	if len(unique) >= len(apps.surfaces) {
		t.Fatal("same-size/state windows do not share retained chrome")
	}
	vertices, indices := 0, 0
	for meshes := range unique {
		for _, part := range []windowSkinPart{meshes.frame, meshes.grip, meshes.controls, meshes.resize} {
			vertices += len(part.mesh.Geometry().Vertices())
			indices += len(part.mesh.Geometry().Indices())
		}
	}
	// Moving/depth-throwing a window changes only its transform. Chrome and
	// title resources must not be rebuilt while it crosses the panel field.
	for _, surface := range apps.surfaces {
		i := w.m.applicationState.index(surface.Key)
		w.m.applicationState.Layouts[i].X += .17
		w.m.applicationState.Layouts[i].Depth -= .3
	}
	w.Draw(1440, 900)
	for _, surface := range apps.surfaces {
		if w.skinWindowMeshes(surface) != warm[surface.ID] {
			t.Fatal("moving a panel regenerated its retained chrome")
		}
	}
	t.Logf("five windows share %d chrome sets / %d geometry buffers: %d vertices, %d indices", len(unique), len(unique)*4, vertices, indices)
}

func windowSkinGlowCases(t *testing.T) []skin.Skin {
	t.Helper()
	var cases []skin.Skin
	for _, id := range []string{"merrick", "advanced", "hologram", "plasma", "future-panels"} {
		cases = append(cases, testWindowSkin(t, id))
	}
	maximum := testWindowSkin(t, "hologram")
	maximum.ID = "custom.maximum-glow"
	for name, material := range maximum.Materials {
		material.Glow = 4
		maximum.Materials[name] = material
	}
	maximum.Window.Frame.States["selected"] = skin.StateStyle{Glow: 4}
	cases = append(cases, maximum)
	return cases
}

func TestWindowSkinGlowRespectsRendererChannelContract(t *testing.T) {
	assertGlow := func(t *testing.T, channels [3]float32) {
		t.Helper()
		for _, value := range channels {
			if !finite(float64(value)) || value < 0 || value > 1 {
				t.Fatalf("glow channel %g violates renderer [0,1] contract", value)
			}
		}
	}
	for _, selected := range windowSkinGlowCases(t) {
		t.Run(selected.ID, func(t *testing.T) {
			for _, state := range []string{"normal", "selected", "focused", "hovered", "pressed"} {
				meshes := buildSkinWindowMeshes(&selected, 1.6, state)
				for _, part := range []windowSkinPart{meshes.frame, meshes.grip, meshes.controls, meshes.resize} {
					assertGlow(t, part.glow)
				}
			}
			w, _ := windowDragWorkspace(t, 1)
			if err := w.SetSkin(selected); err != nil {
				t.Fatal(err)
			}
			frame := w.Draw(1440, 900)
			saturated := false
			for _, command := range frame.Commands {
				for _, draw := range command.Draws {
					assertGlow(t, draw.Glow)
					for _, value := range draw.Glow {
						saturated = saturated || value == 1
					}
				}
			}
			if selected.ID == "custom.maximum-glow" && !saturated {
				t.Fatal("maximum authored glow was discarded instead of bounded")
			}
		})
	}
}

func TestWindowSkinPresetsAndMaximumGlowRenderGPU(t *testing.T) {
	if os.Getenv("WORLDR_TEST_GPU") != "1" {
		t.Skip("set WORLDR_TEST_GPU=1 to validate skin frames through Vulkan")
	}
	const width, height = 1440, 900
	vk, err := native.OpenVK(false, width, height)
	if err != nil {
		t.Fatal(err)
	}
	defer vk.Close()
	pixels := make([]byte, width*height*4)
	for _, selected := range windowSkinGlowCases(t) {
		t.Run(selected.ID, func(t *testing.T) {
			w, _ := windowDragWorkspace(t, 1)
			if err := w.SetSkin(selected); err != nil {
				t.Fatal(err)
			}
			if err := vk.SetSceneAtlas(w.Atlas()); err != nil {
				t.Fatal(err)
			}
			if err := vk.RenderFrame(w.Draw(width, height), [4]float32{0, 0, 0, 1}, pixels); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// The same custom package must retain physical title/control density on a
// narrow calendar and a document, without depending on the preset's ID.
func TestWindowSkinClientPixelTabsPreserveDensityAndPickTargets(t *testing.T) {
	for _, width := range []int{96, 190, 720} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			w, apps := windowDragWorkspace(t, 1)
			selected := testWindowSkin(t, "merrick")
			selected.ID = "custom.slate-tools"
			apps.surfaces[0].MinWidth, apps.surfaces[0].MinHeight = 96, 64
			var data bytes.Buffer
			if err := skin.Encode(&data, selected); err != nil {
				t.Fatal(err)
			}
			loaded, err := skin.Decode(&data)
			if err != nil {
				t.Fatal(err)
			}
			if err := w.SetSkin(loaded); err != nil {
				t.Fatal(err)
			}
			surface := apps.surfaces[0]
			apps.surfaces[0].Title = "Calendar"
			index := w.m.applicationState.index(surface.Key)
			w.m.applicationState.Layouts[index].Width = width
			w.m.applicationState.Layouts[index].Height = 710
			w.Update(0)
			w.Draw(1440, 900)
			surface = apps.surfaces[0]
			meshes := w.skinWindowMeshes(surface)
			l := meshes.layout
			if !l.verticalTitle || abs(l.titlebar.w*l.logicalWidth-32) > .001 || l.titlebar.x+l.titlebar.w > -.5 {
				t.Fatalf("title tab lost authored width or covered client: %+v", l)
			}
			for _, control := range l.controls {
				if abs(control.box.w*l.logicalWidth-24) > .001 || control.box.y < .5 {
					t.Fatal("compact control lost its pixel extent")
				}
				for _, corner := range [][2]float32{{.08, .08}, {.5, .5}, {.92, .92}} {
					x, y := windowSkinPoint(t, w, surface, control.box, corner[0], corner[1])
					got, kind, _, ok := w.applicationWindowControlAt(x, y)
					if !ok || got.ID != surface.ID || kind != control.kind {
						t.Fatalf("narrow control %s has a picking hole", control.name)
					}
				}
			}
			for _, target := range []box{l.grip, l.titlebar} {
				x, y := windowSkinPoint(t, w, surface, target, .5, .5)
				if got, ok := w.applicationDragTarget(x, y); !ok || got.ID != surface.ID {
					t.Fatal("vertical tab did not drag the window")
				}
			}
			label := w.windowSkinCache.titles[surface.ID]
			if label == nil {
				t.Fatal("narrow client lost its title")
			}
			assertWindowSkinOutsideClient(t, label.mesh)
			var minX, maxX float32 = 100, -100
			for _, vertex := range label.mesh.Geometry().Vertices() {
				minX = min(minX, vertex.X)
				maxX = max(maxX, vertex.X)
			}
			if pixels := (maxX - minX) * l.logicalWidth; pixels < 7 || pixels > 15 {
				t.Fatalf("vertical title is not readable at the authored font size: %g pixels", pixels)
			}
			for _, part := range []windowSkinPart{meshes.frame, meshes.grip, meshes.controls, meshes.resize} {
				assertWindowSkinOutsideClient(t, part.mesh)
			}
		})
	}
}

func TestWindowSkinDensityParticipatesInCacheAndLegacyRemainsCanonical(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	apps.surfaces[0].MinWidth, apps.surfaces[0].MinHeight = 96, 64
	for _, id := range []string{"merrick", "plasma"} {
		if err := w.SetSkin(testWindowSkin(t, id)); err != nil {
			t.Fatal(err)
		}
		surface := apps.surfaces[0]
		index := w.m.applicationState.index(surface.Key)
		w.m.applicationState.Layouts[index].Width, w.m.applicationState.Layouts[index].Height = 200, 400
		first := w.skinWindowMeshes(surface)
		w.m.applicationState.Layouts[index].Width, w.m.applicationState.Layouts[index].Height = 400, 800
		second := w.skinWindowMeshes(surface)
		if id == "merrick" && (first == second || abs(first.layout.titlebar.w-second.layout.titlebar.w*2) > .001) {
			t.Fatal("client density failed to invalidate geometry at the same aspect")
		}
		if id == "plasma" && first != second {
			t.Fatal("legacy canonical density changed with client pixel size")
		}
	}
}

func TestWindowSkinClientPixelDensityMatchesProviderMinimums(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	if err := w.SetSkin(testWindowSkin(t, "merrick")); err != nil {
		t.Fatal(err)
	}
	surface := apps.surfaces[0]
	index := w.m.applicationState.index(surface.Key)
	w.m.applicationState.Layouts[index].Width, w.m.applicationState.Layouts[index].Height = 190, 710
	_, aspect, _, width := w.windowSkinDimensions(surface)
	if width != 720 || abs(aspect-float32(720.0/710)) > .004 {
		t.Fatalf("chrome density and provider-constrained client plane disagree: %g, %g", width, aspect)
	}
}

func TestWindowSkinCompactButtonRailFitsAuthoredLargeControls(t *testing.T) {
	s := testWindowSkin(t, "merrick")
	s.Window.Layout.ButtonWidth, s.Window.Layout.ButtonGap = 256, 64
	layout := skinWindowLayoutForWidth(&s, .25, 96)
	for _, control := range layout.controls {
		if control.box.x < -.5 || control.box.x+control.box.w > .5 || control.box.w*96 < 24 {
			t.Fatalf("small rail lost its accessible control: %+v", control)
		}
	}
}

func TestWindowSkinShortUtilityTabsHaveRoomForTheirFullNames(t *testing.T) {
	selected := testWindowSkin(t, "merrick")
	for _, size := range [][2]float32{{480, 86}, {230, 100}, {96, 64}} {
		aspect := size[0] / size[1]
		layout := skinWindowLayoutForWidth(&selected, aspect, size[0])
		bounds := windowSkinTitleBounds(&selected, layout, aspect)
		if pixels := bounds.h / aspect * layout.logicalWidth; pixels < 80 {
			t.Fatalf("%gx%g utility has only %g logical pixels for Programs/Messages", size[0], size[1], pixels)
		}
		if layout.titlebar.y < layout.frame.y-1e-6 || layout.titlebar.y+layout.titlebar.h > layout.frame.y+layout.frame.h+1e-6 {
			t.Fatal("short utility tab extends beyond its frame")
		}
	}
}

func TestLoadedStudioWindowRecipesRetainPixelGripsAndColoredDoubleRails(t *testing.T) {
	for _, preset := range []string{"advanced", "hologram"} {
		selected := testWindowSkin(t, preset)
		selected.ID = "custom." + preset
		var data bytes.Buffer
		if err := skin.Encode(&data, selected); err != nil {
			t.Fatal(err)
		}
		loaded, err := skin.Decode(&data)
		if err != nil {
			t.Fatal(err)
		}
		const width, aspect = float32(1440), float32(1.44)
		meshes := buildSkinWindowMeshesForViewAndWidth(&loaded, aspect, width, "normal", false)
		if pixels := meshes.layout.grip.w * width; abs(pixels-float32(loaded.Window.Layout.GripWidth)) > .001 {
			t.Fatalf("%s lost its authored compact grip width: %g", preset, pixels)
		}
		label := windowSkinTitleBounds(&loaded, meshes.layout, aspect)
		if label.x > -.4 || label.w < .6 {
			t.Fatalf("%s reserves a bulky grip instead of a readable title: %+v", preset, label)
		}
		for _, part := range []windowSkinPart{meshes.frame, meshes.grip, meshes.controls, meshes.resize} {
			assertWindowSkinOutsideClient(t, part.mesh)
		}
		if preset == "hologram" {
			for _, edge := range []struct {
				token string
				left  bool
			}{{"glow-cyan", true}, {"glow-violet", false}} {
				color := loaded.Color(edge.token)
				minimum, maximum, count := float32(100), float32(-100), 0
				for _, v := range meshes.frame.mesh.Geometry().Vertices() {
					if (edge.left && v.X >= -.5 || !edge.left && v.X <= .5) || abs(v.R-float32(color.R)/255) > .001 || abs(v.G-float32(color.G)/255) > .001 || abs(v.B-float32(color.B)/255) > .001 {
						continue
					}
					minimum, maximum, count = min(minimum, v.X), max(maximum, v.X), count+1
				}
				if count == 0 || (maximum-minimum)*width < 2 {
					t.Fatalf("loaded %s rail lost its separate inner/outer colored strokes", edge.token)
				}
			}
		}
	}
}
