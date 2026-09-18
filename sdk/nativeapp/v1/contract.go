package nativeapp

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const Version uint32 = 1

const (
	MaxSurfaces       = 8
	MaxTextures       = 32
	MaxTextureWidth   = 4096
	MaxTextureHeight  = 4096
	MaxTextureBytes   = 64 << 20
	MaxMeshes         = 32
	MaxMeshVertices   = 250_000
	MaxMeshIndices    = 750_000
	MaxSpatialObjects = 4096
	MaxSpatialLabels  = 256
	MaxSemanticNodes  = 512
	MaxTextBytes      = 1 << 20
)

type ResourceID uint64
type SurfaceID uint64

// Manifest is immutable for the life of a connection. ID should use a stable
// reverse-domain name because Worldr combines it with Surface.Key for layouts.
type Manifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func (m Manifest) Validate() error {
	if len(m.ID) < 3 || len(m.ID) > 128 || strings.HasPrefix(m.ID, ".") || strings.HasSuffix(m.ID, ".") {
		return fmt.Errorf("native app ID must contain 3..128 characters")
	}
	for _, r := range m.ID {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_') {
			return fmt.Errorf("native app ID %q must use lowercase letters, digits, dots, hyphens, or underscores", m.ID)
		}
	}
	if len(m.Name) == 0 || len(m.Name) > 128 || !utf8.ValidString(m.Name) {
		return fmt.Errorf("native app name must be valid UTF-8 and contain 1..128 bytes")
	}
	if len(m.Description) > 1024 || !utf8.ValidString(m.Description) {
		return fmt.Errorf("native app description must be valid UTF-8 and at most 1024 bytes")
	}
	return nil
}

// Application publishes its current complete surface list plus retained
// resource deltas. Serve calls Snapshot once after each successful request;
// implementations normally move pending deltas out of their queue while
// constructing the return value. Optional interfaces below receive lifecycle
// and input calls.
type Application interface {
	Manifest() Manifest
	Snapshot() Snapshot
}

type Starter interface{ Start(Host) error }
type Updater interface{ Update(time.Duration) error }
type InputHandler interface{ Handle(SurfaceID, Event) error }
type FocusHandler interface{ Focus(SurfaceID) error }
type ResizeHandler interface {
	Resize(SurfaceID, int, int) error
}
type SurfaceCloser interface{ CloseSurface(SurfaceID) error }
type Closer interface{ Close() error }

// Host records the negotiated limits. Version 1 uses framebuffer pixels for
// pointer coordinates, texture damage, semantic bounds, and resize requests.
type Host struct {
	Version          uint32 `json:"version"`
	MaxSurfaceWidth  int    `json:"max_surface_width"`
	MaxSurfaceHeight int    `json:"max_surface_height"`
	MaxSurfaces      int    `json:"max_surfaces"`
}

type Snapshot struct {
	// Surfaces is complete. Omitting a formerly published surface closes it.
	Surfaces []Surface `json:"surfaces,omitempty"`
	// Textures and Meshes contain only new or changed retained resources.
	Textures []TextureUpdate `json:"textures,omitempty"`
	Meshes   []MeshResource  `json:"meshes,omitempty"`
	// Retirement is explicit and may only occur after all references disappear.
	RetireTextures []ResourceID `json:"retire_textures,omitempty"`
	RetireMeshes   []ResourceID `json:"retire_meshes,omitempty"`
}

type FrameStyle string

const (
	FrameDefault      FrameStyle = "default"
	FrameCinematic    FrameStyle = "cinematic"
	FramePhotoBracket FrameStyle = "photo-bracket"
)

type Surface struct {
	ID      SurfaceID  `json:"id"`
	Key     string     `json:"key"`
	Title   string     `json:"title"`
	Texture ResourceID `json:"texture"`

	FrameStyle    FrameStyle `json:"frame_style,omitempty"`
	ContentAspect float32    `json:"content_aspect,omitempty"`
	UV            [4]float32 `json:"uv,omitempty"`
	Translucent   bool       `json:"translucent,omitempty"`
	Frameless     bool       `json:"frameless,omitempty"`
	DragContent   bool       `json:"drag_content,omitempty"`

	Spatial   *SpatialContent `json:"spatial,omitempty"`
	Semantics SemanticTree    `json:"semantics,omitempty"`
	TextInput TextInputState  `json:"text_input,omitempty"`
}

