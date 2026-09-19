package workspace

import (
	"fmt"
	"math"
	"math/bits"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
)

var (
	_                         experience.ApplicationAware            = (*Workspace)(nil)
	_                         experience.KeyboardOwner               = (*Workspace)(nil)
	_                         experience.ApplicationPlacementChecker = (*Workspace)(nil)
	applicationOverviewButton                                        = box{32, 214, 208, 38}
	applicationPlaceButton                                           = box{32, 258, 208, 38}
	applicationReadButton                                            = box{32, 302, 208, 38}
	applicationDepthButton                                           = box{32, 346, 208, 38}
	applicationBackButton                                            = box{32, 390, 100, 38}
	applicationFrontButton                                           = box{140, 390, 100, 38}
	applicationSizeButton                                            = box{32, 434, 208, 38}
	applicationGroupButton                                           = box{32, 478, 208, 38}
	applicationUngroupButton                                         = box{32, 522, 208, 38}
	applicationCloseButton                                           = box{32, 744, 208, 32}
	applicationLaunchButton                                          = box{535, 30, 180, 37}
)

func (w *Workspace) SetApplications(applications experience.Applications) {
	w.finishWindowThrow()
	w.resetApplicationReadClick()
	w.clearApplicationFocus()
	w.cancelPointer()
	for _, node := range w.applicationNodes {
		w.scene.Remove(node)
	}
	w.applicationNodes = make(map[uint64]scene.NodeID)
	w.applicationSpatial = make(map[uint64]*spatialMount)
	w.applicationFrames = make(map[uint64]scene.NodeID)
	w.applicationDragHandles = make(map[uint64]scene.NodeID)
	w.applicationWindowControls = make(map[uint64]scene.NodeID)
	w.applicationResizeHandles = make(map[uint64]scene.NodeID)
	w.applicationKeys = make(map[uint64]string)
	w.applicationSurfaces = nil
	w.applicationNode = 0
	w.application = experience.ApplicationSurface{}
	w.applications = applications
	w.syncApplications()
}

func (w *Workspace) OwnsKeyboard() bool {
	w.syncApplications()
	return w.applicationKeyboard || w.commands != nil && w.commands.open
}

// CheckApplicationPlacement checks saved slots and current provider surfaces
// without changing workspace view, history or layouts. Existing keys can reuse
// their slots even when full. Unregistered live keys also need future slots;
// providers may have published them earlier in this same host iteration.
func (w *Workspace) CheckApplicationPlacement(key string) error {
	if !validApplicationKey(key) {
		return fmt.Errorf("invalid application layout key")
	}
	if w.m.applicationState.index(key) >= 0 {
		return nil
	}
	available := 0
	for _, placement := range w.m.applicationState.Layouts {
		if placement.Key == "" {
			available++
		}
	}
	if w.applications != nil && available > 0 {
		pending := make(map[string]bool)
		for _, surface := range w.applications.Surfaces() {
			// A loading surface still reserves its key even before an image is
			// ready. Duplicate keys and invalid identities cannot consume slots.
			if surface.ID == 0 || surface.Key == key || !validApplicationKey(surface.Key) || pending[surface.Key] || w.m.applicationState.index(surface.Key) >= 0 {
				continue
			}
			pending[surface.Key] = true
			available--
		}
	}
	if available > 0 {
		return nil
	}
	return fmt.Errorf("saved layout has reached its 32-window limit; forget closed placements before opening another window")
}

func (w *Workspace) ActivateApplication(key string) error {
	w.syncApplications()
	live := false
	for _, surface := range w.applicationSurfaces {
		live = live || surface.Key == key
	}
	if !live {
		return fmt.Errorf("opened application is not available in the workspace")
	}
	if i := w.m.applicationState.index(key); i >= 0 && w.m.applicationState.Layouts[i].Space != w.m.applicationState.Space {
		if err := w.Dispatch(Action{Kind: SwitchSpace, Space: w.m.applicationState.Layouts[i].Space}); err != nil {
			return err
		}
	}
	if err := w.Dispatch(Action{Kind: SelectApplication, ApplicationKey: key}); err != nil {
		return err
	}
	w.readAndFocusSelectedApplication()
	return nil
}

