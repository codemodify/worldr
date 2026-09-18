package workspace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

func multipleApplications(t *testing.T, count int) (*Workspace, *fakeApplications) {
	t.Helper()
	w := study(t)
	texture, err := render.NewTexture(960, 600, make([]byte, 960*600*4))
	if err != nil {
		t.Fatal(err)
	}
	apps := &fakeApplications{}
	for i := 0; i < count; i++ {
		apps.surfaces = append(apps.surfaces, experience.ApplicationSurface{ID: uint64(700 + i), Key: fmt.Sprintf("launch-%d/window-1", i+1), Title: fmt.Sprintf("Terminal %d", i+1), AppID: "foot", Texture: texture})
	}
	w.SetApplications(apps)
	w.Draw(1440, 900)
	return w, apps
}

func projectedApplication(w *Workspace, surface experience.ApplicationSurface, x, y float32) (float32, float32) {
	tw, th := surface.Texture.Size()
	point := w.scene.Node(w.applicationNodes[surface.ID]).Transform.TransformPoint(scene.Vec3{X: x/float32(tw) - .5, Y: .5 - y/float32(th)})
	sx, sy, _, _ := w.camera.Project(point, w.viewport)
	return sx, sy
}

func visibleApplication(t *testing.T, w *Workspace, surface experience.ApplicationSurface) (float32, float32) {
	t.Helper()
	for y := float32(40); y < 600; y += 40 {
		for x := float32(60); x < 960; x += 60 {
			sx, sy := projectedApplication(w, surface, x, y)
			if hit, ok := w.applicationHit(sx, sy); ok && hit.Node == w.applicationNodes[surface.ID] {
				return sx, sy
			}
		}
	}
	t.Fatalf("application %s has no visible point", surface.Key)
	return 0, 0
}

func TestMultipleApplicationsKeepFocusHoverAndCaptureIndependent(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	frame := w.Draw(1440, 900)
	surfaces, meshes := 0, 0
	for _, pass := range frame.Commands {
		if pass.Kind == render.SceneCommand {
			for _, draw := range pass.Draws {
				if draw.Texture != nil {
					surfaces++
				} else {
					meshes++
				}
			}
		}
	}
	// Assembly, world-space guides, housing accent/projection, and a frame and grip per app.
	if surfaces != 2 || meshes != len(w.nodes)+3+2*len(apps.surfaces) {
		t.Fatalf("shared spatial scene has %d apps and %d meshes", surfaces, meshes)
	}
	ax, ay := visibleApplication(t, w, apps.surfaces[0])
	bx, by := visibleApplication(t, w, apps.surfaces[1])
	pointer(w, experience.PointerDown, ax, ay)
	pointer(w, experience.PointerUp, ax, ay)
	pointer(w, experience.PointerMove, bx, by)
	if w.applicationFocusedID != 700 || w.applicationHoveredID != 701 {
		t.Fatal("hovering another app changed keyboard focus")
	}
	key(w, experience.KeyO, 0)
	if last := apps.events[len(apps.events)-1]; last.id != 700 || last.event.Key != experience.KeyO || w.m.applicationState.Overview {
		t.Fatal("focused app's O key opened the overview")
	}
	pointer(w, experience.PointerDown, bx, by)
	if w.applicationFocusedID != 701 || w.applicationCapturedID != 701 {
		t.Fatal("second app did not acquire focus and capture")
	}
	pointer(w, experience.PointerMove, ax, ay)
	if apps.events[len(apps.events)-1].id != 701 {
		t.Fatal("selection moved to the app under the pointer")
	}
	// Closing a different client cannot interrupt the active client gesture.
	apps.surfaces = apps.surfaces[1:]
	w.Update(time.Millisecond)
	if !w.OwnsKeyboard() || w.applicationCapturedID != 701 {
		t.Fatal("unrelated app closure stole keyboard or capture")
	}
	pointer(w, experience.PointerUp, ax, ay)
	if len(w.applicationButtons) != 0 || apps.events[len(apps.events)-1].id != 701 {
		t.Fatal("captured release went to the wrong client")
	}
	apps.surfaces = nil
	w.Update(0)
	if w.OwnsKeyboard() || len(w.applicationNodes) != 0 {
		t.Fatal("last app closure left stale input or scene nodes")
	}
}

