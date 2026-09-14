// Package compositor will host the Wayland server and XWayland integration.
//
// Responsibilities (not implemented in Phase 0):
//   - Real Wayland compositor from day one (DRM/KMS + Vulkan present), not a
//     nested Wayland client that draws into another compositor first.
//   - Accept Wayland clients; map each surface to an engine window actor.
//   - Run X11 clients via XWayland.
//   - Apply server-side decorations to foreign and native clients.
//   - Own input and focus once those land (build step 3).
//
// The compositor process owns the GPU device and present path. Isolated
// experiences/apps (later process model) talk through DMA-BUF + explicit sync.
package compositor

// Server is the compositor core. Unimplemented in Phase 0.
type Server struct{}
