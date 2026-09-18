package host

import "errors"

var (
	ErrClipboardUnavailable = errors.New("host clipboard requires a supported seat and focused input serial")
	ErrClipboardStale       = errors.New("host clipboard offer is stale or does not contain the requested format")
)

// ClipboardOffer is metadata only; observing it never reads clipboard bytes.
// A native host selection has ExternalID zero. A selection published by the
// bridge echoes its caller-supplied ExternalID, preventing clipboard loops.
// ID is a transient selection token and becomes invalid on replacement or loss
// of keyboard focus. Outgoing sources remain available after focus loss.
type ClipboardOffer struct {
	Revision, ID, ExternalID uint64
	MIMEs                    []string
	// Available distinguishes an empty focused clipboard from losing access
	// when keyboard focus leaves this window. It never reads clipboard data.
	Available bool
}

// ClipboardRequest transfers ownership of FD to the caller. Relay it to the
// originating source, then close it, even when that source has disappeared.
// No clipboard content is buffered or interpreted by the host.
type ClipboardRequest struct {
	ExternalID uint64
	MIME       string
	FD         int
}
