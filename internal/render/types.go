// Package render defines the vertex stream shared by the scene and GPU backend.
package render

import (
	"fmt"
	"math"
	"sync/atomic"
)

// Vertex uses top-left pixel coordinates and Vulkan depth (0 near, 1 far).
// RGB is straight (not premultiplied); A is multiplied by the atlas coverage.
// Vertices form a triangle list and must be submitted back to front for blending.
type Vertex struct {
	X, Y, Z    float32
	U, V       float32
	R, G, B, A float32
}

// Atlas stores tightly packed, single-channel coverage. Reserve the first texel
// as white for solid geometry, sampling it at (0.5/Width, 0.5/Height).
type Atlas struct {
	Width, Height int
	Pixels        []byte
}

// MeshVertex is object-space geometry. Barycentric coordinates allow the GPU
// to draw a consistent wire edge in the same fragment pass as the surface.
type MeshVertex struct {
	X, Y, Z    float32
	NX, NY, NZ float32
	R, G, B, A float32
	BX, BY, BZ float32
}

// Geometry owns immutable indexed mesh data. Create it once, reference it from
// draw instances, and explicitly release its backend allocation when unused.
type Geometry struct {
	id       uint64
	vertices []MeshVertex
	indices  []uint32
}

var geometrySequence atomic.Uint64

// NewGeometry validates and copies its inputs; later caller mutations cannot
// change the resource. An updated mesh is a new resource with a new identity.
func NewGeometry(vertices []MeshVertex, indices []uint32) (*Geometry, error) {
	if len(vertices) == 0 || len(indices) == 0 || len(indices)%3 != 0 || uint64(len(vertices)) > math.MaxUint32 || uint64(len(indices)) > math.MaxUint32 {
		return nil, fmt.Errorf("geometry needs vertices and complete indexed triangles")
	}
	for _, index := range indices {
		if uint64(index) >= uint64(len(vertices)) {
			return nil, fmt.Errorf("geometry index %d exceeds %d vertices", index, len(vertices))
		}
	}
	for _, vertex := range vertices {
		for _, value := range [...]float32{vertex.X, vertex.Y, vertex.Z, vertex.NX, vertex.NY, vertex.NZ, vertex.R, vertex.G, vertex.B, vertex.A, vertex.BX, vertex.BY, vertex.BZ} {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return nil, fmt.Errorf("geometry contains non-finite vertex")
			}
		}
	}
	return &Geometry{id: geometrySequence.Add(1), vertices: append([]MeshVertex(nil), vertices...), indices: append([]uint32(nil), indices...)}, nil
}

func (g *Geometry) ID() uint64 {
	if g == nil {
		return 0
	}
	return g.id
}
func (g *Geometry) VertexCount() int {
	if g == nil {
		return 0
	}
	return len(g.vertices)
}
func (g *Geometry) IndexCount() int {
	if g == nil {
		return 0
	}
	return len(g.indices)
}

// Vertices and Indices return copies, used only on a backend's first upload.
func (g *Geometry) Vertices() []MeshVertex {
	if g == nil {
		return nil
	}
	return append([]MeshVertex(nil), g.vertices...)
}
func (g *Geometry) Indices() []uint32 {
	if g == nil {
		return nil
	}
	return append([]uint32(nil), g.indices...)
}

type CommandKind uint32

const (
	OverlayCommand CommandKind = iota
	SceneCommand
	ImageCommand
)

// Image is a retained premultiplied-RGBA screen overlay. Bounds are x, y,
// width, height in top-left framebuffer pixels, with UV (0,0) at top left.
// Images blend in command order without reading or writing scene depth.
// This explicit alpha contract does not change opaque scene content surfaces.
type Image struct {
	Texture *Texture
	Bounds  [4]float32
}

