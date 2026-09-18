// Package xwayland hosts explicitly enabled X11 clients on a private Xwayland
// server. Window policy and focus stay with the workspace; the bridge never
// connects to the desktop's DISPLAY.
package xwayland

import (
	"errors"
	"strings"
)

var ErrClosed = errors.New("Xwayland bridge is closed")

// Window describes an X11 root window associated with one retained surface.
// OverrideRedirect and TransientFor are hints, not requests to change focus.
// PID is the local client PID reported by XRes, or zero when unavailable.
type Window struct {
	ID, ObjectID, TransientFor, PID uint32
	SurfaceID                       uint64
	Title, AppID                    string
	X, Y, Width, Height             int
	OverrideRedirect                bool
}

// ClipboardOffer describes the current X11 CLIPBOARD selection without
// transferring its contents. ExternalID is nonzero when the selection was
// supplied by Worldr through OfferClipboard.
type ClipboardOffer struct {
	Revision   uint64
	ID         uint64
	ExternalID uint64
	MIMEs      []string
}

// ClipboardRequest transfers ownership of FD to the caller. ExternalID is the
// token passed to OfferClipboard for the selection requested by an X11 client.
type ClipboardRequest struct {
	ExternalID uint64
	MIME       string
	FD         int
}

// XDNDOffer describes one copy-only XdndSelection owned by a private X11
// client. Owner is the X11 selection-owner window. Target is nonzero while a
// managed XDND-aware destination is entered.
type XDNDOffer struct {
	Revision   uint64
	ID         uint64
	Owner, PID uint32
	Target     uint32
	Active     bool
	Accepted   bool
	Dropped    bool
	MIMEs      []string
}

const (
	maxClipboardMIMEs = 64
	maxClipboardBytes = 1 << 20
)

func privateEnvironment(parent []string, socket, display, authority string) []string {
	out := make([]string, 0, len(parent)+4)
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "DISPLAY", "XAUTHORITY", "WAYLAND_DISPLAY", "WAYLAND_SOCKET", "XDG_SESSION_TYPE":
			continue
		}
		out = append(out, entry)
	}
	return append(out, "WAYLAND_DISPLAY="+socket, "DISPLAY="+display, "XAUTHORITY="+authority, "XDG_SESSION_TYPE=x11")
}
