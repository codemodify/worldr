package wlclient

// HostKey is one evdev-style key edge from the host seat.
type HostKey struct {
	Code    uint32
	Pressed bool
}

// Input is a poll of host pointer/keyboard since the last TakeInput.
type Input struct {
	X, Y           int
	Click, Release bool
	Inside         bool
	Keys           []HostKey
}
