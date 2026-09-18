package workspace

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

func panelPoint(t *testing.T, w *Workspace, x, y float32) (float32, float32) {
	t.Helper()
	point := w.scene.Node(w.panelNode).Transform.TransformPoint(scene.Vec3{X: x/panelWidth - .5, Y: .5 - y/panelHeight})
	sx, sy, _, visible := w.camera.Project(point, w.viewport)
	if !visible {
		t.Fatal("panel test point lies outside the viewport")
	}
	return sx, sy
}

func TestNativePanelSharesCameraAndRetainsTexture(t *testing.T) {
	w := study(t)
	command(t, w, Action{Kind: SetPlayback})
	frame := w.Draw(1440, 900)
	cameraPasses, surfaces, meshes := 0, 0, 0
	for _, command := range frame.Commands {
		if command.Kind != render.SceneCommand {
			continue
		}
		cameraPasses++
		for _, draw := range command.Draws {
			if draw.Texture != nil {
				surfaces++
				if draw.Texture != w.panel.texture {
					t.Fatal("frame did not retain the live panel texture")
				}
			} else if draw.Geometry != nil {
				meshes++
			}
		}
	}
	// Assembly, world-space guides, and the retained housing accent/projection.
	if cameraPasses != 1 || surfaces != 1 || meshes != len(w.nodes)+3 {
		t.Fatalf("panel must share one depth pass with the assembly and spatial guides: cameras=%d surfaces=%d meshes=%d", cameraPasses, surfaces, meshes)
	}
	initial, _ := w.panel.texture.Snapshot(0)
	w.Update(time.Second)
	w.Draw(1440, 900)
	if w.panel.texture.Revision() != initial.Revision {
		t.Fatal("paused panel uploaded an unchanged image")
	}
	command(t, w, Action{Kind: SeekTime, Seconds: 6.01})
	w.Draw(1440, 900)
	updated, changed := w.panel.texture.Snapshot(initial.Revision)
	if !changed || updated.Rect.Min.Y != 84 {
		t.Fatal("small paused seek did not update the data region")
	}
	command(t, w, Action{Kind: SelectComponent, Component: Shaft})
	w.Draw(1440, 900)
	selected, changed := w.panel.texture.Snapshot(updated.Revision)
	if !changed || selected.Rect.Min.Y != 0 || bytes.Equal(initial.Pixels, selected.Pixels) {
		t.Fatal("component selection did not refresh the linked panel content")
	}
}

func TestSurfaceControlsMapThroughPerspectiveAndScale(t *testing.T) {
	w := study(t)
	command(t, w, Action{Kind: TogglePanelDepth})
	command(t, w, Action{Kind: OrbitCamera, DeltaX: 12, DeltaY: -6})
	w.Draw(2880, 1800)
	before := w.Document()
	sx, sy := panelPoint(t, w, 70, 289)
	hit, ok := w.surfaceHit(sx, sy)
	if !ok || math.Abs(float64(hit.PixelX-70)) > .01 || math.Abs(float64(hit.PixelY-289)) > .01 {
		t.Fatalf("projected surface control did not map to its pixels: %+v %v", hit, ok)
	}
	pointer(w, experience.PointerDown, sx, sy)
	pointer(w, experience.PointerUp, sx, sy)
	if w.Document().Timeline.Playing || w.Document().View.Camera != before.View.Camera {
		t.Fatal("panel play button missed or changed the camera")
	}
	// The panel's own placement control and the outside control use one action.
	sx, sy = panelPoint(t, w, 380, 289)
	pointer(w, experience.PointerDown, sx, sy)
	pointer(w, experience.PointerUp, sx, sy)
	if !w.Document().View.PanelBehind {
		t.Fatal("panel button did not move the surface back")
	}
}