func (w *Workspace) syncApplications() {
	if w.applicationNodes == nil {
		w.applicationNodes = make(map[uint64]scene.NodeID)
		w.applicationFrames = make(map[uint64]scene.NodeID)
		w.applicationDragHandles = make(map[uint64]scene.NodeID)
		w.applicationWindowControls = make(map[uint64]scene.NodeID)
		w.applicationResizeHandles = make(map[uint64]scene.NodeID)
		w.applicationKeys = make(map[uint64]string)
	}
	live := make(map[uint64]bool)
	present := make(map[uint64]bool)
	keys := make(map[string]bool)
	w.applicationSurfaces = w.applicationSurfaces[:0]
	w.applicationLayoutFull = false
	if w.applications != nil {
		surfaces := w.applications.Surfaces()
		for _, surface := range surfaces {
			present[surface.ID] = true
		}
		for _, surface := range surfaces {
			if len(w.applicationSurfaces) >= MaxApplicationLayouts {
				w.applicationLayoutFull = true
				break
			}
			if surface.ID == 0 || surface.Texture == nil || surface.Texture.ID() == 0 {
				continue
			}
			key := surface.Key
			if key == "" {
				key = w.applicationKeys[surface.ID]
				if key == "" {
					for ordinal := 1; ordinal <= MaxApplicationLayouts; ordinal++ {
						candidate := fmt.Sprintf("local:%d", ordinal)
						used := false
						for _, assigned := range w.applicationKeys {
							if assigned == candidate {
								used = true
								break
							}
						}
						if !used {
							key = candidate
							break
						}
					}
				}
			}
			if !validApplicationKey(key) || keys[key] {
				continue
			}
			index := w.m.applicationState.index(key)
			if index < 0 {
				for i, p := range w.m.applicationState.Layouts {
					if p.Key == "" {
						index = i
						break
					}
				}
				if index < 0 {
					w.applicationLayoutFull = true
					continue
				}
				placement := ApplicationPlacement{Key: key, Space: w.m.applicationState.Space, X: 1.65, Y: -.2, Depth: 1.9, Wide: w.m.applicationWide}
				if index > 0 {
					placement.X = -2.5 + float32((index-1)%4)*5
					placement.Y = 1.2 - float32((index-1)/4)*3.5
					placement.Depth = -.4
				}
				if index == 0 && w.m.applicationBehind {
					placement.Depth = -1.6
				}
				if w.desktop {
					// A compact stagger keeps newly launched windows reachable.
					// Overview can retrieve them after further spatial placement.
					placement.X = float32(index%4)*1.35 - .7
					placement.Y = .75 - float32(index%4)*.55
					placement.Depth = float32(index%4) * .35
				}
				w.m.applicationState.Layouts[index] = placement
			}
			w.applicationKeys[surface.ID] = key
			keys[key], live[surface.ID] = true, true
			surface.Key = key
			w.applicationSurfaces = append(w.applicationSurfaces, surface)
			if node := w.applicationNodes[surface.ID]; node == 0 {
				node = w.scene.Add(0, scene.Node{Surface: surface.Texture})
				w.applicationNodes[surface.ID] = node
				w.attachApplicationFrame(surface.ID, node)
				w.resizeApplicationSurface(surface)
			} else {
				w.scene.Node(node).Surface = surface.Texture
			}
			n := w.scene.Node(w.applicationNodes[surface.ID])
			n.SurfaceUV, n.Translucent = surface.SurfaceUV, surface.Translucent
			w.syncSpatialApplication(surface)
		}
	}
	if w.applicationFocusedID != 0 && !live[w.applicationFocusedID] || w.applicationCapturedID != 0 && !live[w.applicationCapturedID] {
		w.clearApplicationFocus()
	}
	for motion := w.windowThrow; motion != nil; {
		next := motion.next
		for i, id := range motion.liveIDs {
			if id != 0 && (!live[id] || w.applicationKeys[id] != motion.origin[i].Key) {
				w.finishOneWindowThrow(motion)
				break
			}
		}
		motion = next
	}
	for id, node := range w.applicationNodes {
		if !live[id] {
			if w.pointer.surface == node {
				w.cancelPointer()
			}
			if w.applicationHoveredID == id {
				w.clearApplicationHover()
			}
			w.scene.Remove(node)
			delete(w.applicationNodes, id)
			delete(w.applicationFrames, id)
			delete(w.applicationDragHandles, id)
			delete(w.applicationWindowControls, id)
			delete(w.applicationResizeHandles, id)
			delete(w.applicationSpatial, id)
		}
	}
	// A live surface can temporarily lack an image. Keep its fallback key so
	// explicit closed-placement cleanup cannot mistake it for a withdrawn app.
	for id := range w.applicationKeys {
		if !present[id] {
			delete(w.applicationKeys, id)
		}
	}
	if w.applicationRestoreKey != "" && keys[w.applicationRestoreKey] {
		w.m.applicationState.Active, w.m.applicationState.Selected = w.applicationRestoreKey, w.applicationRestoreSelection
		w.applicationRestoreKey = ""
	}
	visible := w.visibleApplications()
	activeVisible := false
	for _, s := range visible {
		i := w.m.applicationState.index(s.Key)
		activeVisible = activeVisible || s.Key == w.m.applicationState.Active && i >= 0 && !w.m.applicationState.Layouts[i].Minimized
	}
	if len(visible) > 0 && !activeVisible {
		w.m.applicationState.Active = ""
		w.m.applicationState.Selected = 0
		for _, surface := range visible {
			i := w.m.applicationState.index(surface.Key)
			if i >= 0 && !w.m.applicationState.Layouts[i].Minimized {
				w.m.applicationState.Active = surface.Key
				w.m.applicationState.Selected = 1 << i
				break
			}
		}
	}
	w.selectCurrentApplication()
}

