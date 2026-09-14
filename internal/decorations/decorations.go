// Package decorations implements server-side decorations (SSD).
//
// worldr draws decorations for foreign Wayland/X11 clients and for native
// apps. Client-side decorations are not the default. Unimplemented in Phase 0;
// SSD borders land after surfaces become textured window actors.
package decorations

// ServerSide is compositor-owned chrome for a window actor.
type ServerSide struct{}
