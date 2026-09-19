package workspace

import (
	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
)

type spatialMount struct {
	root    scene.NodeID
	nodes   map[uint64]scene.NodeID
	objects map[scene.NodeID]uint64
	parents map[uint64]uint64
}

func (w *Workspace) syncSpatialApplication(surface experience.ApplicationSurface) {
	if w.applicationSpatial == nil {
		w.applicationSpatial = make(map[uint64]*spatialMount)
	}
	m := w.applicationSpatial[surface.ID]
	if surface.Spatial == nil || surface.Spatial.Validate() != nil {
		if m != nil {
			w.scene.Remove(m.root)
			delete(w.applicationSpatial, surface.ID)
		}
		return
	}
	// Rebuild only hierarchy changes. Reparenting incrementally can encounter
	// a temporary cycle when a valid new hierarchy reverses an old parentage.
	if m != nil {
		changed := len(m.nodes) != len(surface.Spatial.Objects)
		for _, object := range surface.Spatial.Objects {
			changed = changed || m.nodes[object.ID] == 0 || m.parents[object.ID] != object.Parent
		}
		if changed {
			w.scene.Remove(m.root)
			m = nil
		}
	}
	if m == nil {
		m = &spatialMount{root: w.scene.Add(w.applicationNodes[surface.ID], scene.Node{}), nodes: map[uint64]scene.NodeID{}, objects: map[scene.NodeID]uint64{}, parents: map[uint64]uint64{}}
		w.applicationSpatial[surface.ID] = m
	}
	live := make(map[uint64]bool, len(surface.Spatial.Objects))
	for _, object := range surface.Spatial.Objects {
		live[object.ID] = true
		parent := m.root
		if object.Parent != 0 {
			parent = m.nodes[object.Parent]
		}
		id := m.nodes[object.ID]
		if id == 0 || w.scene.Node(id) == nil {
			id = w.scene.Add(parent, object.Node)
			m.nodes[object.ID] = id
			m.objects[id] = object.ID
		} else {
			if m.parents[object.ID] != object.Parent {
				_ = w.scene.Reparent(id, parent)
			}
			n := w.scene.Node(id)
			// Copy public authoring fields; preserve scene-owned hierarchy.
			n.Transform, n.Mesh, n.Surface = object.Node.Transform, object.Node.Mesh, object.Node.Surface
			n.Color, n.WireColor, n.WireWidth = object.Node.Color, object.Node.WireColor, object.Node.WireWidth
			n.Material, n.Glow = object.Node.Material, object.Node.Glow
			n.Hidden, n.Unlit, n.Unpickable, n.DepthReadOnly = object.Node.Hidden, object.Node.Unlit, object.Node.Unpickable, object.Node.DepthReadOnly
			n.Translucent = object.Node.Translucent
			n.SurfaceUV = object.Node.SurfaceUV
			n.CastShadow, n.ReceiveShadow = object.Node.CastShadow, object.Node.ReceiveShadow
			if n.Transform == (scene.Mat4{}) {
				n.Transform = scene.Identity()
			}
			if n.Color == (scene.Color{}) {
				n.Color = scene.Color{R: 1, G: 1, B: 1, A: 1}
			}
		}
		m.parents[object.ID] = object.Parent
	}
	for object, id := range m.nodes {
		if !live[object] {
			w.scene.Remove(id)
			delete(m.nodes, object)
			delete(m.objects, id)
			delete(m.parents, object)
		}
	}
}

func (w *Workspace) placeSpatialApplication(surface experience.ApplicationSurface, width, height float32) {
	if m := w.applicationSpatial[surface.ID]; m != nil {
		w.scene.Node(m.root).Transform = scene.Scale(1, width/height, width)
	}
}

func (w *Workspace) spatialApplicationTransform(surface experience.ApplicationSurface) scene.Mat4 {
	n := w.scene.Node(w.applicationNodes[surface.ID])
	m := w.applicationSpatial[surface.ID]
	if n == nil || m == nil {
		return scene.Identity()
	}
	return n.Transform.Mul(w.scene.Node(m.root).Transform)
}

func (w *Workspace) drawSpatialApplicationLabels() {
	for _, surface := range w.applicationSurfaces {
		m := w.applicationSpatial[surface.ID]
		if m == nil || surface.Spatial == nil || w.scene.Node(w.applicationNodes[surface.ID]).Hidden || w.scene.Node(m.root).Hidden {
			continue
		}
		transform := w.spatialApplicationTransform(surface)
		for _, label := range surface.Spatial.Labels {
			point := transform.TransformPoint(label.Position)
			x, y, z, visible := w.camera.Project(point, w.viewport)
			if !visible {
				continue
			}
			// Do not paint a label through nearer windows or geometry.
			if hit, ok := w.scene.Pick(w.camera, w.viewport, x, y); ok && hit.Depth+0.0001 < z {
				continue
			}
			color := label.Color
			if color == (scene.Color{}) {
				color = scene.ColorHex(0xc4efff, 1)
			}
			width := min(420*w.scale, w.viewport.Width)
			x = max(w.viewport.X, min(x, w.viewport.X+w.viewport.Width-width))
			w.shapedText(x, y, 11*w.scale, width, label.Text, color)
		}
	}
}