// View describes a camera pass. Projection is column-major view-projection
// with Vulkan depth [0,1] and NDC Y increasing down the screen;
// Viewport is x,y,width,height in top-left framebuffer pixels. Light is the
// world-space direction toward the light. Depth is cleared per scene command.
type View struct {
	Projection [16]float32
	Eye, Light [3]float32
	Viewport   [4]float32
	Shadow     Shadow
	// EffectPhase is a normalized [0,1] phase for explicitly animated native
	// materials. It currently drives only Material.Hologram scan bands; ordinary
	// meshes, content surfaces and overlays ignore it. Hosts can freeze this
	// value for Reduced Motion without pausing application data or transforms.
	EffectPhase float32
	// PointLights add at most four view-local fill lights. PointLightCount marks
	// the active prefix. The fixed array keeps View value-comparable.
	PointLights     [4]PointLight
	PointLightCount int
	// TransparencyLayers bounds depth peeling when any Draw is Translucent.
	// Zero selects 8; explicit values must be in [1,32]. Layers beyond the
	// budget are omitted behind the nearest retained layers. Peeling uses
	// single-sample coverage and resolves equal-depth coincident fragments as
	// one surface. A frame supports four transparent views and 256MiB of targets.
	TransparencyLayers int
}

// Shadow describes one bounded directional-light depth map (1024x1024).
// Projection maps world positions to Vulkan clip coordinates, with depth [0,1].
// Strength is [0,1]; zero disables the pass. Bias is a normalized-depth receiver
// bias in [0,.05]. Only explicitly marked opaque objects cast or receive.
// A frame supports at most four shadowed camera commands. Outside this light
// frustum receivers stay lit. This is an SDR direct-light shadow, not AO/GI.
type Shadow struct {
	Projection     [16]float32
	Strength, Bias float32
}

// Material adds lightweight direct-light specular and view-dependent rim
// lighting to a mesh. Every field must be finite and in [0,1]. Specular controls
// highlight strength; Roughness broadens the highlight (with an effective floor
// of 0.08); Metallic tints reflection toward the mesh color. RimColor and
// RimStrength supply a restrained grazing-angle accent, independent of wire
// edges. This is an illustrative material model, not a full physical renderer.
// The zero value preserves the original diffuse lighting. Unlit meshes ignore
// the direct-light fields; content surfaces ignore every material field. A
// material belongs to its node and is not inherited through the hierarchy.
type Material struct {
	Specular, Roughness, Metallic, RimStrength float32
	RimColor                                   [3]float32
	// Transmission reduces diffuse body color to ten percent at one while
	// retaining direct highlights and the authored rim. Use it with Translucent
	// and a bounded Color alpha for a thin cinematic glass surface. The zero
	// Refraction value deliberately preserves that established path.
	Transmission float32
	// Refraction opts a transmitted translucent mesh into bounded backdrop
	// sampling. It controls both the strength of the sampled backdrop and a
	// maximum 18-pixel normal-directed bend at one. RefractionBlur adds a fixed
	// five-tap frosted-glass footprint of up to six output pixels. Both fields
	// require Transmission; frame validation rejects them on opaque meshes,
	// unlit meshes and content surfaces. Zero preserves exact legacy/client
	// pixels and the prior thin-glass result.
	Refraction, RefractionBlur float32
	// Hologram opts a translucent mesh into a depth-composited projection
	// treatment: view-dependent transparency, world-space scan bands and a
	// narrow highlight where the projection approaches opaque scene depth.
	// RimColor supplies its emitted tint; an all-zero rim uses the mesh color.
	// View.EffectPhase animates the bands. Content surfaces and screen overlays
	// cannot use this field, and zero preserves the established mesh result.
	Hologram float32
}

// PointLight is a bounded view-local fill light. Position and Radius use world
// coordinates. Color is authored sRGB and Intensity is linear radiance relative
// to the directional key light. Point lights do not cast shadows; they are
// intended for a few responsive instrument and selection accents.
type PointLight struct {
	Position  [3]float32
	Color     [3]float32
	Intensity float32
	Radius    float32
}

type ToneMap uint32

const (
	ToneMapNone ToneMap = iota
	// ToneMapFilmic applies a bounded filmic shoulder before SDR encoding. It
	// preserves highlight color produced by lights and bloom without implying
	// HDR scanout.
	ToneMapFilmic
)

