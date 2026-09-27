package workspace

import (
	"math"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
)

const ambientCatWindowImpulseSpeed = 2.1

type ambientCatPawScreen struct {
	x, y  float32
	valid bool
}

type ambientCatCollisionState struct {
	contacts map[uint64]bool
	paws     [2]ambientCatPawScreen
}

func (w *Workspace) resetAmbientCatCollisions() {
	w.ambientCatCollisions = ambientCatCollisionState{}
}

// updateAmbientCatCollisions observes the two decorative front paws after the
// cat pose advances. The visual topmost application under a paw owns contact.
// A rising edge creates one physical impulse; sustained overlap cannot generate
// a new history entry every frame.
func (w *Workspace) updateAmbientCatCollisions() {
	if !w.desktop || w.skinBackdropVisible() || w.ambientCat == nil || !w.environment.Cat || !w.environment.CatCollisions ||
		w.settingsOpen || w.helpOpen || w.m.applicationState.Reading || w.m.applicationState.Overview ||
		w.m.applicationState.Placing || len(w.applicationSurfaces) == 0 {
		w.resetAmbientCatCollisions()
		return
	}

	w.layout(w.width, w.height)
	w.syncScene()
	current := make(map[uint64]bool, 2)
	currentPaws := [2]ambientCatPawScreen{}
	type contact struct {
		surface experience.ApplicationSurface
		leg     int
		dx, dy  float32
	}
	contacts := make([]contact, 0, 2)
	seen := make(map[uint64]bool, 2)

	for leg := 0; leg < 2; leg++ {
		point, ok := w.catWorldPaw(leg)
		if !ok {
			continue
		}
		x, y, _, visible := w.camera.Project(point, w.viewport)
		if !visible || !finite(float64(x)) || !finite(float64(y)) {
			continue
		}
		currentPaws[leg] = ambientCatPawScreen{x: x, y: y, valid: true}
		surface, hit := w.catApplicationAt(x, y)
		if !hit {
			continue
		}
		current[surface.ID] = true
		if w.ambientCatCollisions.contacts == nil || w.ambientCatCollisions.contacts[surface.ID] || seen[surface.ID] {
			continue
		}
		dx, dy := float32(0), float32(0)
		if prior := w.ambientCatCollisions.paws[leg]; prior.valid {
			dx, dy = x-prior.x, y-prior.y
		}
		if dx*dx+dy*dy < .04 {
			dx, dy = w.ambientCatScreenDirection(point)
		}
		seen[surface.ID] = true
		contacts = append(contacts, contact{surface: surface, leg: leg, dx: dx, dy: dy})
	}

	// The first observed frame establishes contact and paw velocity baselines.
	// Enabling the switch or opening a window under a resting paw therefore does
	// not manufacture an impact; the cat must leave and make fresh contact.
	initial := w.ambientCatCollisions.contacts == nil
	w.ambientCatCollisions.contacts = current
	w.ambientCatCollisions.paws = currentPaws
	if initial || ambientCatBehavior(w.ambientCat.phase).activity != ambientCatRunning {
		return
	}

	for _, hit := range contacts {
		selected := w.m.applicationState.movementSelectionFor(hit.surface.Key)
		if selected == 0 || w.ambientCatTargetHeld(selected) {
			continue
		}
		vx, vy, ok := w.catPlacementImpulse(hit.surface, hit.dx, hit.dy)
		if !ok || !w.applyWindowImpulse(hit.surface.Key, vx, vy, 0) {
			continue
		}
		w.ambientCat.triggerSwat(hit.leg)
	}
}

func (w *Workspace) ambientCatTargetHeld(selected uint32) bool {
	switch w.pointer.kind {
	case captureApplicationPlacement:
		return w.pointer.windowSelection&selected != 0
	case captureApplicationResize:
		return w.m.applicationState.movementSelectionFor(w.pointer.resizeKey)&selected != 0
	}
	return false
}

func (w *Workspace) catApplicationAt(x, y float32) (experience.ApplicationSurface, bool) {
	hit, ok := w.scene.Pick(w.camera, w.viewport, x, y)
	if !ok {
		return experience.ApplicationSurface{}, false
	}
	for _, surface := range w.applicationSurfaces {
		if w.applicationNodes[surface.ID] == hit.Node || w.applicationFrames[surface.ID] == hit.Node ||
			w.applicationDragHandles[surface.ID] == hit.Node || w.applicationWindowControls[surface.ID] == hit.Node ||
			w.applicationResizeHandles[surface.ID] == hit.Node {
			return surface, true
		}
		if mount := w.applicationSpatial[surface.ID]; mount != nil && mount.objects[hit.Node] != 0 {
			return surface, true
		}
	}
	return experience.ApplicationSurface{}, false
}

func (w *Workspace) catWorldPaw(leg int) (scene.Vec3, bool) {
	local, ok := w.ambientCat.pawPosition(leg)
	if !ok {
		return scene.Vec3{}, false
	}
	return roomPlacement(catLandmarkCenter()).TransformPoint(local), true
}

func (w *Workspace) ambientCatScreenDirection(position scene.Vec3) (float32, float32) {
	behavior := ambientCatBehavior(w.ambientCat.phase)
	_, tangent := ambientCatPath(behavior.routePhase)
	if tangent.Length() <= 1e-5 {
		return 1, 0
	}
	// position is already in the room. Advance along the route's room direction.
	forward := roomPlacement(catLandmarkCenter()).TransformVector(tangent.Normalize())
	x, y, _, visible := w.camera.Project(position, w.viewport)
	xx, yy, _, ahead := w.camera.Project(position.Add(forward.Mul(.2)), w.viewport)
	if !visible || !ahead {
		return 1, 0
	}
	return xx - x, yy - y
}

// catPlacementImpulse solves the screen projection of the application's fixed
// X/Y placement basis. The resulting world-space impulse follows the visible
// paw direction even after the user orbits the scene.
func (w *Workspace) catPlacementImpulse(surface experience.ApplicationSurface, dx, dy float32) (float64, float64, bool) {
	length := float32(math.Hypot(float64(dx), float64(dy)))
	if length <= 1e-5 {
		return 0, 0, false
	}
	dx, dy = dx/length, dy/length
	_, center, _, _ := w.applicationTransformFor(surface)
	right, up, _ := applicationBasis()
	cx, cy, _, centerVisible := w.camera.Project(center, w.viewport)
	rx, ry, _, rightVisible := w.camera.Project(center.Add(right), w.viewport)
	ux, uy, _, upVisible := w.camera.Project(center.Add(up), w.viewport)
	if !centerVisible || !rightVisible || !upVisible {
		return 0, 0, false
	}
	ax, ay, bx, by := rx-cx, ry-cy, ux-cx, uy-cy
	determinant := ax*by - ay*bx
	if abs(determinant) <= 1e-5 {
		return 0, 0, false
	}
	placementX := (dx*by - dy*bx) / determinant
	placementY := (ax*dy - ay*dx) / determinant
	placementLength := float32(math.Hypot(float64(placementX), float64(placementY)))
	if placementLength <= 1e-5 || !finite(float64(placementLength)) {
		return 0, 0, false
	}
	scale := float32(ambientCatWindowImpulseSpeed) / placementLength
	return float64(placementX * scale), float64(placementY * scale), true
}
