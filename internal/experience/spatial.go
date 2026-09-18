package experience

import (
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/scene"
)

// SpatialContent is borrowed on the host goroutine, like ApplicationSurface.
// Object identities belong to one application. Parents precede their children;
// zero means the app origin. Coordinates use app width as one unit, +Y up and
// +Z toward the viewer. The opaque Texture is the controls/backing plane at Z=0.
// Meshes and images are retained immutable resources; transforms may change on
// each Poll/Send. Providers retire resources only after removing all references.
type SpatialContent struct {
	Objects []SpatialObject
	Labels  []SpatialLabel
}

type SpatialObject struct {
	ID, Parent uint64
	Node       scene.Node
}

// Labels are screen-readable annotations attached to app-local anchors. They
// do not take input; controls belong to the content plane or pickable objects.
type SpatialLabel struct {
	Text     string
	Position scene.Vec3
	Color    scene.Color
}

func (s *SpatialContent) Validate() error {
	if s == nil {
		return nil
	}
	if len(s.Objects) > 4096 || len(s.Labels) > 256 {
		return fmt.Errorf("native scene exceeds object or label budget")
	}
	seen := make(map[uint64]bool, len(s.Objects))
	for _, o := range s.Objects {
		if o.ID == 0 || seen[o.ID] || o.Parent != 0 && !seen[o.Parent] {
			return fmt.Errorf("native scene has an invalid identity or parent")
		}
		if o.Node.Mesh != nil && o.Node.Surface != nil {
			return fmt.Errorf("native object cannot contain both mesh and image")
		}
		for _, v := range o.Node.Transform {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return fmt.Errorf("native object transform must be finite")
			}
		}
		n := o.Node
		values := []float32{n.Color.R, n.Color.G, n.Color.B, n.Color.A, n.WireColor.R, n.WireColor.G, n.WireColor.B, n.WireColor.A, n.WireWidth, n.Material.Specular, n.Material.Roughness, n.Material.Metallic, n.Material.RimStrength, n.Material.Transmission, n.Material.Refraction, n.Material.RefractionBlur, n.Material.Hologram}
		values = append(values, n.Material.RimColor[:]...)
		values = append(values, n.Glow[:]...)
		values = append(values, n.SurfaceUV[:]...)
		for _, v := range values {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return fmt.Errorf("native object appearance must be finite")
			}
		}
		if n.WireWidth < 0 {
			return fmt.Errorf("native wire width cannot be negative")
		}
		seen[o.ID] = true
	}
	for _, l := range s.Labels {
		if len(l.Text) > 512 || !utf8.ValidString(l.Text) {
			return fmt.Errorf("native label exceeds text budget")
		}
		for _, v := range []float32{l.Position.X, l.Position.Y, l.Position.Z, l.Color.R, l.Color.G, l.Color.B, l.Color.A} {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return fmt.Errorf("native label anchor must be finite")
			}
		}
	}
	return nil
}