func (w *Workspace) selectCurrentApplication() {
	w.application = experience.ApplicationSurface{}
	w.applicationNode = 0
	for _, surface := range w.applicationSurfaces {
		i := w.m.applicationState.index(surface.Key)
		if surface.Key == w.m.applicationState.Active && w.inCurrentSpace(surface) && i >= 0 && !w.m.applicationState.Layouts[i].Minimized {
			w.application = surface
			w.applicationNode = w.applicationNodes[surface.ID]
			break
		}
	}
	w.m.applicationState.aliases()
	w.m.applicationBehind, w.m.applicationReading, w.m.applicationWide = w.m.applicationState.Behind, w.m.applicationState.Reading, w.m.applicationState.Wide
}

func (w *Workspace) installApplicationView(previous ApplicationViewState) {
	v := w.m.applicationState
	if previous.Active != v.Active || previous.Overview != v.Overview || previous.Placing != v.Placing || previous.Space != v.Space {
		w.clearApplicationFocus()
	}
	w.selectCurrentApplication()
	for _, surface := range w.applicationSurfaces {
		i, j := v.index(surface.Key), previous.index(surface.Key)
		if i >= 0 && (j < 0 || v.Layouts[i].Wide != previous.Layouts[j].Wide || v.Layouts[i].Width != previous.Layouts[j].Width || v.Layouts[i].Height != previous.Layouts[j].Height) {
			w.resizeApplicationSurface(surface)
		}
	}
}

func applicationLogicalSize(placement ApplicationPlacement) (width, height int) {
	if placement.Width != 0 && placement.Height != 0 {
		return placement.Width, placement.Height
	}
	if placement.Wide {
		return 1440, 900
	}
	return 960, 600
}

func (w *Workspace) resizeApplicationSurface(surface experience.ApplicationSurface) {
	if w.applications == nil {
		return
	}
	i := w.m.applicationState.index(surface.Key)
	if i < 0 {
		return
	}
	width, height := applicationLogicalSize(w.m.applicationState.Layouts[i])
	w.applications.Resize(surface.ID, width, height)
}

func applicationBasis() (right, up, normal scene.Vec3) {
	yaw, pitch := float64(initialModel().yaw), float64(initialModel().pitch)
	right = scene.Vec3{X: float32(math.Sin(yaw)), Z: -float32(math.Cos(yaw))}
	normal = scene.Vec3{X: float32(math.Cos(pitch) * math.Cos(yaw)), Y: float32(math.Sin(pitch)), Z: float32(math.Cos(pitch) * math.Sin(yaw))}
	up = normal.Cross(right)
	return
}

