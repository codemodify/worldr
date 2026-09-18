// Package experience defines the contract between a native experience and its
// host. Platform events, display ownership, and files belong to the host;
// document semantics, interaction, and presentation belong to the experience.
package experience

import (
	"time"

	"github.com/codemodify/worldr/internal/render"
)

type Info struct{ ID, Title, Controls string }

// Experience is owned by one host goroutine. Draw borrows its frame storage:
// the host must finish submitting the frame before the next Draw or Close.
// Atlas pixels remain immutable until Close. Handle reports consumption, never
// a process-exit request; the host owns global commands such as quit and save.
type Experience interface {
	Info() Info
	Atlas() render.Atlas
	Update(time.Duration)
	Draw(width, height int) render.Frame
	Handle(Event) bool
	Close() error
}

// Stateful encodes the experience's typed document, without renderer resources
// or live input capture. LoadState must validate before changing current state.
// The host owns file I/O and an outer envelope identifying the experience.
type Stateful interface {
	SaveState() ([]byte, error)
	LoadState([]byte) error
}

// Checkpointer snapshots committed state without canceling input, stopping
// animations, changing focus or editing history. The host may call it while
// the user works, then write the returned owned bytes on a background worker.
type Checkpointer interface{ CheckpointState() ([]byte, error) }

// NotificationReceiver shows a host status message without changing focus.
type NotificationReceiver interface{ Notify(message string) }

// Demonstrator drives the same domain commands as real interaction. elapsed is
// a deterministic host clock; the host does not know any experience bindings.
type Demonstrator interface{ Demo(elapsed time.Duration) }

type EventKind uint8

const (
	PointerMove EventKind = iota + 1
	PointerDown
	PointerUp
	PointerCancel
	KeyInput
	PointerScroll
	KeyboardCancel
	KeymapChanged
	KeyboardModifiers
	KeyboardRepeatInfo
	TextCommit
	TextPreedit
)

type Button uint8

const (
	ButtonNone Button = iota
	ButtonPrimary
	ButtonSecondary
	ButtonMiddle
)

type Key string

const (
	KeyUnknown Key = ""
	KeySpace   Key = "Space"
	KeyEscape  Key = "Escape"
	KeyF1      Key = "F1"
	KeyLeft    Key = "ArrowLeft"
	KeyRight   Key = "ArrowRight"
	KeyB       Key = "B"
	KeyE       Key = "E"
	KeyF       Key = "F"
	KeyG       Key = "G"
	KeyP       Key = "P"
	KeyO       Key = "O"
	KeyR       Key = "R"
	KeyS       Key = "S"
	KeyQ       Key = "Q"
	KeyZ       Key = "Z"
	KeyY       Key = "Y"
	Key1       Key = "1"
	Key2       Key = "2"
	Key3       Key = "3"
)

type Modifiers uint8

const (
	ModControl Modifiers = 1 << iota
	ModShift
	ModAlt
	ModSuper
)

func (m Modifiers) Has(flag Modifiers) bool { return m&flag == flag }

// Pointer coordinates use the current render extent in framebuffer pixels.
// Down/Up carry the changed button; Move needs no button because the experience
// owns capture. Cancel has no coordinates and must not be treated as a release.
// Key is a normalized name for native commands. Keycode and ButtonCode retain
// Linux evdev codes for application bridges, including keys without a semantic
// name. XKB masks belong to the most recent KeymapChanged map. KeyboardCancel
// clears keyboard focus independently of pointer capture. Scroll values use
// Wayland axis units; positive values move down or right. Text composition is
// delivered separately from physical keys. TextCommit applies deletion and
// insertion atomically; TextPreedit is temporary composition, not document text.
type Event struct {
	Text                      string
	TextContext               string
	PreeditBegin, PreeditEnd  int32
	DeleteBefore, DeleteAfter uint32
	// A native mesh pick carries a provider-local object ID, triangle and
	// app-local hit point. Zero object means ordinary content-plane input.
	SpatialObject                     uint64
	SpatialTriangle                   int
	SpatialPoint                      [3]float32
	Kind                              EventKind
	X, Y                              float32
	Button                            Button
	Key                               Key
	Modifiers                         Modifiers
	Pressed, Repeat                   bool
	Keycode, ButtonCode               uint32
	Time                              uint32
	Depressed, Latched, Locked, Group uint32
	ScrollX, ScrollY                  float32
	Keymap                            string
	RepeatRate, RepeatDelay           int32
}

// TextInputState describes a focused native text destination. Offsets are UTF-8
// bytes. CursorRect is x/y/width/height in framebuffer pixels for the host,
// texture pixels for an application. Changing ContextID invalidates any pending
// composition from the old field, even when the surrounding text is identical.
type TextInputState struct {
	Enabled        bool
	ContextID      string
	Surrounding    string
	Cursor, Anchor int
	CursorRect     [4]int
}

type TextInputSource interface{ TextInput() TextInputState }
