package scene

import (
	"fmt"

	"github.com/codemodify/worldr/internal/render"
)

type NodeID uint64

type Node struct {
	Transform Mat4
	Mesh      *Mesh
	// Surface is a two-sided content plane in local XY, opaque by default, spanning
	// [-0.5, 0.5] on each axis. Its top-left maps to texture pixel (0, 0).
	// A node may contain either a Mesh or a Surface, never both.
	Surface *render.Texture
	// SurfaceUV is normalized x,y,width,height; zero selects the full image.
	SurfaceUV [4]float32
	Color     Color
	WireColor Color
	WireWidth float32
	Material  render.Material
	// Glow is a mesh-only background halo; like Material, it is not inherited.
	Glow                      [3]float32
	Hidden, Unlit, Unpickable bool
	// DepthReadOnly meshes test the opaque depth buffer without writing it.
	// They draw after ordinary meshes and surfaces, in caller traversal order.
	// Callers remain responsible for mutual back-to-front blending order.
	DepthReadOnly bool
	// Translucent uses per-pixel depth peeling; see render.Draw.Translucent.
	Translucent               bool
	CastShadow, ReceiveShadow bool
	parent                    NodeID
	children                  []NodeID
}

// Scene owns a tree of independently transformed objects. NodeID 0 denotes the
// implicit root. A Scene and its Canvas are intended for one render goroutine.
type Scene struct {
	// Light is the world-space direction toward the light; zero keeps the
	// original workspace light. Shadow is an explicit bounded light camera.
	Light Vec3
	// EffectPhase is forwarded to explicitly animated native materials. Keep it
	// fixed when presentation motion is reduced; it never affects picking.
	EffectPhase        float32
	PointLights        []render.PointLight
	Shadow             render.Shadow
	TransparencyLayers int
	nodes              map[NodeID]*Node
	roots              []NodeID
	nextID             NodeID
	readOnly           []render.Draw
}

func NewScene() *Scene { return &Scene{nodes: make(map[NodeID]*Node)} }

// Add attaches a node to a parent. Invalid parents are programming errors. A
// zero transform/color is initialized to identity/opaque white respectively.
func (s *Scene) Add(parent NodeID, node Node) NodeID {
	if parent != 0 && s.nodes[parent] == nil {
		panic("scene: unknown parent")
	}
	node.validateContent()
	if s.nodes == nil {
		s.nodes = make(map[NodeID]*Node)
	}
	if node.Transform == (Mat4{}) {
		node.Transform = Identity()
	}
	if node.Color == (Color{}) {
		node.Color = Color{1, 1, 1, 1}
	}
	s.nextID++
	id := s.nextID
	node.parent, node.children = parent, nil
	s.nodes[id] = &node
	if parent == 0 {
		s.roots = append(s.roots, id)
	} else {
		s.nodes[parent].children = append(s.nodes[parent].children, id)
	}
	return id
}

func (node *Node) validateContent() {
	if node.Mesh != nil && node.Surface != nil {
		panic("scene: a node cannot contain both a mesh and a surface")
	}
}
func (s *Scene) Node(id NodeID) *Node { return s.nodes[id] }
func (s *Scene) Children(id NodeID) []NodeID {
	if id == 0 {
		return append([]NodeID(nil), s.roots...)
	}
	if node := s.nodes[id]; node != nil {
		return append([]NodeID(nil), node.children...)
	}
	return nil
}

// Reparent preserves a node's local transform and rejects cycles.
func (s *Scene) Reparent(id, parent NodeID) error {
	node := s.nodes[id]
	if node == nil || (parent != 0 && s.nodes[parent] == nil) {
		return fmt.Errorf("scene: unknown node or parent")
	}
	for ancestor := parent; ancestor != 0; ancestor = s.nodes[ancestor].parent {
		if ancestor == id {
			return fmt.Errorf("scene: parenting would create a cycle")
		}
	}
	s.detach(id, node.parent)
	node.parent = parent
	if parent == 0 {
		s.roots = append(s.roots, id)
	} else {
		s.nodes[parent].children = append(s.nodes[parent].children, id)
	}
	return nil
}
func (s *Scene) Remove(id NodeID) {
	node := s.nodes[id]
	if node == nil {
		return
	}
	s.detach(id, node.parent)
	var remove func(NodeID)
	remove = func(child NodeID) {
		for _, grandchild := range s.nodes[child].children {
			remove(grandchild)
		}
		delete(s.nodes, child)
	}
	remove(id)
}
func (s *Scene) detach(id, parent NodeID) {
	children := &s.roots
	if parent != 0 {
		children = &s.nodes[parent].children
	}
	for i, child := range *children {
		if child == id {
			*children = append((*children)[:i], (*children)[i+1:]...)
			return
		}
	}
}

