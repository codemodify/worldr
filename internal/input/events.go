// Package input reads relative pointer and keyboard events from evdev.
package input

// EventKind identifies one ordered input edge or movement.
type EventKind uint8

const (
	Move EventKind = iota + 1
	Down
	Up
	Scroll
	KeyInput
	// Cancel means device state was lost. Consumers must cancel both keyboard
	// focus/repeat and pointer capture, and forget their held modifier state.
	Cancel
)

// Event preserves the coordinates and timestamp at this input edge. Code is
// the original Linux key or button code. Time is the evdev timestamp in
// milliseconds, wrapping at uint32 as in the application input protocol.
// Scroll uses ten units per wheel detent: positive Y scrolls down and positive
// X scrolls right. High-resolution wheels preserve fractional detents.
type Event struct {
	Kind             EventKind
	X, Y             int
	Code, Time       uint32
	Pressed          bool
	ScrollX, ScrollY float32
}
