package workspace

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
)

func portalFixture(t *testing.T, count int) (*Workspace, *fakeApplications) {
	t.Helper()
	w := desktop(t)
	texture, err := render.NewTexture(640, 400, make([]byte, 640*400*4))
	if err != nil {
		t.Fatal(err)
	}
	apps := &fakeApplications{}
	for i := 0; i < count; i++ {
		apps.surfaces = append(apps.surfaces, experience.ApplicationSurface{
			ID: uint64(950 + i), Key: fmt.Sprintf("portal/window-%d", i+1),
			Title: fmt.Sprintf("Tool %d", i+1), Texture: texture,
		})
	}
	w.SetApplications(apps)
	w.Draw(1440, 900)
	return w, apps
}

func TestSpatialPortalsNavigateGroupsAcrossSpacesWithoutMovingWindows(t *testing.T) {
	w, apps := portalFixture(t, 3)
	for _, action := range []Action{
		{Kind: SelectApplication, ApplicationKey: apps.surfaces[0].Key},
		{Kind: SelectApplication, ApplicationKey: apps.surfaces[1].Key, Additive: true},
		{Kind: GroupApplications},
		{Kind: MoveApplications, DeltaX: 24, DeltaY: -7, DeltaDepth: 3},
		{Kind: CreateSpace, SpaceName: "Research"},
		{Kind: SelectApplication, ApplicationKey: apps.surfaces[2].Key},
		{Kind: MoveToSpace, Space: 1},
	} {
		command(t, w, action)
	}
	portals := w.SpatialPortals()
	if len(portals) != 2 || portals[0].Applications != 2 || portals[0].Group == 0 || portals[1].SpaceName != "Research" || portals[1].Applications != 1 {
		t.Fatalf("unexpected portal catalog: %+v", portals)
	}
	placements := w.Document().View.Application.Layouts
	before := w.Document()
	command(t, w, Action{Kind: NavigatePortal, ApplicationKey: apps.surfaces[2].Key})
	after := w.Document()
	placement := after.View.Application.Layouts[after.View.Application.index(apps.surfaces[2].Key)]
	if after.View.Application.Space != 1 || after.View.Application.Active != apps.surfaces[2].Key || after.View.Application.Selected == 0 {
		t.Fatal("portal did not select its remote application and space")
	}
	if after.View.Camera.TargetX != placement.X || after.View.Camera.TargetY != placement.Y || after.View.Camera.TargetDepth != placement.Depth || after.View.Camera.Zoom < -.2 || after.View.Camera.Zoom > .25 {
		t.Fatalf("portal did not center a detail camera: %+v / %+v", after.View.Camera, placement)
	}
	if after.View.Application.Layouts != placements || w.OwnsKeyboard() {
		t.Fatal("portal travel moved windows or granted application input focus")
	}
	w.Draw(1440, 900)
	right, up, normal := applicationBasis()
	wantTarget := right.Mul(placement.X).Add(up.Mul(placement.Y)).Add(normal.Mul(placement.Depth))
	if w.camera.Target.Sub(wantTarget).Length() > .0001 {
		t.Fatalf("render camera did not use the saved portal target: %+v / %+v", w.camera.Target, wantTarget)
	}
	visibleApplication(t, w, apps.surfaces[2])
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("one undo did not return from portal travel")
	}
	command(t, w, Action{Kind: Redo})
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	restored := desktop(t)
	if err = restored.LoadState(data); err != nil || restored.Document().View.Camera != w.Document().View.Camera {
		t.Fatal("portal camera target did not survive session restore", err)
	}
}

func TestPortalNavigationValidatesTargetsTransactionally(t *testing.T) {
	w, _ := portalFixture(t, 1)
	before := w.Document()
	for _, action := range []Action{
		{Kind: NavigatePortal, Space: 15},
		{Kind: NavigatePortal, ApplicationKey: "missing"},
	} {
		if err := w.Dispatch(action); err == nil || w.Document() != before {
			t.Fatalf("invalid portal action changed the document: %+v", action)
		}
	}
	bad := before
	bad.View.Camera.TargetX = 101
	if err := bad.Validate(); err == nil {
		t.Fatal("accepted an out-of-bounds camera target")
	}
	bad = before
	bad.View.Camera.TargetDepth = float32(math.NaN())
	if err := bad.Validate(); err == nil {
		t.Fatal("accepted a non-finite camera target")
	}
	state, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err = json.Unmarshal(state, &object); err != nil {
		t.Fatal(err)
	}
	object["camera"].(map[string]any)["target_y"] = 1000
	state, _ = json.Marshal(object)
	if err = w.LoadState(state); err == nil || w.Document() != before {
		t.Fatal("invalid saved portal target changed the live workspace")
	}
}