func applicationPlane(center, right, up, normal scene.Vec3, width, height float32) scene.Mat4 {
	return (scene.Mat4{right.X, right.Y, right.Z, 0, up.X, up.Y, up.Z, 0, normal.X, normal.Y, normal.Z, 0, center.X, center.Y, center.Z, 1}).Mul(scene.Scale(width, height, 1))
}

func (w *Workspace) applicationTransformFor(surface experience.ApplicationSurface) (scene.Mat4, scene.Vec3, float32, float32) {
	right, up, normal := applicationBasis()
	placement := w.m.applicationState.Layouts[w.m.applicationState.index(surface.Key)]
	center := right.Mul(placement.X).Add(up.Mul(placement.Y)).Add(normal.Mul(placement.Depth))
	width := float32(4.6)
	var height float32
	if placement.Width != 0 && placement.Height != 0 {
		// Compact and Wide are provider-resolution presets, not different
		// physical sizes. Preserve each preset's world-units-per-logical-pixel
		// when direct resizing turns it into a custom size. Maximized windows
		// deliberately use the Compact basis so they fill more of the scene.
		basis := float32(960)
		if placement.Wide && !placement.Maximized {
			basis = 1440
		}
		width = 4.6 * float32(placement.Width) / basis
		height = 4.6 * float32(placement.Height) / basis
	} else {
		tw, th := surface.Texture.Size()
		height = width * float32(th) / float32(tw)
		if surface.ContentAspect > 0 && !math.IsInf(float64(surface.ContentAspect), 0) {
			height = width / surface.ContentAspect
		}
	}
	if (surface.Frameless || surface.DragContent) && height > 3 {
		width *= 3 / height
		height = 3
	}
	return applicationPlane(center, right, up, normal, width, height), center, width, height
}

func (w *Workspace) frameApplicationCamera(center, normal, up scene.Vec3, width, height float32) {
	tangent := float32(math.Tan(float64(w.camera.FOV) / 2))
	aspect := w.viewport.Width / w.viewport.Height
	distance := float32(math.Max(float64(height), float64(width/aspect))) / (2 * tangent) * 1.08
	w.camera.Eye = center.Add(normal.Mul(distance))
	w.camera.Target = center
	w.camera.Up = up
}

func (w *Workspace) syncApplicationScene() {
	attached := len(w.applicationSurfaces) > 0
	view := w.m.applicationState
	if panel := w.scene.Node(w.panelNode); panel != nil {
		panel.Hidden = attached
	}
	for _, id := range w.nodes {
		if node := w.scene.Node(id); node != nil {
			node.Hidden = attached && (view.Reading || view.Overview)
		}
	}
	if !attached {
		return
	}
	right, up, normal := applicationBasis()
	columns, rows := applicationOverviewGrid(len(w.visibleApplications()), w.viewport)
	i := 0
	for _, surface := range w.applicationSurfaces {
		node := w.scene.Node(w.applicationNodes[surface.ID])
		if !w.inCurrentSpace(surface) {
			node.Hidden = true
			continue
		}
		transform, center, width, height := w.applicationTransformFor(surface)
		placementIndex := view.index(surface.Key)
		minimized := placementIndex >= 0 && view.Layouts[placementIndex].Minimized
		node.Hidden = view.Reading && !view.Overview && surface.Key != view.Active
		node.Surface = surface.Texture
		if minimized && !view.Overview {
			node.Surface = nil
		}
		if view.Overview {
			width = 4.3
			tw, th := surface.Texture.Size()
			height = width * float32(th) / float32(tw)
			if surface.ContentAspect > 0 && !math.IsInf(float64(surface.ContentAspect), 0) {
				height = width / surface.ContentAspect
			}
			if height > 2.7 {
				width *= 2.7 / height
				height = 2.7
			}
			center = right.Mul((float32(i%columns) - float32(columns-1)/2) * 4.9).Add(up.Mul((float32(rows-1)/2 - float32(i/columns)) * 3.4))
			transform = applicationPlane(center, right, up, normal, width, height)
		}
		node.Transform = transform
		w.placeSpatialApplication(surface, width, height)
		if mount := w.applicationSpatial[surface.ID]; mount != nil {
			w.scene.Node(mount.root).Hidden = minimized && !view.Overview
		}
		w.syncApplicationFrame(surface)
		if view.Reading && !view.Overview && surface.Key == view.Active {
			if cinematicFrameSurface(surface) && !surface.Frameless {
				// Include the outer cinematic rails in the readable framing.
				width, height = width*1.09, height*1.09
			}
			w.frameApplicationCamera(center, normal, up, width, height)
		}
		i++
	}
	if view.Overview {
		w.frameApplicationCamera(scene.Vec3{}, normal, up, float32(columns)*4.9, float32(rows)*3.4)
	}
}

