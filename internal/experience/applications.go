package experience

import "github.com/codemodify/worldr/internal/render"

// ApplicationSurface describes live client content without exposing a display
// protocol to the scene. IDs identify this connection only and are not saved.
// Texture dimensions are image pixels; the adapter converts input to the
// client's logical coordinate system.
type ApplicationSurface struct {
	FrameStyle FrameStyle
	ID         uint64
	Key        string // Stable launch-slot/window association for workspace layouts.
	AppID      string
	Title      string
	Texture    *render.Texture
	// ContentAspect overrides the image aspect for cropped/fractional buffers.
	// Pointer coordinates still span the complete texture dimensions. Zero uses
	// the texture ratio. SurfaceUV is normalized x,y,width,height (zero: full).
	ContentAspect float32
	SurfaceUV     [4]float32
	Translucent   bool
	// Spatial adds retained 3D objects to the application's content plane.
	// These objects share the workspace camera and depth buffer.
	Spatial *SpatialContent
	// Frameless suppresses workspace borders, halos and drag grips. DragContent
	// treats the image as passive content: drag it to move the window, and keep
	// pointer gestures and Read-mode keyboard shortcuts in the workspace.
	// Both default to false.
	Frameless, DragContent bool
}

type FrameStyle string

const (
	FrameDefault      FrameStyle = ""
	FrameCinematic    FrameStyle = "cinematic"
	FramePhotoBracket FrameStyle = "photo-bracket"
)

type ApplicationLaunch struct{ Kind, Title string }
type ApplicationLaunchCatalog interface{ ApplicationLaunches() []ApplicationLaunch }

type ApplicationTextInput interface {
	TextInput(id uint64) TextInputState
}

// Applications supplies live content and receives explicit workspace decisions.
// The host owns its lifetime and polls it before updating the experience.
// Calls and borrowed Surfaces slices belong to the host goroutine. Focus(0)
// releases keyboard focus; Send coordinates are texture pixels. Resize changes
// the client's logical content extent, independently of spatial placement.
type Applications interface {
	Surfaces() []ApplicationSurface
	Focus(id uint64)
	Send(id uint64, event Event)
	Resize(id uint64, width, height int)
}

// ApplicationPoller advances provider work on the host goroutine. Poll must do
// bounded work and return without waiting for future input. Providers without
// background work need not implement it.
type ApplicationPoller interface {
	Poll() error
}

// ApplicationProviderCloser releases a provider and all of its applications.
// The host calls Close once, in reverse provider registration order, and stops
// polling or dispatching input before teardown begins.
type ApplicationProviderCloser interface {
	Close() error
}

// ApplicationTextureRetirer drains texture IDs that the provider no longer
// uses. The host releases their GPU resources after the experience has removed
// these textures from its frame, and before closing the GPU session. A provider
// must retain pending IDs until drained, including IDs retired during Close;
// this method remains callable after provider teardown.
type ApplicationTextureRetirer interface {
	RetiredTextures() []uint64
}

// ApplicationGeometryRetirer drains immutable mesh resources no longer used
// by any surface from this provider. The host retires them after reconciliation.
type ApplicationGeometryRetirer interface{ RetiredGeometryIDs() []uint64 }

// ApplicationCloser requests that one live surface close. Compatibility
// clients may first present their own unsaved-work confirmation dialog.
type ApplicationCloser interface {
	CloseApplication(id uint64)
}

// ApplicationLauncher creates native content and returns its stable layout key.
// A launch is an explicit process action and does not belong to document undo.
type ApplicationLauncher interface {
	LaunchApplication(kind string) (string, error)
}

type ApplicationAware interface {
	SetApplications(Applications)
}

// ApplicationPlacementChecker checks whether a stable key can occupy the
// current saved layout and available provider surfaces before the host opens
// content. It does not reserve a slot or change the workspace view or history.
type ApplicationPlacementChecker interface {
	CheckApplicationPlacement(key string) error
}

// ApplicationActivator explicitly selects and focuses an already registered
// application. Opening a file need not activate its viewer.
type ApplicationActivator interface {
	ActivateApplication(key string) error
}

// ApplicationCursor describes an optional client cursor in screen space.
// Texture contains premultiplied RGBA pixels; Scale is buffer pixels per
// logical cursor pixel. Hotspot coordinates are logical pixels and can lie
// outside the image. Hidden is an explicit request to suppress the cursor.
// Resources remain owned by the application provider.
type ApplicationCursor struct {
	Texture            *render.Texture
	HotspotX, HotspotY float32
	// LogicalSize supports independently scaled cursor/drag-icon dimensions.
	// Zero retains the historical texture/Scale behavior.
	LogicalSize [2]float32
	Scale       float32
	Hidden      bool
}

// ApplicationCursorProvider returns a cursor only for its current pointer
// target. False requests the workspace fallback; keyboard focus is unrelated.
type ApplicationCursorProvider interface {
	ApplicationCursor(id uint64) (ApplicationCursor, bool)
}

// PointerCursor lets an experience choose the cursor for the surface to which
// it routes the pointer, including an active captured drag.
type PointerCursor interface {
	Cursor() (ApplicationCursor, bool)
}

// KeyboardOwner prevents host shortcuts from intercepting keys intended for a
// focused application. The host retains a separate explicit exit chord.
type KeyboardOwner interface {
	OwnsKeyboard() bool
}

// ApplicationNewPlacement receives explicitly opened instances after the
// provider accepts them. It places reused closed slots in the current space
// without taking keyboard focus. Session restoration must not call it.
type ApplicationNewPlacement interface{ PlaceNewApplication(key string) }

// ApplicationDragRouter temporarily replaces implicit pointer capture while a
// client-owned data drag is active. A zero destination means no compatible drop
// target. Input remains in the source provider; keyboard focus does not follow
// the drag. The returned bool says whether the destination accepted routing.
type ApplicationDragRouter interface {
	ApplicationDragActive(source uint64) bool
	ApplicationDrag(source, target uint64, event Event) bool
}
