// Package app provides the standalone GPU window runtime for worldr-kit.
// Applications receive framebuffer coordinates and record retained GPU frames;
// no desktop, workspace, or application shell is required.
package app

import (
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/platform/linux/host"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

// Application is owned by Run's goroutine. Draw borrows its buffers until the
// frame has been submitted. Atlas pixels must remain immutable while running.
// Run closes the application on every return, including initialization failure.
type Application interface {
	Atlas() Atlas
	Update(time.Duration) error
	Draw(width, height int) Frame
	Handle(Event) bool
	Close() error
}

// Options dimensions are logical pixels for a native window, framebuffer pixels
// when Headless. FPS bounds updates and submissions (default 60, maximum 240).
// Transparent preserves alpha for the host compositor; blur behind a window is
// compositor policy. Snapshot writes a final PNG with its alpha preserved.
type Options struct {
	Title                                  string
	Width, Height, FPS                     int
	Transparent, ClientDecorated, Headless bool
	Frames                                 int
	Duration                               time.Duration
	Snapshot                               string
}

// Optional application interfaces:
//
// SetHost(*Host) receives native window and clipboard operations before input.
// SetScale(float32) receives the compositor scale before Draw and after changes.
// NeedsFrame() bool skips GPU work while false; Update and input still run.
// TextInput() TextInputState enables the compositor's text input protocol.
// RetiredTextures() []uint64 owns texture cache lifetimes: textures stay on the
// GPU until returned here after a frame no longer references them, or Run exits.
// Host.Wake may request a frame from a worker goroutine.

type (
	Event           = experience.Event
	EventKind       = experience.EventKind
	Key             = experience.Key
	Button          = experience.Button
	Modifiers       = experience.Modifiers
	TextInputState  = experience.TextInputState
	Frame           = render.Frame
	Atlas           = render.Atlas
	Vertex          = render.Vertex
	Command         = render.Command
	CommandKind     = render.CommandKind
	Image           = render.Image
	Texture         = render.Texture
	Geometry        = render.Geometry
	MeshVertex      = render.MeshVertex
	View            = render.View
	Draw            = render.Draw
	Material        = render.Material
	OutputTransform = render.OutputTransform
	Canvas          = scene.Canvas
	Color           = scene.Color
	ResizeEdge      = host.ResizeEdge
)

func NewCanvas() (*Canvas, error)                        { return scene.NewCanvas() }
func NewCanvasWithFont(fontData []byte) (*Canvas, error) { return scene.NewCanvasWithFont(fontData) }
func ColorHex(rgb uint32, alpha float32) Color           { return scene.ColorHex(rgb, alpha) }
func NewTexture(width, height int, pixels []byte) (*Texture, error) {
	return render.NewTexture(width, height, pixels)
}
func NewGeometry(vertices []MeshVertex, indices []uint32) (*Geometry, error) {
	return render.NewGeometry(vertices, indices)
}

const (
	PointerMove        = experience.PointerMove
	PointerDown        = experience.PointerDown
	PointerUp          = experience.PointerUp
	PointerCancel      = experience.PointerCancel
	PointerScroll      = experience.PointerScroll
	KeyInput           = experience.KeyInput
	KeyboardCancel     = experience.KeyboardCancel
	KeymapChanged      = experience.KeymapChanged
	KeyboardModifiers  = experience.KeyboardModifiers
	KeyboardRepeatInfo = experience.KeyboardRepeatInfo
	TextCommit         = experience.TextCommit
	TextPreedit        = experience.TextPreedit
	ButtonNone         = experience.ButtonNone
	ButtonPrimary      = experience.ButtonPrimary
	ButtonSecondary    = experience.ButtonSecondary
	ButtonMiddle       = experience.ButtonMiddle
	ModControl         = experience.ModControl
	ModShift           = experience.ModShift
	ModAlt             = experience.ModAlt
	ModSuper           = experience.ModSuper
	KeyUnknown         = experience.KeyUnknown
	KeySpace           = experience.KeySpace
	KeyEnter           = experience.KeyEnter
	KeyTab             = experience.KeyTab
	KeyEscape          = experience.KeyEscape
	KeyF1              = experience.KeyF1
	KeyLeft            = experience.KeyLeft
	KeyRight           = experience.KeyRight
	KeyUp              = experience.KeyUp
	KeyDown            = experience.KeyDown
	KeyB               = experience.KeyB
	KeyC               = experience.KeyC
	KeyE               = experience.KeyE
	KeyF               = experience.KeyF
	KeyG               = experience.KeyG
	KeyH               = experience.KeyH
	KeyJ               = experience.KeyJ
	KeyK               = experience.KeyK
	KeyM               = experience.KeyM
	KeyO               = experience.KeyO
	KeyP               = experience.KeyP
	KeyQ               = experience.KeyQ
	KeyR               = experience.KeyR
	KeyS               = experience.KeyS
	KeyY               = experience.KeyY
	KeyZ               = experience.KeyZ
	Key1               = experience.Key1
	Key2               = experience.Key2
	Key3               = experience.Key3
	OverlayCommand     = render.OverlayCommand
	SceneCommand       = render.SceneCommand
	ImageCommand       = render.ImageCommand
	FluidCommand       = render.FluidCommand
	ToneMapNone        = render.ToneMapNone
	ToneMapFilmic      = render.ToneMapFilmic
	ResizeTop          = host.ResizeTop
	ResizeBottom       = host.ResizeBottom
	ResizeLeft         = host.ResizeLeft
	ResizeRight        = host.ResizeRight
	ResizeTopLeft      = host.ResizeTopLeft
	ResizeTopRight     = host.ResizeTopRight
	ResizeBottomLeft   = host.ResizeBottomLeft
	ResizeBottomRight  = host.ResizeBottomRight
)