func (w *Workspace) clearApplicationHover() {
	if w.applications != nil && w.applicationHoveredID != 0 {
		w.applications.Send(w.applicationHoveredID, experience.Event{Kind: experience.PointerCancel})
	}
	w.applicationHoveredID = 0
	w.applicationHover = false
}

func (w *Workspace) clearApplicationFocus() {
	if w.applications != nil {
		if w.applicationFocusedID != 0 {
			w.applications.Send(w.applicationFocusedID, experience.Event{Kind: experience.KeyboardCancel})
			w.applications.Focus(0)
		}
		if w.applicationCapturedID != 0 && w.applicationCapturedID != w.applicationHoveredID {
			w.applications.Send(w.applicationCapturedID, experience.Event{Kind: experience.PointerCancel})
		}
	}
	w.clearApplicationHover()
	w.applicationFocusedID, w.applicationCapturedID = 0, 0
	w.applicationKeyboard = false
	w.applicationButtons = nil
	w.applicationCaptureObject = 0
}

func (w *Workspace) applicationHit(x, y float32) (scene.Hit, bool) {
	w.layout(w.width, w.height)
	w.syncScene()
	hit, ok := w.scene.Pick(w.camera, w.viewport, x, y)
	if ok && w.applicationForNode(hit.Node).ID != 0 {
		return hit, true
	}
	return hit, false
}

func (w *Workspace) applicationForNode(node scene.NodeID) experience.ApplicationSurface {
	for _, surface := range w.applicationSurfaces {
		if w.applicationNodes[surface.ID] == node {
			return surface
		}
		if mount := w.applicationSpatial[surface.ID]; mount != nil && mount.objects[node] != 0 {
			return surface
		}
	}
	return experience.ApplicationSurface{}
}

func (w *Workspace) mapApplication(event experience.Event, captured bool) (experience.Event, experience.ApplicationSurface, bool) {
	var hit scene.Hit
	var ok bool
	var surface experience.ApplicationSurface
	if captured {
		w.layout(w.width, w.height)
		w.syncScene()
		node := w.applicationNodes[w.applicationCapturedID]
		surface = w.applicationForNode(node)
		hit, _, ok = w.scene.MapCapturedSurface(w.camera, w.viewport, event.X, event.Y, node)
	} else {
		hit, ok = w.applicationHit(event.X, event.Y)
		if ok {
			surface = w.applicationForNode(hit.Node)
		}
	}
	if ok {
		if mount := w.applicationSpatial[surface.ID]; mount != nil {
			if id := mount.objects[hit.Node]; id != 0 {
				event.SpatialObject, event.SpatialTriangle = id, hit.Triangle
				if inverse, valid := w.spatialApplicationTransform(surface).Inverse(); valid {
					p := inverse.TransformPoint(hit.Point)
					event.SpatialPoint = [3]float32{p.X, p.Y, p.Z}
				}
				// Pointer gestures use the backing plane's consistent pixel space.
				if plane, _, valid := w.scene.MapCapturedSurface(w.camera, w.viewport, event.X, event.Y, w.applicationNodes[surface.ID]); valid {
					hit.PixelX, hit.PixelY = plane.PixelX, plane.PixelY
				}
			}
		}
		w.applicationX, w.applicationY = hit.PixelX, hit.PixelY
	}
	if captured {
		event.SpatialObject = w.applicationCaptureObject
	}
	event.X, event.Y = w.applicationX, w.applicationY
	return event, surface, ok
}

func applicationButton(event experience.Event) uint32 {
	if event.ButtonCode != 0 {
		return event.ButtonCode
	}
	switch event.Button {
	case experience.ButtonPrimary:
		return 272
	case experience.ButtonSecondary:
		return 273
	case experience.ButtonMiddle:
		return 274
	}
	return 0
}