// walk visits hierarchy state only. Geometry is never projected, lit, clipped,
// or expanded on the CPU during frame preparation.
func (s *Scene) walk(visit func(NodeID, *Node, Mat4, Color, bool)) { s.walkColor(false, visit) }

func (s *Scene) walkColor(linear bool, visit func(NodeID, *Node, Mat4, Color, bool)) {
	var descend func(NodeID, Mat4, Color, bool)
	descend = func(id NodeID, parent Mat4, parentColor Color, parentUnpickable bool) {
		node := s.nodes[id]
		if node == nil || node.Hidden {
			return
		}
		node.validateContent()
		world := parent.Mul(node.Transform)
		color := parentColor.Multiply(node.Color)
		if linear {
			color = parentColor.multiplyLinear(node.Color)
		}
		unpickable := parentUnpickable || node.Unpickable
		if color.A <= 0 {
			return
		}
		visit(id, node, world, color, unpickable)
		for _, child := range node.children {
			descend(child, world, color, unpickable)
		}
	}
	for _, root := range s.roots {
		descend(root, Identity(), Color{1, 1, 1, 1}, false)
	}
}

// Draw records mesh and surface instances in one camera pass so they share a
// depth buffer. A backend uploads retained geometry and changed texture pixels;
// transformation, clipping and shading run on the GPU. CPU frame preparation
// scales with scene nodes rather than triangle count. DepthReadOnly meshes are
// submitted last in this pass, preserving their relative traversal order;
// callers order those meshes for blending with one another.
func (s *Scene) Draw(c *Canvas, camera Camera, vp Viewport) {
	if vp.Width <= 0 || vp.Height <= 0 {
		return
	}
	start := len(c.draws)
	s.readOnly = s.readOnly[:0]
	s.walkColor(c.linearColor, func(_ NodeID, node *Node, world Mat4, color Color, _ bool) {
		if node.Mesh == nil && node.Surface == nil {
			return
		}
		draw := render.Draw{
			Texture: node.Surface, UV: node.SurfaceUV, Model: [16]float32(world),
			Color:     [4]float32{color.R, color.G, color.B, color.A},
			WireColor: [4]float32{node.WireColor.R, node.WireColor.G, node.WireColor.B, node.WireColor.A},
			WireWidth: node.WireWidth, Unlit: node.Unlit, Material: node.Material, Glow: node.Glow, DepthReadOnly: node.DepthReadOnly, Translucent: node.Translucent, CastShadow: node.CastShadow, ReceiveShadow: node.ReceiveShadow,
		}
		if node.Mesh != nil {
			draw.Geometry = node.Mesh.geometry
		}
		if draw.DepthReadOnly {
			s.readOnly = append(s.readOnly, draw)
		} else {
			c.draws = append(c.draws, draw)
		}
	})
	c.draws = append(c.draws, s.readOnly...)
	clear(s.readOnly) // Keep scratch storage without retaining removed resources.
	s.readOnly = s.readOnly[:0]
	if len(c.draws) == start {
		return
	}
	c.flushUI()
	light := s.Light
	if light == (Vec3{}) {
		light = Vec3{-.35, .75, .8}
	}
	var pointLights [4]render.PointLight
	copy(pointLights[:], s.PointLights)
	c.commands = append(c.commands, render.Command{
		Kind:  render.SceneCommand,
		View:  render.View{Projection: [16]float32(camera.matrix(vp)), Eye: [3]float32{camera.Eye.X, camera.Eye.Y, camera.Eye.Z}, Light: [3]float32{light.X, light.Y, light.Z}, EffectPhase: s.EffectPhase, PointLights: pointLights, PointLightCount: len(s.PointLights), Shadow: s.Shadow, TransparencyLayers: s.TransparencyLayers, Viewport: [4]float32{vp.X, vp.Y, vp.Width, vp.Height}},
		Draws: c.draws[start:],
	})
}

type Hit struct {
	Node     NodeID
	Triangle int // -1 for content surfaces
	Depth    float32
	Point    Vec3
	// Surface distinguishes a content plane hit. PixelX/Y are continuous
	// texture coordinates measured right/down from its top-left; they are
	// independent of the node's scale or its distance from the camera.
	Surface        bool
	PixelX, PixelY float32
}