func TestSurfaceScrubIsOneUndoableGestureAndCancelRestoresTransport(t *testing.T) {
	w := study(t)
	command(t, w, Action{Kind: TogglePanelDepth})
	w.Draw(1440, 900)
	before := w.Document()
	history := len(w.history)
	sx, sy := panelPoint(t, w, 24+464*.4, 212)
	pointer(w, experience.PointerDown, sx, sy)
	if w.pointer.kind != captureSurfaceTimeline {
		t.Fatal("visible chart did not capture scrub")
	}
	for _, position := range []float32{.5, .6, .7} {
		sx, sy = panelPoint(t, w, 24+464*position, 212)
		pointer(w, experience.PointerMove, sx, sy)
	}
	pointer(w, experience.PointerUp, sx, sy)
	if math.Abs(w.State().Time-21) > .002 || w.State().Playing || len(w.history) != history+1 || w.Document().View.Camera != before.View.Camera {
		t.Fatal("surface scrub was not one isolated timeline edit", w.State())
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("surface scrub undo did not restore transport")
	}
	sx, sy = panelPoint(t, w, 24+464*.6, 212)
	pointer(w, experience.PointerDown, sx, sy)
	w.Handle(experience.Event{Kind: experience.PointerCancel})
	if w.Document() != before || w.historyPosition != history {
		t.Fatal("cancelled surface scrub changed state or history")
	}
}

func TestPanelDepthPersistenceUndoAndOutsideControls(t *testing.T) {
	w := study(t)
	w.Draw(1440, 900)
	rear := w.scene.Node(w.panelNode).Transform
	x, y := panelDepthButton.x+30, panelDepthButton.y+18
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	w.Draw(1440, 900)
	if w.State().PanelBehind || w.scene.Node(w.panelNode).Transform == rear {
		t.Fatal("outside depth control did not move the panel")
	}
	w.Update(time.Second)
	timeline := w.Document().Timeline
	command(t, w, Action{Kind: Undo})
	if !w.State().PanelBehind || w.Document().Timeline != timeline {
		t.Fatal("placement undo changed unrelated playback")
	}
	if !key(w, experience.KeyB, 0) || w.State().PanelBehind {
		t.Fatal("B did not bring the panel forward")
	}
	if key(w, experience.KeyB, experience.ModControl) || w.State().PanelBehind {
		t.Fatal("modified B changed placement")
	}
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	loaded := study(t)
	if err := loaded.LoadState(data); err != nil || loaded.Document() != w.Document() {
		t.Fatalf("panel depth did not survive save/load: %v", err)
	}
	var legacy map[string]any
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	delete(legacy["view"].(map[string]any), "panel_behind")
	data, _ = json.Marshal(legacy)
	if err := loaded.LoadState(data); err != nil || !loaded.State().PanelBehind {
		t.Fatalf("older document did not receive the default rear placement: %v", err)
	}
}

func TestCapturedSurfaceScrubContinuesBeyondPanelBounds(t *testing.T) {
	w := study(t)
	command(t, w, Action{Kind: TogglePanelDepth})
	w.Draw(1440, 900)
	sx, sy := panelPoint(t, w, 400, 212)
	pointer(w, experience.PointerDown, sx, sy)
	if w.pointer.kind != captureSurfaceTimeline {
		t.Fatal("chart did not start capture")
	}
	// This point has left the panel but remains in the camera viewport.
	sx, sy = panelPoint(t, w, 550, 212)
	pointer(w, experience.PointerMove, sx, sy)
	pointer(w, experience.PointerUp, sx, sy)
	if w.State().Time != duration || w.State().Playing {
		t.Fatal("captured surface scrub did not clamp beyond its texture bounds")
	}
}

func TestOccludedPanelCannotStartScrub(t *testing.T) {
	w := study(t)
	w.Draw(1440, 900)
	// Search the shared scene for a chart point covered by the assembly. The
	// same ray must select the mesh, never the panel hidden behind it.
	for y := float32(145); y < 235; y += 8 {
		for x := float32(30); x < 480; x += 8 {
			sx, sy := panelPoint(t, w, x, y)
			hit, ok := w.scene.Pick(w.camera, w.viewport, sx, sy)
			if !ok || hit.Surface {
				continue
			}
			before := w.Document().Timeline
			pointer(w, experience.PointerDown, sx, sy)
			if w.pointer.kind != captureOrbit || w.Document().Timeline != before {
				t.Fatal("occluded surface intercepted a mesh press")
			}
			w.Handle(experience.Event{Kind: experience.PointerCancel})
			return
		}
	}
	t.Fatal("initial rear panel does not demonstrate mesh occlusion")
}