// OutputTransform controls the linear-to-SDR presentation pass. Its zero value
// is bit-for-bit compatible with the original linear sRGB output. Exposure is
// measured in stops; Saturation and Contrast are signed offsets around the
// neutral value. Bloom extracts scene highlights in linear light and uses a
// fixed bounded 13-tap kernel. Radius is in output pixels; zero selects four.
// These controls require Frame.LinearColor. They do not provide an ICC transform,
// wide-gamut rendering, display calibration, or HDR signaling.
type OutputTransform struct {
	Exposure, Saturation, Contrast float32
	ToneMap                        ToneMap
	BloomStrength, BloomThreshold  float32
	BloomRadius                    float32
}

// Draw instantiates either retained geometry or a content surface, never both.
// Only these small constants change when an object moves. Model is column-major.
// A texture occupies a unit XY quad centered at the origin, with top-left UV at
// (-0.5,+0.5). Surface RGB is tinted by Color. By default surfaces are opaque
// (texture/tint alpha are ignored); Translucent explicitly enables coverage. Mesh colors use straight
// RGBA. A surface shares the camera's depth buffer with all other instances.
type Draw struct {
	// UV crops a texture using normalized x,y,width,height. Zero means full
	// image. Geometry and logical picking bounds are unchanged.
	UV               [4]float32
	Geometry         *Geometry
	Texture          *Texture
	Model            [16]float32
	Color, WireColor [4]float32
	WireWidth        float32
	Unlit            bool
	Material         Material
	// Glow authors a soft RGB background halo from mesh coverage. Channels
	// must be finite and in [0,1]; zero skips the effect. Vertex and instance
	// alpha attenuate emission. It does not alter the mesh's normal shading,
	// emit from content textures, or illuminate foreground surfaces. Occluded
	// fragments do not emit, and the halo is clipped to the camera viewport.
	// At most four SceneCommands per frame may contain emitting meshes.
	Glow [3]float32
	// DepthReadOnly meshes blend against the existing scene depth without
	// writing depth. Submit them after regular meshes and opaque surfaces;
	// callers own the mutual blend order of overlapping translucent meshes.
	// Content surfaces remain opaque and cannot use this flag.
	DepthReadOnly bool
	// Translucent opts into per-pixel depth peeling after opaque and legacy
	// DepthReadOnly draws. Mesh RGBA is straight; texture RGB must be
	// premultiplied by alpha. Texture/tint alpha both apply. Transparent layers
	// test opaque depth and never write the main depth buffer. Intersections
	// are correctly composed within View.TransparencyLayers. Mutually exclusive
	// with DepthReadOnly and CastShadow. The zero value stays unchanged.
	Translucent bool
	// CastShadow treats the authored silhouette as opaque in View.Shadow.
	// ReceiveShadow applies its visibility to direct lighting (or to app
	// surface RGB). Flags are local to the draw; ordinary app pixels ignore
	// shadow maps. Unlit meshes ignore received shadows.
	CastShadow, ReceiveShadow bool
}

// Command preserves the order of 2D overlays and independent 3D camera views.
// Overlay commands draw Frame.Vertices[First:First+Count] without depth testing.
// Scene commands clear depth within View.Viewport, then draw meshes and surfaces.
// Image commands blend retained premultiplied RGBA within Image.Bounds without depth.
type Command struct {
	Kind         CommandKind
	First, Count int
	View         View
	Draws        []Draw
	Image        Image
}

type Frame struct {
	// LinearColor opts into sRGB decoding, linear lighting/filtering/blending,
	// and sRGB output for the whole ordered frame. Authored colors, clear RGB,
	// and app texture pixels stay sRGB; alpha stays linear coverage. Clear RGB
	// is straight; output/readback bytes are premultiplied sRGB. False
	// preserves the original encoded-RGB renderer. Output is SDR sRGB, not HDR
	// or ICC-profile conversion. Select one mode consistently across frames.
	LinearColor bool
	// Output applies a display-referred SDR grade after all ordered scene and
	// overlay composition. The zero value preserves the normal sRGB transfer.
	Output   OutputTransform
	Vertices []Vertex
	Commands []Command
}
