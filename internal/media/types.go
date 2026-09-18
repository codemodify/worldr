// Package media embeds local video playback and audio without creating a window.
// Player methods are serialized internally. Call Poll and Render regularly while
// playing; decoded video is copied directly into a caller-owned RGBA surface.
package media

import "errors"

var (
	ErrClosed      = errors.New("media player is closed")
	ErrUnavailable = errors.New("native media playback requires Linux, CGO and libmpv with software rendering")
)

type Options struct {
	// AudioOutput is an mpv audio driver name. Empty selects the system output;
	// tests use "null" to exercise audio synchronization silently.
	AudioOutput string
	// InitialState is applied before initializing playback, so a restored
	// muted or paused session cannot briefly start with default audio settings.
	// Nil uses playing, unmuted audio at volume 70.
	InitialState *InitialState
}

type InitialState struct {
	Paused bool
	Volume float64
	Muted  bool
}

// State is a value snapshot; asynchronous decoder failures appear in Error.
type State struct {
	Loaded, Paused, Ended, HasVideo, Muted bool
	Position, Duration, Volume             float64
	Width, Height                          int
	Error                                  string
	// SeekRevision advances when playback has restarted after a seek, including
	// while paused. It lets a host defer unpausing until a restored seek is ready.
	SeekRevision uint64
}