func (w *Workspace) handleApplication(event experience.Event) bool {
	if w.application.ID == 0 {
		return false
	}
	view := w.m.applicationState
	if w.pointer.kind == captureApplicationPlacement {
		switch event.Kind {
		case experience.PointerMove:
			w.moveApplicationPlacement(event.X, event.Y)
			return true
		case experience.PointerUp:
			if applicationButton(event) == 272 {
				w.moveApplicationPlacement(event.X, event.Y)
				w.commitPointer()
			}
			return true
		case experience.PointerCancel, experience.KeyboardCancel:
			w.clearApplicationFocus()
			return w.cancelPointer()
		}
	}
	switch event.Kind {
	case experience.KeyboardCancel:
		owned := w.applicationKeyboard
		w.clearApplicationFocus()
		return owned
	case experience.KeyInput, experience.KeyboardModifiers, experience.KeyboardRepeatInfo, experience.KeymapChanged, experience.TextCommit, experience.TextPreedit:
		if !w.applicationKeyboard || view.Overview || view.Placing {
			return false
		}
		w.applications.Send(w.applicationFocusedID, event)
		return true
	case experience.PointerCancel:
		captured := len(w.applicationButtons) > 0 || w.applicationHover
		w.clearApplicationHover()
		w.applicationButtons = nil
		w.applicationCapturedID = 0
		w.applicationCaptureObject = 0
		return captured
	case experience.PointerDown:
		button := applicationButton(event)
		if button == 0 {
			return false
		}
		captured := len(w.applicationButtons) > 0
		mapped, surface, hit := w.mapApplication(event, captured)
		if !captured && !hit {
			w.clearApplicationFocus()
			x, y := (event.X-w.ox)/w.scale, (event.Y-w.oy)/w.scale
			return (view.Reading || view.Overview) && w.inViewport(x, y)
		}
		w.stopWindowThrowForKey(surface.Key)
		if view.Overview || view.Placing {
			w.clearApplicationFocus()
			if button != 272 {
				return true
			}
			add := event.Modifiers.Has(experience.ModShift)
			if view.Placing && !add {
				w.applicationRestoreKey = ""
				w.startWindowDrag(surface, event)
				return true
			}
			index := view.index(surface.Key)
			if add || view.Selected&(1<<index) == 0 || view.Active != surface.Key {
				if err := w.Dispatch(Action{Kind: SelectApplication, ApplicationKey: surface.Key, Additive: add}); err != nil {
					return true
				}
			}
			if view.Overview {
				if !add {
					_ = w.Dispatch(Action{Kind: ToggleApplicationOverview})
				}
				return true
			}
			return true
		}
		w.cancelPointer()
		w.applicationRestoreKey = ""
		if !captured && surface.Key != view.Active {
			// Installing a selection normally cancels both input owners. This
			// click already has a valid pointer route to the chosen surface;
			// preserve that route while clearing the previous keyboard owner.
			preserveHover := w.applicationHoveredID == surface.ID
			if preserveHover {
				w.applicationHoveredID = 0
			}
			_ = w.Dispatch(Action{Kind: SelectApplication, ApplicationKey: surface.Key})
			if preserveHover {
				w.applicationHoveredID, w.applicationHover = surface.ID, true
			}
		}
		if w.applicationFocusedID != surface.ID {
			// Keyboard ownership can change while pointer focus already belongs
			// to this surface. Preserve its enter serial and cursor through the
			// click; only a different hover target needs pointer cancellation.
			if w.applicationFocusedID != 0 {
				w.applications.Send(w.applicationFocusedID, experience.Event{Kind: experience.KeyboardCancel})
			}
			w.applications.Focus(surface.ID)
			w.applicationFocusedID = surface.ID
			w.applicationKeyboard = true
		}
		if w.applicationHoveredID != surface.ID {
			w.clearApplicationHover()
		}
		w.applicationHoveredID = surface.ID
		w.applicationHover = true
		w.applicationCapturedID = surface.ID
		if !captured {
			w.applicationCaptureObject = mapped.SpatialObject
		}
		if w.applicationButtons == nil {
			w.applicationButtons = make(map[uint32]bool)
		}
		w.applicationButtons[button] = true
		mapped.ButtonCode = button
		w.applications.Send(surface.ID, mapped)
		return true
	case experience.PointerMove, experience.PointerScroll:
		if w.pointer.kind != captureNone {
			return false
		}
		if view.Overview || view.Placing {
			if event.Kind == experience.PointerScroll && view.Placing {
				if _, ok := w.applicationHit(event.X, event.Y); ok {
					_ = w.Dispatch(Action{Kind: MoveApplications, DeltaDepth: -event.ScrollY * .035})
					return true
				}
			}
			return false
		}
		captured := len(w.applicationButtons) > 0
		mapped, surface, hit := w.mapApplication(event, captured)
		if captured || hit {
			if surface.ID == 0 {
				return true
			}
			if w.applicationHoveredID != surface.ID {
				w.clearApplicationHover()
			}
			w.applicationHoveredID = surface.ID
			w.applicationHover = true
			w.applications.Send(surface.ID, mapped)
			return true
		}
		w.clearApplicationHover()
	case experience.PointerUp:
		if len(w.applicationButtons) == 0 {
			return false
		}
		button := applicationButton(event)
		if !w.applicationButtons[button] {
			return true
		}
		mapped, surface, _ := w.mapApplication(event, true)
		mapped.ButtonCode = button
		w.applications.Send(surface.ID, mapped)
		delete(w.applicationButtons, button)
		if len(w.applicationButtons) == 0 {
			w.applicationCapturedID = 0
			w.applicationCaptureObject = 0
			// Releasing the implicit grab can leave the pointer over another
			// surface or background. Update its route now, even without motion,
			// so a client's hidden/resize cursor cannot outlive its pointer focus.
			next, target, hit := w.mapApplication(event, false)
			if !hit || target.ID != w.applicationHoveredID {
				w.clearApplicationHover()
				if hit {
					w.applicationHoveredID, w.applicationHover = target.ID, true
					next.Kind = experience.PointerMove
					next.Button, next.ButtonCode, next.Pressed = experience.ButtonNone, 0, false
					w.applications.Send(target.ID, next)
				}
			}
		}
		return true
	}
	return false
}