// Pick casts one ray through immutable local-space BVHs. Transforming the ray
// without renormalizing its local direction preserves world-space distances
// across differently scaled instances. Near/far clipping matches the GPU pass.
// Unpickable nodes intentionally allow input to pass through their geometry.
func (s *Scene) Pick(camera Camera, vp Viewport, x, y float32) (Hit, bool) {
	ray, ok := camera.Ray(vp, x, y)
	if !ok {
		return Hit{}, false
	}
	best := ray.MaxDistance
	var hit Hit
	found := false
	s.walk(func(id NodeID, node *Node, world Mat4, _ Color, unpickable bool) {
		if (node.Mesh == nil && node.Surface == nil) || unpickable {
			return
		}
		inverse, ok := world.Inverse()
		if !ok {
			return
		}
		localRay := Ray{Origin: inverse.TransformPoint(ray.Origin), Direction: inverse.TransformVector(ray.Direction), MaxDistance: ray.MaxDistance}
		face, distance, ok := -1, float32(0), false
		var pixelX, pixelY float32
		if node.Surface != nil {
			var inside bool
			distance, pixelX, pixelY, inside, ok = intersectSurface(localRay, node.Surface)
			ok = ok && inside && distance <= best
		} else {
			face, distance, ok = node.Mesh.raycast(localRay, best)
		}
		if !ok {
			return
		}
		point := ray.Origin.Add(ray.Direction.Mul(distance))
		_, _, depth, visible := camera.Project(point, vp)
		if !visible {
			return
		}
		hit, found, best = Hit{Node: id, Triangle: face, Depth: depth, Point: point, Surface: node.Surface != nil, PixelX: pixelX, PixelY: pixelY}, true, distance
	})
	return hit, found
}

// MapSurface maps a pointer to a particular content plane for an established
// pointer capture. It ignores intervening objects and accepts points outside
// the plane, reporting that separately through inside. Hidden or unpickable
// nodes, invalid transforms, pointers outside the viewport, and intersections
// outside the camera depth range return ok=false.
func (s *Scene) MapSurface(camera Camera, vp Viewport, x, y float32, nodeID NodeID) (hit Hit, inside, ok bool) {
	return s.mapSurface(camera, vp, x, y, nodeID, false)
}

// MapCapturedSurface extends mapping beyond the camera viewport for a pointer
// gesture that already belongs to this node. It must never establish a new
// target: use Pick for that. Near/far clipping and hierarchy visibility still
// apply, while inside reports whether the extended ray hits the content quad.
func (s *Scene) MapCapturedSurface(camera Camera, vp Viewport, x, y float32, nodeID NodeID) (hit Hit, inside, ok bool) {
	return s.mapSurface(camera, vp, x, y, nodeID, true)
}

func (s *Scene) mapSurface(camera Camera, vp Viewport, x, y float32, nodeID NodeID, captured bool) (hit Hit, inside, ok bool) {
	ray, valid := camera.pointerRay(vp, x, y, captured)
	if !valid {
		return
	}
	s.walk(func(id NodeID, node *Node, world Mat4, _ Color, unpickable bool) {
		if id != nodeID || node.Surface == nil || unpickable {
			return
		}
		inverse, valid := world.Inverse()
		if !valid {
			return
		}
		localRay := Ray{Origin: inverse.TransformPoint(ray.Origin), Direction: inverse.TransformVector(ray.Direction), MaxDistance: ray.MaxDistance}
		distance, pixelX, pixelY, within, valid := intersectSurface(localRay, node.Surface)
		if !valid {
			return
		}
		point := ray.Origin.Add(ray.Direction.Mul(distance))
		_, _, depth, visible := camera.Project(point, vp)
		if captured {
			projected := camera.matrix(vp).transform(point)
			visible = projected.w > 0 && projected.z >= 0 && projected.z <= projected.w
		}
		if !visible {
			return
		}
		hit = Hit{Node: id, Triangle: -1, Depth: depth, Point: point, Surface: true, PixelX: pixelX, PixelY: pixelY}
		inside, ok = within, true
	})
	return
}

// The ray direction is deliberately not normalized after transforming to local
// space: its parameter is the world distance used by mesh intersection tests.
func intersectSurface(ray Ray, texture *render.Texture) (distance, pixelX, pixelY float32, inside, ok bool) {
	if ray.Direction.Z == 0 {
		return
	}
	distance = -ray.Origin.Z / ray.Direction.Z
	if !finite(distance) || distance < 0 || distance > ray.MaxDistance {
		return 0, 0, 0, false, false
	}
	point := ray.Origin.Add(ray.Direction.Mul(distance))
	width, height := texture.Size()
	pixelX, pixelY = (point.X+0.5)*float32(width), (0.5-point.Y)*float32(height)
	if width <= 0 || height <= 0 || !finite(pixelX) || !finite(pixelY) {
		return 0, 0, 0, false, false
	}
	inside = pixelX >= 0 && pixelX < float32(width) && pixelY >= 0 && pixelY < float32(height)
	return distance, pixelX, pixelY, inside, true
}