func TestPortalNavigationRestoresAndActivatesMinimizedTarget(t *testing.T) {
	w, apps := portalFixture(t, 2)
	target, sibling := apps.surfaces[0], apps.surfaces[1]
	command(t, w, Action{Kind: ToggleApplicationMinimized, ApplicationKey: target.Key})
	w.Update(0)
	if w.m.applicationState.Active != sibling.Key || w.application.ID != sibling.ID {
		t.Fatal("fixture did not select the visible sibling after minimizing the target")
	}

	command(t, w, Action{Kind: NavigatePortal, ApplicationKey: target.Key})
	w.Update(0)
	view := w.Document().View.Application
	i := view.index(target.Key)
	if i < 0 || view.Layouts[i].Minimized || view.Active != target.Key || view.Selected != 1<<i || w.application.ID != target.ID {
		t.Fatalf("portal did not restore and retain its requested target: %+v / live=%d", view, w.application.ID)
	}
	w.Draw(1440, 900)
	if node := w.scene.Node(w.applicationNodes[target.ID]); node == nil || node.Hidden || node.Surface != target.Texture {
		t.Fatal("restored portal target was not visible in the spatial scene")
	}
}

func formerInlinePortalBox(w *Workspace, portal SpatialPortal) (box, bool) {
	right, up, normal := applicationBasis()
	point := right.Mul(portal.X).Add(up.Mul(portal.Y)).Add(normal.Mul(portal.Depth + .08))
	x, y, _, visible := w.camera.Project(point, w.viewport)
	if !visible {
		return box{}, false
	}
	x, y = (x-w.ox)/w.scale, (y-w.oy)/w.scale
	left := (w.viewport.X-w.ox)/w.scale + 8
	top := (w.viewport.Y-w.oy)/w.scale + 8
	rightEdge := (w.viewport.X+w.viewport.Width-w.ox)/w.scale - 8
	bottom := (w.viewport.Y+w.viewport.Height-w.oy)/w.scale - 8
	marker := box{x - 104, y - 25, 208, 50}
	marker.x = max(left, min(marker.x, rightEdge-marker.w))
	marker.y = max(top, min(marker.y, bottom-marker.h))
	return marker, true
}

