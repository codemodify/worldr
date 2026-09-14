// Package engine holds the scene and window-actor model.
//
// The desktop is a cinematic window theater (Compiz-like). Client surfaces
// become textured window actors. A Compiz-style effect graph is a later
// build step; this package is a stub until the compositor can feed it
// surfaces.
package engine

// Actor is a window (or other scene object) in the cinematic desktop.
type Actor struct{}

// Scene holds window actors and, later, the effect graph.
type Scene struct{}