func TestGroupedPlacementIsOneUndoableGestureAndCancels(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[0].Key})
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[1].Key, Additive: true})
	command(t, w, Action{Kind: GroupApplications})
	command(t, w, Action{Kind: ToggleApplicationPlacement})
	w.Draw(1440, 900)
	a, b := w.Document().View.Application.Layouts[0], w.Document().View.Application.Layouts[1]
	if a.Group == 0 || a.Group != b.Group {
		t.Fatal("selected apps were not grouped")
	}
	x, y := visibleApplication(t, w, apps.surfaces[1])
	before, history := w.Document(), len(w.history)
	pointer(w, experience.PointerDown, x, y)
	if w.pointer.kind != captureApplicationPlacement || w.OwnsKeyboard() {
		t.Fatal("placement mode forwarded pointer input to the client")
	}
	for _, delta := range []float32{10, 25, 40} {
		pointer(w, experience.PointerMove, x+delta, y+delta/2)
	}
	pointer(w, experience.PointerUp, x+40, y+20)
	after := w.Document()
	aa, bb := after.View.Application.Layouts[0], after.View.Application.Layouts[1]
	if aa.X == a.X || math.Abs(float64((aa.X-a.X)-(bb.X-b.X))) > .001 || math.Abs(float64((aa.Y-a.Y)-(bb.Y-b.Y))) > .001 {
		t.Fatal("group members did not move by the same world displacement")
	}
	if len(w.history) != history+1 || after.View.Camera != before.View.Camera {
		t.Fatal("placement gesture changed camera or created several undo edits")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("placement undo did not restore both group members")
	}
	w.Draw(1440, 900)
	x, y = visibleApplication(t, w, apps.surfaces[1])
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerMove, x+30, y+30)
	w.Handle(experience.Event{Kind: experience.PointerCancel})
	if w.Document() != before || w.pointer.kind != captureNone {
		t.Fatal("cancelled group drag persisted layout edits")
	}
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[0].Key})
	command(t, w, Action{Kind: MoveApplications, DeltaDepth: -2})
	after = w.Document()
	if after.View.Application.Layouts[0].Depth != a.Depth-2 || after.View.Application.Layouts[1].Depth != b.Depth-2 {
		t.Fatal("depth movement did not carry the selected app's group")
	}
	command(t, w, Action{Kind: UngroupApplications})
	if w.Document().View.Application.Layouts[0].Group != 0 || w.Document().View.Application.Layouts[1].Group != 0 {
		t.Fatal("ungroup did not release every member")
	}
}

func TestOverviewRetrievesAll32AppsAndNeverSendsInput(t *testing.T) {
	w, apps := multipleApplications(t, 32)
	// Move one app far outside the ordinary camera, then retrieve it from the grid.
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[31].Key})
	command(t, w, Action{Kind: MoveApplications, DeltaX: 60, DeltaDepth: -15})
	positions := w.Document().View.Application.Layouts
	command(t, w, Action{Kind: ToggleApplicationOverview})
	w.Draw(1440, 900)
	for _, surface := range apps.surfaces {
		x, y := projectedApplication(w, surface, 480, 300)
		hit, ok := w.applicationHit(x, y)
		if !ok || hit.Node != w.applicationNodes[surface.ID] {
			t.Fatalf("overview cannot retrieve %s", surface.Key)
		}
	}
	count := len(apps.events)
	x, y := projectedApplication(w, apps.surfaces[0], 480, 300)
	w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y, Modifiers: experience.ModShift})
	pointer(w, experience.PointerUp, x, y)
	if !w.m.applicationState.Overview || len(apps.events) != count || w.OwnsKeyboard() {
		t.Fatal("overview selection forwarded client input or left overview")
	}
	command(t, w, Action{Kind: GroupApplications})
	x, y = projectedApplication(w, apps.surfaces[31], 480, 300)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if w.m.applicationState.Overview || w.application.ID != 731 || w.OwnsKeyboard() {
		t.Fatal("overview did not retrieve selected app without granting keyboard focus")
	}
	command(t, w, Action{Kind: ToggleApplicationReading})
	w.Draw(1440, 900)
	if _, ok := w.applicationHit(w.viewport.X+w.viewport.Width/2, w.viewport.Y+w.viewport.Height/2); !ok {
		t.Fatal("read mode did not retrieve off-screen selected app")
	}
	for i, p := range w.Document().View.Application.Layouts {
		if p.X != positions[i].X || p.Y != positions[i].Y || p.Depth != positions[i].Depth {
			t.Fatal("overview/read changed saved spatial placement")
		}
	}
}

func TestStableApplicationLayoutRestoresWithDifferentProtocolIDs(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[1].Key})
	command(t, w, Action{Kind: MoveApplications, DeltaX: 3, DeltaY: -2, DeltaDepth: -4})
	command(t, w, Action{Kind: ToggleApplicationSize})
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[0].Key, Additive: true})
	command(t, w, Action{Kind: GroupApplications})
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("Terminal")) || bytes.Contains(data, []byte("\"id\"")) {
		t.Fatal("runtime app metadata leaked into saved layout")
	}
	loaded := study(t)
	if err := loaded.LoadState(data); err != nil {
		t.Fatal(err)
	}
	reconnected := &fakeApplications{surfaces: append([]experience.ApplicationSurface(nil), apps.surfaces...)}
	reconnected.surfaces[0].ID = 9001
	reconnected.surfaces[1].ID = 9002
	// Connection order changes without changing which saved layout is applied.
	reconnected.surfaces[0], reconnected.surfaces[1] = reconnected.surfaces[1], reconnected.surfaces[0]
	loaded.SetApplications(reconnected)
	loaded.Draw(1440, 900)
	if loaded.Document() != w.Document() {
		t.Fatal("stable-key restoration depended on protocol IDs or connection order")
	}
	for _, resize := range reconnected.resizes {
		if resize.id == 9002 && resize.width != 1440 {
			t.Fatal("per-app wide preference did not restore")
		}
	}
	if loaded.OwnsKeyboard() {
		t.Fatal("restoring layout granted keyboard focus")
	}
}