// TextureUpdate is a retained RGBA8 update. Rows are tightly packed, top to
// bottom. RGB is sRGB. Alpha is premultiplied when Translucent is requested.
// Revision starts at 1 and increments by exactly one. The first update and any
// resize must cover Rect{0,0,Width,Height}; later updates may be damaged regions.
type TextureUpdate struct {
	ID       ResourceID `json:"id"`
	Revision uint64     `json:"revision"`
	Width    int        `json:"width"`
	Height   int        `json:"height"`
	Rect     Rect       `json:"rect"`
	Pixels   []byte     `json:"pixels"`
}

type Rect struct {
	X      int `json:"x,omitempty"`
	Y      int `json:"y,omitempty"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func (r Rect) validWithin(width, height int) bool {
	return r.Width > 0 && r.Height > 0 && r.X >= 0 && r.Y >= 0 && r.X <= width-r.Width && r.Y <= height-r.Height
}

type Vec3 struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
	Z float32 `json:"z"`
}
type Color struct {
	R float32 `json:"r"`
	G float32 `json:"g"`
	B float32 `json:"b"`
	A float32 `json:"a"`
}

// MeshResource is immutable. Updating geometry uses a new ID and retires the
// old one after references disappear. Indices are complete triangles. Normals
// are optional; when supplied there must be one finite nonzero normal per vertex.
type MeshResource struct {
	ID         ResourceID `json:"id"`
	Vertices   []Vec3     `json:"vertices"`
	Normals    []Vec3     `json:"normals,omitempty"`
	Indices    []uint32   `json:"indices"`
	FaceColors []Color    `json:"face_colors,omitempty"`
}

type SpatialContent struct {
	Objects []SpatialObject `json:"objects,omitempty"`
	Labels  []SpatialLabel  `json:"labels,omitempty"`
}

// SpatialObject coordinates use application width as one unit, +Y upward and
// +Z toward the viewer. Parents must precede children. Transform is column-major;
// zero selects identity. An object can reference a mesh or texture, not both.
type SpatialObject struct {
	ID            uint64      `json:"id"`
	Parent        uint64      `json:"parent,omitempty"`
	Mesh          ResourceID  `json:"mesh,omitempty"`
	Texture       ResourceID  `json:"texture,omitempty"`
	Transform     [16]float32 `json:"transform,omitempty"`
	UV            [4]float32  `json:"uv,omitempty"`
	Color         Color       `json:"color,omitempty"`
	WireColor     Color       `json:"wire_color,omitempty"`
	WireWidth     float32     `json:"wire_width,omitempty"`
	Material      Material    `json:"material,omitempty"`
	Glow          [3]float32  `json:"glow,omitempty"`
	Hidden        bool        `json:"hidden,omitempty"`
	Unlit         bool        `json:"unlit,omitempty"`
	Unpickable    bool        `json:"unpickable,omitempty"`
	DepthReadOnly bool        `json:"depth_read_only,omitempty"`
	Translucent   bool        `json:"translucent,omitempty"`
	CastShadow    bool        `json:"cast_shadow,omitempty"`
	ReceiveShadow bool        `json:"receive_shadow,omitempty"`
}

type Material struct {
	Specular    float32    `json:"specular,omitempty"`
	Roughness   float32    `json:"roughness,omitempty"`
	Metallic    float32    `json:"metallic,omitempty"`
	RimStrength float32    `json:"rim_strength,omitempty"`
	RimColor    [3]float32 `json:"rim_color,omitempty"`
	// Transmission in [0,1] is available only to translucent mesh objects.
	// It reduces diffuse body color while retaining direct highlights and rim.
	Transmission float32 `json:"transmission,omitempty"`
	// Refraction in [0,1] samples the already opaque scene through a lit,
	// transmitted translucent mesh, with a bounded normal-directed bend.
	// RefractionBlur in [0,1] adds a bounded five-tap frosted footprint. Both are
	// optional additive v1 fields; zero preserves the original thin-glass path.
	Refraction     float32 `json:"refraction,omitempty"`
	RefractionBlur float32 `json:"refraction_blur,omitempty"`
}

type SpatialLabel struct {
	Text     string `json:"text"`
	Position Vec3   `json:"position"`
	Color    Color  `json:"color"`
}

type Role string

const (
	RoleButton    Role = "button"
	RoleTextField Role = "textbox"
	RoleMenuItem  Role = "menuitem"
	RoleLabel     Role = "label"
	RoleSlider    Role = "slider"
	RoleImage     Role = "image"
	RoleDocument  Role = "document"
	RoleStatus    Role = "status"
)

type SemanticNode struct {
	ID          string `json:"id"`
	Role        Role   `json:"role"`
	Label       string `json:"label,omitempty"`
	Value       string `json:"value,omitempty"`
	Description string `json:"description,omitempty"`
	Bounds      Rect   `json:"bounds"`
	Disabled    bool   `json:"disabled,omitempty"`
	Selected    bool   `json:"selected,omitempty"`
}

type SemanticTree struct {
	Nodes     []SemanticNode `json:"nodes,omitempty"`
	FocusedID string         `json:"focused_id,omitempty"`
}

// TextInputState offsets are UTF-8 byte offsets. ContextID must change when a
// different field receives focus so stale compositor transactions are rejected.
type TextInputState struct {
	Enabled     bool   `json:"enabled"`
	ContextID   string `json:"context_id,omitempty"`
	Surrounding string `json:"surrounding,omitempty"`
	Cursor      int    `json:"cursor,omitempty"`
	Anchor      int    `json:"anchor,omitempty"`
	CursorRect  Rect   `json:"cursor_rect,omitempty"`
}

type EventKind string

const (
	PointerMove       EventKind = "pointer_move"
	PointerDown       EventKind = "pointer_down"
	PointerUp         EventKind = "pointer_up"
	PointerCancel     EventKind = "pointer_cancel"
	PointerScroll     EventKind = "pointer_scroll"
	KeyInput          EventKind = "key"
	KeyboardCancel    EventKind = "keyboard_cancel"
	KeymapChanged     EventKind = "keymap"
	KeyboardModifiers EventKind = "modifiers"
	KeyboardRepeat    EventKind = "repeat_info"
	TextCommit        EventKind = "text_commit"
	TextPreedit       EventKind = "text_preedit"
)

type Button uint8

const (
	ButtonNone Button = iota
	ButtonPrimary
	ButtonSecondary
	ButtonMiddle
)

type Modifiers uint8

const (
	ModControl Modifiers = 1 << iota
	ModShift
	ModAlt
	ModSuper
)

func (m Modifiers) Has(flag Modifiers) bool { return m&flag == flag }

// Event preserves normalized names together with evdev/XKB data. TextCommit
// and TextPreedit are separate from physical keys. Spatial fields identify an
// app-local object pick; zero means ordinary backing-plane input.
type Event struct {
	Kind EventKind `json:"kind"`

	X          float32 `json:"x,omitempty"`
	Y          float32 `json:"y,omitempty"`
	Button     Button  `json:"button,omitempty"`
	ButtonCode uint32  `json:"button_code,omitempty"`
	ScrollX    float32 `json:"scroll_x,omitempty"`
	ScrollY    float32 `json:"scroll_y,omitempty"`

	Key       string    `json:"key,omitempty"`
	Keycode   uint32    `json:"keycode,omitempty"`
	Modifiers Modifiers `json:"modifiers,omitempty"`
	Pressed   bool      `json:"pressed,omitempty"`
	Repeat    bool      `json:"repeat,omitempty"`
	Time      uint32    `json:"time,omitempty"`

	Keymap      string `json:"keymap,omitempty"`
	Depressed   uint32 `json:"depressed,omitempty"`
	Latched     uint32 `json:"latched,omitempty"`
	Locked      uint32 `json:"locked,omitempty"`
	Group       uint32 `json:"group,omitempty"`
	RepeatRate  int32  `json:"repeat_rate,omitempty"`
	RepeatDelay int32  `json:"repeat_delay,omitempty"`

	Text         string `json:"text,omitempty"`
	TextContext  string `json:"text_context,omitempty"`
	PreeditBegin int32  `json:"preedit_begin,omitempty"`
	PreeditEnd   int32  `json:"preedit_end,omitempty"`
	DeleteBefore uint32 `json:"delete_before,omitempty"`
	DeleteAfter  uint32 `json:"delete_after,omitempty"`

	SpatialObject   uint64     `json:"spatial_object,omitempty"`
	SpatialTriangle int        `json:"spatial_triangle,omitempty"`
	SpatialPoint    [3]float32 `json:"spatial_point,omitempty"`
}

func finite(values ...float32) bool {
	for _, value := range values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return false
		}
	}
	return true
}
