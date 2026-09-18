package host

import "errors"

var ErrClosed = errors.New("Wayland host is closed")

type Kind uint8

const (
	Move Kind = iota + 1
	Down
	Up
	Cancel
	Key
	Close
	Scroll
	KeyboardCancel
	KeymapChanged
	ModifiersChanged
	RepeatInfo
	TextCommit
	TextPreedit
)

type Event struct {
	Kind                              Kind
	Code                              uint32
	Modifiers                         uint8
	X, Y                              float32
	Pressed                           bool
	Keycode, ButtonCode               uint32
	Time                              uint32
	Depressed, Latched, Locked, Group uint32
	ScrollX, ScrollY                  float32
	Keymap                            string
	RepeatRate, RepeatDelay           int32
	Text, TextContext                 string
	PreeditBegin, PreeditEnd          int32
	DeleteBefore, DeleteAfter         uint32
}