func (w *Workspace) beginApplicationPlacement(surface experience.ApplicationSurface, x, y float32) {
	w.layout(w.width, w.height)
	w.syncScene()
	node := w.applicationNodes[surface.ID]
	if hit, _, ok := w.scene.MapCapturedSurface(w.camera, w.viewport, x, y, node); ok {
		w.pointer = pointerCapture{kind: captureApplicationPlacement, start: w.Document(), surface: node, lastWorld: hit.Point}
	}
}

func (w *Workspace) moveApplicationPlacement(x, y float32) {
	w.layout(w.width, w.height)
	w.syncScene()
	if hit, _, ok := w.scene.MapCapturedSurface(w.camera, w.viewport, x, y, w.pointer.surface); ok {
		right, up, _ := applicationBasis()
		delta := hit.Point.Sub(w.pointer.lastWorld)
		w.preview(Action{Kind: MoveApplications, DeltaX: delta.Dot(right), DeltaY: delta.Dot(up)})
		w.pointer.lastWorld = hit.Point
	}
}

func shortApplicationTitle(title string) string {
	text := []rune(title)
	if len(text) == 0 {
		return "APPLICATION"
	}
	if len(text) > 24 {
		text = append(text[:23], '…')
	}
	return string(text)
}

func (w *Workspace) drawApplicationControls() {
	v := w.m.applicationState
	w.text(43, 132, 12, fmt.Sprintf("WORKSPACE / %02d APPS", len(w.visibleApplications())), muted, 1)
	w.text(43, 165, 15, shortApplicationTitle(w.application.Title), ink, 1)
	w.text(43, 188, 11, fmt.Sprintf("%d selected / %d move together", bits.OnesCount32(v.Selected), bits.OnesCount32(v.movementSelection())), teal, 1)
	w.button(applicationOverviewButton, "OVERVIEW", v.Overview)
	place := "PLACE / GROUP"
	if v.Placing {
		place = "DONE PLACING"
	}
	w.button(applicationPlaceButton, place, v.Placing)
	read := "READ SELECTED"
	if v.Reading && !v.Overview {
		read = "RETURN TO SPACE"
	}
	w.button(applicationReadButton, read, v.Reading && !v.Overview)
	depth := "SEND SELECTION BACK"
	if v.Behind {
		depth = "BRING FORWARD"
	}
	w.button(applicationDepthButton, depth, !v.Behind)
	w.button(applicationBackButton, "DEPTH −", false)
	w.button(applicationFrontButton, "DEPTH +", false)
	size := "SIZE: COMPACT"
	if i := v.index(v.Active); i >= 0 && v.Layouts[i].Width != 0 {
		size = fmt.Sprintf("SIZE: %d × %d", v.Layouts[i].Width, v.Layouts[i].Height)
	} else if v.Wide {
		size = "SIZE: WIDE"
	}
	w.button(applicationSizeButton, size, v.Wide)
	w.button(applicationGroupButton, "GROUP SELECTED", bits.OnesCount32(v.Selected) > 1)
	w.button(applicationUngroupButton, "UNGROUP", false)
	if v.Overview {
		w.text(43, 594, 12, "Arrows select; Enter returns.", teal, 1)
		w.text(43, 617, 12, "Shift+click selects several.", muted, 1)
		if w.application.DragContent {
			w.text(43, 640, 12, "Drag the photo into Space.", muted, 1)
		} else {
			w.text(43, 640, 12, "Esc returns to your view.", muted, 1)
		}
	} else if v.Placing {
		w.text(43, 594, 12, "Drag an app to place it.", teal, 1)
		w.text(43, 617, 12, "Shift+click selects several.", muted, 1)
		w.text(43, 640, 12, "Scroll changes depth.", muted, 1)
		if w.application.DragContent {
			w.text(43, 662, 12, "Enter to view the photo.", muted, 1)
		} else {
			w.text(43, 662, 12, "Enter to read and type.", muted, 1)
		}
	} else {
		if w.application.DragContent {
			w.text(43, 594, 12, "Enter to view the photo.", teal, 1)
		} else if w.applicationKeyboard {
			w.text(43, 594, 12, "Keyboard sent to application.", teal, 1)
		} else {
			w.text(43, 594, 12, "Enter to read and type.", teal, 1)
		}
		if w.application.DragContent {
			w.text(43, 617, 12, "Drag the photo itself to move.", muted, 1)
		} else {
			w.text(43, 617, 12, "Drag the grip to move a window.", muted, 1)
		}
		w.text(43, 640, 12, "Ctrl+Alt+O opens overview.", muted, 1)
	}
	w.text(43, 685, 12, "Grouped apps move together.", muted, 1)
	w.text(43, 708, 12, "Ctrl+Z undoes placement.", muted, 1)
	if w.applicationKeyboard {
		w.text(43, 662, 12, "Keyboard → application", teal, 1)
	}
	if _, ok := w.applications.(experience.ApplicationCloser); ok {
		w.button(applicationCloseButton, "CLOSE SELECTED", false)
	}
}

func (w *Workspace) drawApplicationOverviewLabels() {
	if !w.m.applicationState.Overview {
		return
	}
	for _, surface := range w.applicationSurfaces {
		if !w.inCurrentSpace(surface) {
			continue
		}
		if surface.Frameless || surface.DragContent {
			continue
		}
		node := w.scene.Node(w.applicationNodes[surface.ID])
		point := node.Transform.TransformPoint(scene.Vec3{X: -.5, Y: -.5})
		x, y, _, visible := w.camera.Project(point, w.viewport)
		if !visible {
			continue
		}
		label := shortApplicationTitle(surface.Title)
		i := w.m.applicationState.index(surface.Key)
		if i >= 0 && w.m.applicationState.Layouts[i].Minimized {
			label = "[MIN] " + label
		}
		color := muted
		if w.m.applicationState.Selected&(1<<i) != 0 {
			label = "[+] " + label
			color = teal
		}
		if group := w.m.applicationState.Layouts[i].Group; group != 0 {
			label = fmt.Sprintf("G%d %s", group, label)
		}
		w.canvas.Text(x, y+4*w.scale, 11*w.scale, label, w.color(color, 1))
	}
}