func TestApplicationRestoreWaitsForSavedSelectionAndLoadingLiveLayoutIsSafe(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[1].Key})
	command(t, w, Action{Kind: ToggleApplicationReading})
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	loaded := study(t)
	if err := loaded.LoadState(data); err != nil {
		t.Fatal(err)
	}
	arriving := &fakeApplications{surfaces: append([]experience.ApplicationSurface(nil), apps.surfaces[:1]...)}
	loaded.SetApplications(arriving)
	loaded.Draw(1440, 900)
	if loaded.application.ID != apps.surfaces[0].ID {
		t.Fatal("first available app was not displayed during launch")
	}
	arriving.surfaces = append(arriving.surfaces, apps.surfaces[1])
	loaded.Update(0)
	loaded.Draw(1440, 900)
	if loaded.application.Key != apps.surfaces[1].Key || loaded.Document() != w.Document() || loaded.OwnsKeyboard() {
		t.Fatal("late mapping lost saved active app or granted keyboard focus")
	}
	empty := study(t)
	emptyData, _ := empty.SaveState()
	if err := loaded.LoadState(emptyData); err != nil {
		t.Fatal(err)
	}
	loaded.Draw(1440, 900)
	if len(loaded.applicationNodes) != 2 || loaded.Document().Validate() != nil {
		t.Fatal("loading an empty layout did not safely attach existing live apps")
	}
}

func TestInvalidApplicationLayoutsAreTransactionalAndCapacityBounded(t *testing.T) {
	w, _ := multipleApplications(t, 2)
	before := w.Document()
	for _, modify := range []func(*Document){
		func(d *Document) { d.View.Application.Layouts[0].X = float32(math.Inf(1)) },
		func(d *Document) { d.View.Application.Layouts[0].Depth = 41 },
		func(d *Document) { d.View.Application.Layouts[1].Key = d.View.Application.Layouts[0].Key },
		func(d *Document) { d.View.Application.Layouts[0].Group = 33 },
		func(d *Document) { d.View.Application.Active = "missing" },
		func(d *Document) { d.View.Application.Selected |= 1 << 31 },
	} {
		d := before
		modify(&d)
		if d.Validate() == nil {
			t.Fatal("invalid application layout accepted")
		}
	}
	data, _ := w.SaveState()
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	application := decoded["view"].(map[string]any)["application"].(map[string]any)
	application["layouts"] = append(application["layouts"].([]any), map[string]any{"key": "overflow"})
	invalid, _ := json.Marshal(decoded)
	if err := w.LoadState(invalid); err == nil || w.Document() != before {
		t.Fatal("oversized layout silently truncated or changed document")
	}
	if err := w.Dispatch(Action{Kind: MoveApplications, DeltaDepth: 100}); err == nil || w.Document() != before {
		t.Fatal("invalid movement changed layout")
	}
	full, fullApps := multipleApplications(t, 32)
	for i := range fullApps.surfaces {
		fullApps.surfaces[i].Key = "new/" + fullApps.surfaces[i].Key
		fullApps.surfaces[i].ID += 10000
	}
	full.Update(0)
	full.Draw(1440, 900)
	if !full.applicationLayoutFull || len(full.applicationNodes) != 0 {
		t.Fatal("exhausted saved layout silently reused old keys")
	}
}

func TestPlacementCancellationRetainsNewlyMappedApplication(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	command(t, w, Action{Kind: ToggleApplicationPlacement})
	w.Draw(1440, 900)
	x, y := visibleApplication(t, w, apps.surfaces[0])
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerMove, x+20, y+10)
	if w.pointer.kind != captureApplicationPlacement {
		t.Fatal("placement gesture did not start")
	}
	newSurface := apps.surfaces[0]
	newSurface.ID = 999
	newSurface.Key = "new-launch/window-1"
	apps.surfaces = []experience.ApplicationSurface{apps.surfaces[1], newSurface}
	w.Update(0)
	w.Draw(1440, 900)
	if w.pointer.kind != captureNone || w.Document().View.Application.index(newSurface.Key) < 0 || len(w.applicationNodes) != 2 || w.Document().Validate() != nil {
		t.Fatal("cancelling a disconnected placement lost the newly mapped app")
	}
}