func frameHasPortalCardRect(w *Workspace, frame render.Frame, marker box) bool {
	x0, y0 := w.ox+marker.x*w.scale, w.oy+marker.y*w.scale
	x1, y1 := w.ox+(marker.x+marker.w)*w.scale, w.oy+(marker.y+marker.h)*w.scale
	want := [][2]float32{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y0}, {x1, y1}, {x0, y1}}
	for start := 0; start+len(want) <= len(frame.Vertices); start++ {
		match := true
		for i, point := range want {
			vertex := frame.Vertices[start+i]
			if math.Abs(float64(vertex.X-point[0])) > .001 || math.Abs(float64(vertex.Y-point[1])) > .001 {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestCameraZoomDoesNotDrawOrCaptureInlinePortalCards(t *testing.T) {
	w, apps := portalFixture(t, 3)
	for _, action := range []Action{
		{Kind: SelectApplication, ApplicationKey: apps.surfaces[0].Key},
		{Kind: SelectApplication, ApplicationKey: apps.surfaces[1].Key, Additive: true},
		{Kind: GroupApplications},
	} {
		command(t, w, action)
	}
	w.Draw(1440, 900)
	command(t, w, Action{Kind: ZoomCamera, DeltaZoom: -.3})
	if w.Document().View.Camera.Zoom > -.24 {
		t.Fatal("fixture did not reach the former group-card zoom range")
	}
	groupFrame := w.Draw(1440, 900)
	var group SpatialPortal
	for _, portal := range w.SpatialPortals() {
		if portal.Applications == 2 {
			group = portal
			break
		}
	}
	marker, visible := formerInlinePortalBox(w, group)
	if group.ID == "" || !visible {
		t.Fatal("fixture could not locate the former group portal card")
	}
	if frameHasPortalCardRect(w, groupFrame, marker) {
		t.Fatal("group zoom retained the old inline portal card background")
	}
	x := w.ox + (marker.x+marker.w/2)*w.scale
	y := w.oy + (marker.y+marker.h/2)*w.scale
	before, beforeEvents := w.Document(), len(apps.events)
	for _, event := range []experience.Event{
		{Kind: experience.PointerMove, X: x, Y: y},
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y},
		{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x, Y: y},
	} {
		if w.handlePortalNavigation(event) {
			t.Fatal("the removed inline portal card retained an invisible pointer target")
		}
	}
	if w.Document() != before || len(apps.events) != beforeEvents || w.OwnsKeyboard() || w.pointer.kind != captureNone {
		t.Fatal("former portal-card input changed navigation or application ownership")
	}

	command(t, w, Action{Kind: ZoomCamera, DeltaZoom: -.8})
	if w.Document().View.Camera.Zoom > -.55 {
		t.Fatal("fixture did not reach the former atlas-card zoom range")
	}
	atlasFrame := w.Draw(1440, 900)
	for _, portal := range w.SpatialPortals() {
		marker, visible := formerInlinePortalBox(w, portal)
		if visible && frameHasPortalCardRect(w, atlasFrame, marker) {
			t.Fatal("far zoom retained an inline portal card instead of leaving portals in the atlas")
		}
	}
}

func TestPortalFooterStillOpensAtlasAtFarZoom(t *testing.T) {
	w, _ := portalFixture(t, 2)
	command(t, w, Action{Kind: ZoomCamera, DeltaZoom: -.8})
	baseVertices := len(w.Draw(1440, 900).Vertices)
	before := w.Document()
	x := w.ox + (portalButton.x+portalButton.w/2)*w.scale
	y := w.oy + (portalButton.y+portalButton.h/2)*w.scale
	if !pointer(w, experience.PointerDown, x, y) || !pointer(w, experience.PointerUp, x, y) || !w.portals.open {
		t.Fatal("far camera zoom removed the persistent portal footer or its atlas action")
	}
	if w.Document() != before || w.pointer.kind != captureNone {
		t.Fatal("opening the portal atlas through its footer changed workspace state")
	}
	if frame := w.Draw(1440, 900); len(frame.Vertices) <= baseVertices {
		t.Fatal("portal footer opened no visible atlas")
	}
}

func TestPortalAtlasAndReservedCyclingWorkFromApplicationFocus(t *testing.T) {
	w, apps := portalFixture(t, 2)
	command(t, w, Action{Kind: CreateSpace, SpaceName: "Build"})
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[1].Key})
	command(t, w, Action{Kind: MoveToSpace, Space: 1})
	w.Draw(1440, 900)
	x, y := visibleApplication(t, w, apps.surfaces[0])
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if !w.OwnsKeyboard() {
		t.Fatal("fixture did not focus an application")
	}
	beforeEvents := len(apps.events)
	open := experience.Event{Kind: experience.KeyInput, Key: experience.KeyG, Keycode: 34, Pressed: true, Modifiers: experience.ModControl | experience.ModAlt}
	if !w.Handle(open) || !w.portals.open || w.OwnsKeyboard() {
		t.Fatal("reserved portal chord did not open the atlas and revoke app focus")
	}
	if frame := w.Draw(1440, 900); len(frame.Vertices) == 0 || len(frame.Commands) == 0 {
		t.Fatal("portal atlas did not produce a renderable frame")
	}
	afterOpen := len(apps.events)
	if afterOpen <= beforeEvents {
		t.Fatal("opening portals did not cancel focused client input")
	}
	open.Pressed, open.Modifiers = false, 0
	if !w.Handle(open) || len(apps.events) != afterOpen {
		t.Fatal("portal chord release leaked or produced unexpected client input")
	}
	// Choose the Build portal and travel with a fresh Enter. Its release remains
	// owned after the atlas closes.
	right := experience.Event{Kind: experience.KeyInput, Key: experience.KeyRight, Keycode: 106, Pressed: true}
	w.Handle(right)
	right.Pressed = false
	w.Handle(right)
	enter := experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: true}
	if !w.Handle(enter) || w.portals.open || w.Document().View.Application.Space != 1 || w.OwnsKeyboard() {
		t.Fatal("portal atlas Enter did not travel without client focus")
	}
	enter.Pressed = false
	if !w.Handle(enter) || len(apps.events) != afterOpen {
		t.Fatal("atlas Enter release reached the selected application")
	}
	cycle := experience.Event{Kind: experience.KeyInput, Key: experience.KeyLeft, Keycode: 105, Pressed: true, Modifiers: experience.ModControl | experience.ModAlt}
	if !w.Handle(cycle) || w.Document().View.Application.Space != 0 || w.portals.open {
		t.Fatal("reserved direct portal cycling did not return to the previous group")
	}
	cycle.Pressed, cycle.Modifiers = false, 0
	if !w.Handle(cycle) {
		t.Fatal("direct portal cycle did not retain its release")
	}
}

func TestPortalTargetsAreDiscoverableInToolsPalette(t *testing.T) {
	w, _ := portalFixture(t, 2)
	if !w.openCommands() {
		t.Fatal("could not open tools palette")
	}
	w.commands.field.Set("Portal / Main")
	w.refreshCommands()
	count := 0
	for _, entry := range w.commands.entries {
		if entry.action.Kind == NavigatePortal {
			count++
			if entry.action.ApplicationKey == "" {
				t.Fatalf("palette portal lacks a navigation target: %+v", entry)
			}
		}
	}
	if count != 2 {
		t.Fatalf("tools palette did not expose both window portals: %+v", w.commands.entries)
	}
}
